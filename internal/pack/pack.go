// Package pack defines, loads, validates, and renders packs — the
// self-contained, versioned unit of platform content every later milestone
// consumes. It renders; it never applies. Under delivery-through-engine the
// rendered output reaches a cluster by being written into the source the
// gitops engine watches, which is a later milestone's job. Rendering is a pure
// function of its inputs: this package reads only the injected fs.FS, and
// never the network, the clock, or the environment.
//
// The binding contract is docs/domains/pack.md.
package pack

import (
	"context"
	"fmt"
	"io/fs"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
)

// MetadataFile is the CUE metadata file at the root of every pack.
const MetadataFile = "pack.cue"

// ManifestsDir is the directory a raw pack keeps its manifest files in.
const ManifestsDir = "manifests"

// valuesPath is the CUE path of the closed definition a pack uses to lock
// down, expose, and default its values surface.
const valuesPath = "#Values"

// packSchema is the closed CUE definition every pack.cue must satisfy. It is
// closed on purpose: an undeclared top-level field is a coded error, not
// silently ignored metadata. Definitions such as #Values are not regular
// fields, so a pack may declare them alongside these.
//
// #Pack is a disjunction discriminated on type rather than one flat struct,
// which is what makes "chart is required for helm and forbidden otherwise"
// a schema failure instead of a hand-written check: a chart on a raw pack has
// no field to unify with, and a helm pack without one leaves a required field
// unset. #ExactSemVer is a shape check only — checkChartVersion in helm.go is
// the authority, and when the two disagree the parser wins.
//
// The shared fields live in a hidden _common rather than a #Common definition
// because a definition is closed: `#Common & {type!: ...}` rejects its own
// disjuncts' fields and the schema fails to compile at all. Closedness is not
// lost — #Pack is a definition, so an undeclared top-level field is still an
// error, and a chart on a raw pack still has no field to unify with.
const packSchema = `#ExactSemVer: =~"^[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?(\\+[0-9A-Za-z.-]+)?$"

#ChartRepo: {
	kind!:    "repo"
	url!:     =~"^https?://"
	name!:    string & !=""
	version!: #ExactSemVer
}

#ChartOCI: {
	kind!:    "oci"
	url!:     =~"^oci://"
	version!: #ExactSemVer
	digest?:  =~"^sha256:[a-f0-9]{64}$"
}

_common: {
	name!:      =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	version!:   string & !=""
	namespace?: =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	category?:  string & !=""
}

#Pack: _common & ({
	type!: "raw" | "kustomize"
} | {
	type!:  "helm"
	chart!: #ChartRepo | #ChartOCI
})`

// Type is a pack's render type. It is declared in pack.cue and never sniffed
// from the payload: a payload that disagrees with the declared type is an
// error, not a guess.
type Type string

// The render types a pack may declare.
const (
	TypeRaw       Type = "raw"
	TypeHelm      Type = "helm"
	TypeKustomize Type = "kustomize"
)

// Metadata is the data half of pack.cue: what the pack is, independent of any
// setup that installs it. Name and Version together are the pack's artifact
// identity; instance identity belongs to the setup, not here.
type Metadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    Type   `json:"type"`
	// Namespace, when set, forces every rendered namespaced object into one
	// namespace.
	Namespace string `json:"namespace,omitempty"`
	// Category identifies what a pack is (gateway, engine, ...). It is
	// metadata only and never selects a code path.
	Category string `json:"category,omitempty"`
	// Chart is the chart a helm pack delegates to. It is non-nil exactly
	// when Type is TypeHelm; the schema admits it for no other type.
	Chart *Chart `json:"chart,omitempty"`
}

// Pack is a loaded, validated pack: its metadata plus the filesystem its
// payload is read from. Construct it with Load; the zero value is not usable.
type Pack struct {
	meta   Metadata
	fsys   fs.FS
	values cue.Value // the #Values definition; !Exists() when undeclared
}

// Metadata returns the pack's validated metadata.
func (p *Pack) Metadata() Metadata { return p.meta }

