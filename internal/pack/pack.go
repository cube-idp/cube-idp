// Package pack implements cube-idp's extensibility tier 1 (spec §4.4):
// data-only directories with pack.cue metadata, fetched from local dirs or
// OCI, values-validated with CUE, rendered in-process.
//
// Pack format: a directory containing:
//
//	pack.cue             required: name, version; optional description,
//	                      #Values schema (see docs/pack-contract-v1.md)
//	manifests/*.yaml     optional: raw multi-doc YAML manifests
//	kustomization.yaml   optional: a kustomize overlay rooted at the pack
//	chart.yaml           optional: a helm chart reference, rendered client-side
//	                      (spec §4: engines receive rendered manifests only;
//	                      helm-controller is not installed in-cluster)
//
// Render precedence for raw manifests: if kustomization.yaml exists at the
// pack root, it is the *sole* source of raw manifests — manifests/ is
// consumed through it (as `resources:`), never walked independently, so
// objects are not double-rendered. Otherwise the Phase 1 behavior (walk
// manifests/*.yaml directly, in sorted filename order) is unchanged.
// chart.yaml helm rendering is orthogonal to this precedence and is always
// appended, regardless of which raw-manifest path was taken.
//
// chart.yaml shape:
//
//	chart: traefik
//	repo: https://traefik.github.io/charts   # or oci://registry/chart
//	version: "34.1.0"
//	releaseName: traefik
//	namespace: traefik
//	values:                                  # chart-level defaults, merged
//	  ...                                     # UNDER user-supplied values
package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// Pack is fetched + validated pack metadata: a local, on-disk directory
// whose pack.cue has already been parsed.
type Pack struct {
	Name    string
	Version string
	Dir     string

	// Description is the pack's optional one-line description (contract v1,
	// Phase 5 P1: pack.cue `description: "…"`). Empty when the pack declares
	// none — packs predating the field load exactly as before. The packs-repo
	// index artifact (P2) and the remote catalog (P6) surface it to users.
	Description string

	// Pinned records the fetch-time pin for cube.lock, in one of:
	//   "git+<sha>"   — git pack refs (this task), the full commit SHA
	//                   resolved via resolveGitPin.
	//   "oci:<digest>" — OCI pack refs (Task 5).
	//   "dir:<dirhash>" — local directory and http/s3 getter refs, which have
	//                   no upstream pin protocol of their own (Task 5).
	// Empty until the relevant task fills it in for that source kind.
	Pinned string

	// Expose is the D11 discoverability contract (Phase 2): parsed from
	// pack.cue's optional expose: block. nil when the pack declares none —
	// packs predating this field, and packs like traefik that expose
	// nothing through themselves, load exactly as before.
	Expose *Expose

	// Images is the pack's declared runtime image list (spec D14, optional
	// pack.cue `images: [...string]`): images the pack pulls at runtime that
	// never appear in its own rendered manifests (e.g. envoy-gateway's
	// dynamically-provisioned proxy image). nil when the pack declares none
	// — packs predating this field load exactly as before. `up`'s lock
	// assembly unions this into Entry.Images so `cube-idp vendor` (Task 6)
	// can bundle it for air-gapped installs.
	Images []string

	// GatewayService is the pack's declared data-plane Service (spec §5.7b,
	// R7b, optional pack.cue `gatewayService: {name, namespace}`). nil when
	// the pack declares none — `up`'s gatewayServiceFQDN then falls back to
	// the <pack>.<pack>.svc convention (traefik: zero migration).
	GatewayService *GatewayService

	// DependsOn is the optional pack.cue dependsOn: pack names this pack
	// needs first — nil when undeclared, packs predating the field load
	// exactly as before; spec 2026-07-19 §3.1.
	DependsOn []string
}

// Rendered is the final set of objects a pack produces for a given set of
// values: raw manifests plus (if the pack has one) a client-side helm
// template render. Task 9 pushes this as an OCI artifact; Task 10
// orchestrates Fetch -> Render -> push -> deliver.
type Rendered struct {
	Name    string
	Version string
	Objects []*unstructured.Unstructured
}

// loadMeta reads and validates pack.cue in dir, returning the pack's
// required name/version metadata (plus the optional expose: block, D11).
func loadMeta(dir string) (*Pack, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pack.cue"))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackCueInvalid, fmt.Sprintf("pack at %s has no pack.cue", dir),
			"every pack needs a pack.cue with at least name and version")
	}
	ctx := cuecontext.New()
	v := ctx.CompileBytes(raw)
	if v.Err() != nil {
		return nil, diag.Wrap(v.Err(), diag.CodePackCueInvalid, "pack.cue does not compile", "fix the CUE syntax")
	}
	p := &Pack{Dir: dir}
	if err := v.LookupPath(cue.ParsePath("name")).Decode(&p.Name); err != nil || p.Name == "" {
		return nil, diag.New(diag.CodePackCueInvalid, "pack.cue is missing 'name'", "add: name: \"<pack-name>\"")
	}
	if err := v.LookupPath(cue.ParsePath("version")).Decode(&p.Version); err != nil || p.Version == "" {
		return nil, diag.New(diag.CodePackCueInvalid, "pack.cue is missing 'version'", "add: version: \"0.1.0\"")
	}
	if dv := v.LookupPath(cue.ParsePath("description")); dv.Exists() {
		if err := dv.Decode(&p.Description); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("pack.cue description: in %s is invalid", dir),
				`description: must be a string, e.g. description: "in-cluster git server"`)
		}
	}
	expose, err := parseExpose(ctx, v, dir)
	if err != nil {
		return nil, err
	}
	p.Expose = expose

	if iv := v.LookupPath(cue.ParsePath("images")); iv.Exists() {
		if err := iv.Decode(&p.Images); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("pack.cue images: in %s is invalid", dir),
				`images: must be a list of strings, e.g. images: ["envoyproxy/envoy:v1.29"]`)
		}
	}

	if dv := v.LookupPath(cue.ParsePath("dependsOn")); dv.Exists() {
		if err := dv.Decode(&p.DependsOn); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("pack.cue dependsOn: in %s is invalid", dir),
				`dependsOn: must be a list of pack names, e.g. dependsOn: ["floci"]`)
		}
	}

	if gv := v.LookupPath(cue.ParsePath("gatewayService")); gv.Exists() {
		schema := ctx.CompileString(gatewayServiceSchemaCUE)
		unified := schema.Unify(gv)
		if err := unified.Validate(cue.Concrete(true)); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("gatewayService: block in %s/pack.cue is invalid", dir),
				`gatewayService needs both name and namespace, e.g. gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}`)
		}
		var gs GatewayService
		if err := unified.Decode(&gs); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("gatewayService: block in %s/pack.cue is invalid", dir),
				"gatewayService.name and .namespace must be strings")
		}
		p.GatewayService = &gs
	}
	return p, nil
}

