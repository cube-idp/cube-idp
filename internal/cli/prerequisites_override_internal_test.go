package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/gateway"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ref"
)

// The override fixtures: a conforming pack of each flavor an override may
// take. They are written to disk rather than served from an in-memory FS
// because the override path is exactly the one that goes through the
// reference grammar, and the local backend reads real files.
const (
	rawPackCUE = `name: "team-prereq"
version: "0.1.0"
type: "raw"
namespace: "team-system"
`
	helmPackCUE = `name: "team-gateway"
version: "1.2.3"
type: "helm"
namespace: "team-system"
chart: {
	kind:    "oci"
	url:     "oci://example.test/charts/team-gateway"
	version: "1.2.3"
}
`
	// The same helm pack with no namespace: its CRs would have no target.
	helmPackNoNamespaceCUE = `name: "team-gateway"
version: "1.2.3"
type: "helm"
chart: {
	kind:    "oci"
	url:     "oci://example.test/charts/team-gateway"
	version: "1.2.3"
}
`
	rawPackManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: team-config
data:
  hello: world
`
)

// writePack writes a pack directory and returns the file:// reference that
// resolves it. An absolute path is not a reference on its own — the grammar
// accepts ./relative or file:///absolute — and file:// is what a temp dir can
// be spelled as.
func writePack(t *testing.T, cue string, manifests map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pack.MetadataFile), []byte(cue), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range manifests {
		manifestDir := filepath.Join(dir, pack.ManifestsDir)
		if err := os.MkdirAll(manifestDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return "file://" + dir
}

// resolveOne resolves a single-entry list and returns the unit it produced.
func resolveOne(t *testing.T, entry v1alpha1.PrerequisiteSpec) unitSpec {
	t.Helper()
	return resolveSpecs(t, []v1alpha1.PrerequisiteSpec{entry})[0]
}

// TestOverrideRaw: a raw override waits the kind-set and is not stamped —
// the pack contract already applied the namespace it declares.
func TestOverrideRaw(t *testing.T) {
	t.Parallel()
	packRef := writePack(t, rawPackCUE, map[string]string{"cm.yaml": rawPackManifest})

	spec := resolveOne(t, v1alpha1.PrerequisiteSpec{Name: "team-prereq", Ref: packRef})
	if got := flavorOf(spec); got != "raw" {
		t.Errorf("flavor = %s, want raw", got)
	}
	want := []string{"ConfigMap team-system/team-config"}
	if got := descriptors(spec.objs); !slices.Equal(got, want) {
		t.Errorf("objects = %v, want %v", got, want)
	}
}

// TestOverrideHelm: a helm override renders the CR pair the gateway
// predicate judges, so it becomes a CR unit, stamped with the namespace its
// pack declares — a helm render is the only one that comes back
// namespace-less.
func TestOverrideHelm(t *testing.T) {
	t.Parallel()
	packRef := writePack(t, helmPackCUE, nil)

	spec := resolveOne(t, v1alpha1.PrerequisiteSpec{Name: "team-gateway", Ref: packRef})
	if got := flavorOf(spec); got != "cr" {
		t.Fatalf("flavor = %s, want cr", got)
	}
	for _, o := range spec.objs {
		if o.GetNamespace() != "team-system" {
			t.Errorf("%s landed in namespace %q, want team-system", o.GetKind(), o.GetNamespace())
		}
	}
	// An entry that is not the gateway unit gets the domain predicate
	// itself: the cube-authored Gateway is not part of this unit, so
	// nothing needs to be excused from it.
	if _, _, err := spec.judge(gateway.GatewayObject(testDomain)); err == nil {
		t.Error("a foreign helm override's judge accepted the cube Gateway; it should not recognize it")
	}
}

// TestOverrideHelmWithoutNamespace: rejected at the edge rather than stamped
// with a guess — CRs sent to "default" would install a prerequisite where
// nobody declared it and nobody looks. Uncoded: the CLI originates no code
// catalog.
func TestOverrideHelmWithoutNamespace(t *testing.T) {
	t.Parallel()
	packRef := writePack(t, helmPackNoNamespaceCUE, nil)

	_, err := prerequisiteSpecs(t.Context(), prereqInputs{
		units:  []v1alpha1.PrerequisiteSpec{{Name: "team-gateway", Ref: packRef}},
		domain: testDomain,
	})
	if err == nil {
		t.Fatal("a helm override with no namespace resolved; want a rejection")
	}
	if !strings.Contains(err.Error(), "no namespace") {
		t.Errorf("error = %v, want it to name the missing namespace", err)
	}
}

// TestOverrideKeepsGatewayObjectByName: the emitted Gateway is name-selected
// domain behavior — an entry named traefik-gateway gets it whatever pack it
// points at, and its judge must be the composed one, or bootstrap would kill
// the run on the first poll with CUBE-GWY-003 for an object the unit is
// supposed to carry.
func TestOverrideKeepsGatewayObjectByName(t *testing.T) {
	t.Parallel()
	packRef := writePack(t, helmPackCUE, nil)

	spec := resolveOne(t, v1alpha1.PrerequisiteSpec{Name: v1alpha1.PrerequisiteTraefikGateway, Ref: packRef})
	if !carriesGateway(spec.objs) {
		t.Fatal("a traefik-gateway entry pointing at another pack lost the cube Gateway")
	}
	ok, _, err := spec.judge(gateway.GatewayObject(testDomain))
	if err != nil || !ok {
		t.Errorf("judge(Gateway) = (%v, %v), want (true, nil)", ok, err)
	}
}

// TestOverrideRenamedUnitGetsNoGateway: a list that renames the gateway unit
// gets no listener, which the list author owns.
func TestOverrideRenamedUnitGetsNoGateway(t *testing.T) {
	t.Parallel()
	packRef := writePack(t, helmPackCUE, nil)

	spec := resolveOne(t, v1alpha1.PrerequisiteSpec{Name: "team-gateway", Ref: packRef})
	if carriesGateway(spec.objs) {
		t.Error("a renamed gateway unit was given the cube Gateway")
	}
}

// TestOverrideUnresolvableRef: resolution failures stay the reference
// grammar's, coded by internal/ref and passed through untouched.
func TestOverrideUnresolvableRef(t *testing.T) {
	t.Parallel()
	_, err := prerequisiteSpecs(t.Context(), prereqInputs{
		units:  []v1alpha1.PrerequisiteSpec{{Name: "team-prereq", Ref: "not-a-reference"}},
		domain: testDomain,
	})
	assertCode(t, err, ref.CodeMalformedRef)
}

// TestCheckNoPackPrerequisites: a pack's own lifecycle:pre manifests are a
// different mechanism, and dropping declared content silently is the wrong
// default. No render path fills the field today, so the guard is checked
// where it is decided rather than through a pack that cannot express it.
func TestCheckNoPackPrerequisites(t *testing.T) {
	t.Parallel()
	if err := checkNoPackPrerequisites("team-prereq", "team-pack", pack.RenderPlan{}); err != nil {
		t.Fatalf("an empty prerequisite group was rejected: %v", err)
	}
	plan := pack.RenderPlan{Prerequisites: []*unstructured.Unstructured{{}, {}}}
	err := checkNoPackPrerequisites("team-prereq", "team-pack", plan)
	if err == nil {
		t.Fatal("declared lifecycle:pre manifests were accepted; want a rejection")
	}
	for _, want := range []string{"team-prereq", "team-pack", "2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// carriesGateway reports whether the objects include the cube-authored
// Gateway.
func carriesGateway(objs []*unstructured.Unstructured) bool {
	for _, o := range objs {
		if o.GetAPIVersion() == gateway.GatewayAPIVersion && o.GetKind() == "Gateway" {
			return true
		}
	}
	return false
}