// Load reads pack.cue at the root of fsys, validates it against the pack
// schema, and checks that the payload matches the declared type. fsys is
// rooted at the pack directory — the CLI edge builds it with os.DirFS, so this
// package never resolves a reference or touches the OS filesystem itself. ref
// names the source in error messages only.
func Load(ctx context.Context, fsys fs.FS, ref string) (*Pack, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	src, err := fs.ReadFile(fsys, MetadataFile)
	if err != nil {
		return nil, newSourceUnreadableError(ref, err)
	}

	meta, values, err := decodeMetadata(src)
	if err != nil {
		return nil, err
	}
	if err := checkPayload(fsys, meta.Type); err != nil {
		return nil, err
	}
	return &Pack{meta: meta, fsys: fsys, values: values}, nil
}

// decodeMetadata compiles pack.cue, unifies it with the closed pack schema,
// and returns the decoded metadata plus the pack's #Values definition (an
// invalid cue.Value when the pack declares none).
func decodeMetadata(src []byte) (Metadata, cue.Value, error) {
	cc := cuecontext.New()

	v := cc.CompileBytes(src, cue.Filename(MetadataFile))
	if err := v.Err(); err != nil {
		return Metadata{}, cue.Value{}, newMetadataCompileError(cueDetails(err))
	}

	schema := cc.CompileString(packSchema).LookupPath(cue.ParsePath("#Pack"))
	unified := v.Unify(schema)
	if err := unified.Validate(cue.Concrete(true), cue.Final()); err != nil {
		return Metadata{}, cue.Value{}, newMetadataSchemaError(cueDetails(err))
	}

	var meta Metadata
	if err := unified.Decode(&meta); err != nil {
		return Metadata{}, cue.Value{}, newMetadataSchemaError(cueDetails(err))
	}
	if meta.Chart != nil {
		if err := checkChartVersion(meta.Chart.Version); err != nil {
			return Metadata{}, cue.Value{}, err
		}
	}
	return meta, v.LookupPath(cue.ParsePath(valuesPath)), nil
}

// cueDetails flattens a CUE error into one whose message carries every
// position and sub-error. CUE's own Error() collapses disjunction failures to
// "3 errors in empty disjunction", which tells a user nothing.
func cueDetails(err error) error {
	if err == nil {
		return nil
	}
	//nolint:err113 // a rendered diagnostic, not an error identity: pack's
	// coded error carries identity and this is only its cause detail.
	return fmt.Errorf("%s", cueerrors.Details(err, nil))
}

// checkPayload verifies the payload matches the declared type. Each type has
// one required marker; finding it is cheap, hermetic, and turns v1's silent
// payload sniffing into an explicit up-front error.
func checkPayload(fsys fs.FS, t Type) error {
	switch t {
	case TypeRaw:
		if !dirExists(fsys, ManifestsDir) {
			return newPayloadMismatchError(t, ManifestsDir+"/ directory")
		}
	case TypeKustomize:
		if !anyFileExists(fsys, "kustomization.yaml", "kustomization.yml", "Kustomization") {
			return newPayloadMismatchError(t, "kustomization.yaml")
		}
	case TypeHelm:
		// Inverted on purpose: a helm pack delegates to a chart it does not
		// carry, so its marker is the chart block in pack.cue (the schema's
		// job) plus the *absence* of chart content. Ignoring bundled files
		// instead would ship content this build silently never uses.
		if name, found := anyChartContent(fsys); found {
			return newChartContentError(name)
		}
	}
	return nil
}

// anyChartContent reports the first Helm chart file or directory present at
// the root of fsys, if any. These are the names a chart is recognized by, so
// finding one at a thin pack's root means the author bundled a chart.
func anyChartContent(fsys fs.FS) (string, bool) {
	for _, name := range []string{"Chart.yaml", "Chart.yml", "templates", "charts"} {
		if _, err := fs.Stat(fsys, name); err == nil {
			return name, true
		}
	}
	return "", false
}

func dirExists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.IsDir()
}

func anyFileExists(fsys fs.FS, names ...string) bool {
	for _, name := range names {
		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