// gatewayServiceSchemaCUE is the R7b gatewayService: block schema (spec
// §5.7b): both name and namespace are required — a pack that declares this
// block is naming a specific in-cluster Service, and a half-declared one
// (e.g. name without namespace) can't resolve to anything, so it's rejected
// rather than silently partially applied.
const gatewayServiceSchemaCUE = `{ name: string, namespace: string }`

// exposeSchemaCUE is the D11 expose: block schema (checkpoint 0.8): an
// optional set of URLs (may contain the ${GATEWAY_HOST} substitution
// token), an optional credential Secret reference, and optional implied
// login fields (e.g. ArgoCD's implicit "admin" username). Every field is
// itself optional — only authSecretRef's own namespace/name are required,
// so a pack that declares a credential can't declare it half-broken.
const exposeSchemaCUE = `
{
	urls?: [...string]
	authSecretRef?: {
		namespace: string
		name:      string
	}
	impliedFields?: [string]: string
}
`

// parseExpose reads the optional expose: block out of an already-compiled
// pack.cue value v (sharing ctx, the same *cue.Context v was compiled
// with — Unify requires operands from one context). A pack.cue with no
// expose: field returns (nil, nil): TestExposeIsOptional guards that
// packs predating this field keep loading exactly as before. A malformed
// block (e.g. an authSecretRef missing its name) is rejected as
// CUBE-4011, never silently dropped.
func parseExpose(ctx *cue.Context, v cue.Value, dir string) (*Expose, error) {
	ev := v.LookupPath(cue.ParsePath("expose"))
	if !ev.Exists() {
		return nil, nil
	}
	schema := ctx.CompileString(exposeSchemaCUE)
	unified := schema.Unify(ev)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return nil, diag.Wrap(err, diag.CodePackExposeInv,
			fmt.Sprintf("expose: block in %s/pack.cue is invalid", dir),
			"fix the expose block — see the pack authoring docs for the shape")
	}

	e := &Expose{}
	if uv := unified.LookupPath(cue.ParsePath("urls")); uv.Exists() {
		if err := uv.Decode(&e.URLs); err != nil {
			return nil, diag.Wrap(err, diag.CodePackExposeInv,
				fmt.Sprintf("expose.urls in %s/pack.cue is invalid", dir), "expose.urls must be a list of strings")
		}
	}
	if rv := unified.LookupPath(cue.ParsePath("authSecretRef")); rv.Exists() {
		var ref SecretRef
		if err := rv.Decode(&ref); err != nil {
			return nil, diag.Wrap(err, diag.CodePackExposeInv,
				fmt.Sprintf("expose.authSecretRef in %s/pack.cue is invalid", dir),
				"expose.authSecretRef needs both namespace and name")
		}
		e.AuthSecretRef = &ref
	}
	if fv := unified.LookupPath(cue.ParsePath("impliedFields")); fv.Exists() {
		if err := fv.Decode(&e.ImpliedFields); err != nil {
			return nil, diag.Wrap(err, diag.CodePackExposeInv,
				fmt.Sprintf("expose.impliedFields in %s/pack.cue is invalid", dir),
				"expose.impliedFields must be a map of string to string")
		}
	}
	return e, nil
}

// validateValues unifies user values with #Values (if declared in
// pack.cue) and returns the concrete, defaulted value map. Packs without a
// #Values schema accept any values map unchecked.
func (p *Pack) validateValues(values map[string]any) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "pack.cue"))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackCueInvalid, fmt.Sprintf("pack at %s has no pack.cue", p.Dir),
			"every pack needs a pack.cue with at least name and version")
	}
	ctx := cuecontext.New()
	root := ctx.CompileBytes(raw)
	if root.Err() != nil {
		return nil, diag.Wrap(root.Err(), diag.CodePackCueInvalid, "pack.cue does not compile", "fix the CUE syntax")
	}
	schema := root.LookupPath(cue.ParsePath("#Values"))
	if !schema.Exists() {
		return values, nil
	}
	unified := schema.Unify(ctx.Encode(values))
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return nil, diag.Wrap(err, diag.CodePackValuesInv,
			fmt.Sprintf("values for pack %q do not match its #Values schema", p.Name),
			"compare your values with the pack's pack.cue #Values definition")
	}
	var out map[string]any
	if err := unified.Decode(&out); err != nil {
		return nil, diag.Wrap(err, diag.CodePackValuesInv, "cannot decode validated values", "simplify the values to plain YAML types")
	}
	return out, nil
}
