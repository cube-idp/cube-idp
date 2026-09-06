package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"
	"testing"
	"testing/fstest"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// The sync fixture's identity. These names describe the mirror under testdata,
// which is published as the branch the round-trip syncs from
// (bootstrap_e2e_test.go syncFixture* constants). The same constants are
// asserted against the objects actually delivered into the cluster, so mirror
// and delivered content are checked against one declaration.
const (
	fixtureDir           = "testdata/sync-fixture"
	fixtureNamespaceName = "cube-sync-e2e"
	fixtureConfigMapName = "cube-sync-fixture"
	fixtureDataKey       = "fixture"
	fixtureDataValue     = "cube-idp-e2e-sync"
)

// TestSyncFixtureContract locks the sync fixture to a closed schema. It is
// hermetic and runs in the green gate — deliberately without the CUBE_E2E
// skip its cluster-creating neighbours carry.
//
// It is a guard, NOT the reproduction of
// https://github.com/cube-idp/cube-idp/issues/202, which was reproduced by the
// real round-trip failing against a namespace-less source. What this locks is
// the property whose absence caused it: every namespaced object carries the
// placement the sync wiring does not supply, because
// internal/engine/flux/flux.go:103-111 emits no targetNamespace.
//
// The schema is closed rather than permissive to bound what the e2e's
// delivered-content comparison has to cover — a field outside this set is
// rejected here rather than left to that comparison. It does not tie the
// mirror to the published branch: that tie is the asserted commit pin.
func TestSyncFixtureContract(t *testing.T) {
	assertFixtureFileSet(t)
	assertFixtureNamespace(t, mustLoadFixtureDoc(t, "namespace.yaml"))
	assertFixtureConfigMap(t, mustLoadFixtureDoc(t, "configmap.yaml"))
	assertFixtureKustomization(t, mustLoadFixtureDoc(t, "kustomization.yaml"))
}

// loadFixtureDoc reads one fixture document as a generic mapping, so unknown
// fields are observable rather than dropped by a typed decode. The filesystem
// is injected so the loader's own rejections are testable against synthetic
// input (CLAUDE.md §5 / ARCHITECTURE.md §7).
func loadFixtureDoc(fsys fs.FS, name string) (map[string]any, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", name, err)
	}
	if err := assertSingleDocument(raw, name); err != nil {
		return nil, err
	}
	var doc map[string]any
	// Strict: a duplicate key would otherwise resolve silently to one of two
	// values, and which one is left to the YAML consumer.
	if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse fixture %s: %w", name, err)
	}
	return doc, nil
}

// assertSingleDocument checks the file holds exactly one non-empty YAML
// document. A second document is applied content the key checks never see,
// because a plain unmarshal consumes only the first. Bare separators carry no
// resource and do not count.
func assertSingleDocument(raw []byte, name string) error {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	seen := 0
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("parse fixture %s: %w", name, err)
		}
		if len(doc) > 0 {
			seen++
		}
	}
	if seen != 1 {
		return fmt.Errorf("fixture %s holds %d documents, want exactly 1", name, seen)
	}
	return nil
}

// mustLoadFixtureDoc loads a document from the real mirror or fails the test.
func mustLoadFixtureDoc(t *testing.T, name string) map[string]any {
	t.Helper()
	doc, err := loadFixtureDoc(os.DirFS(fixtureDir), name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return doc
}

// assertFixtureFileSet checks the directory holds exactly the closed file set.
func assertFixtureFileSet(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixture directory: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}
	// The closed file set: the fixture is exactly these three documents, so an
	// added file is a contract violation rather than content the engine would
	// silently sync. Held here, in its only consumer — package-level mutable
	// state is banned (CLAUDE.md §2).
	assertStringsEqual(t, "fixture files", got, []string{"configmap.yaml", "kustomization.yaml", "namespace.yaml"})
}

// assertFixtureNamespace checks the Namespace the fixture brings with it —
// nothing else creates it, since the wiring supplies no placement.
func assertFixtureNamespace(t *testing.T, doc map[string]any) {
	t.Helper()
	assertKeys(t, "namespace.yaml", doc, []string{"apiVersion", "kind", "metadata"})
	assertField(t, "namespace.yaml apiVersion", doc["apiVersion"], "v1")
	assertField(t, "namespace.yaml kind", doc["kind"], "Namespace")
	meta := assertMapping(t, "namespace.yaml metadata", doc["metadata"])
	assertKeys(t, "namespace.yaml metadata", meta, []string{"name"})
	assertField(t, "namespace.yaml metadata.name", meta["name"], fixtureNamespaceName)
}

// assertFixtureConfigMap checks the one namespaced object. Its explicit
// metadata.namespace is the property whose absence broke the old fixture.
func assertFixtureConfigMap(t *testing.T, doc map[string]any) {
	t.Helper()
	assertKeys(t, "configmap.yaml", doc, []string{"apiVersion", "data", "kind", "metadata"})
	assertField(t, "configmap.yaml apiVersion", doc["apiVersion"], "v1")
	assertField(t, "configmap.yaml kind", doc["kind"], "ConfigMap")
	meta := assertMapping(t, "configmap.yaml metadata", doc["metadata"])
	assertKeys(t, "configmap.yaml metadata", meta, []string{"name", "namespace"})
	assertField(t, "configmap.yaml metadata.name", meta["name"], fixtureConfigMapName)
	assertField(t, "configmap.yaml metadata.namespace", meta["namespace"], fixtureNamespaceName)
	data := assertMapping(t, "configmap.yaml data", doc["data"])
	assertKeys(t, "configmap.yaml data", data, []string{fixtureDataKey})
	assertField(t, "configmap.yaml data."+fixtureDataKey, data[fixtureDataKey], fixtureDataValue)
}

// assertFixtureKustomization checks the build configuration is inert: exactly
// the two local resources, and no namespace/patch/transformer field. A
// transformation here would place objects without their own placement, which
// is the very thing this fixture exists to prove is unnecessary.
func assertFixtureKustomization(t *testing.T, doc map[string]any) {
	t.Helper()
	assertKeys(t, "kustomization.yaml", doc, []string{"apiVersion", "kind", "resources"})
	assertField(t, "kustomization.yaml apiVersion", doc["apiVersion"], "kustomize.config.k8s.io/v1beta1")
	assertField(t, "kustomization.yaml kind", doc["kind"], "Kustomization")
	items, ok := doc["resources"].([]any)
	if !ok {
		t.Fatalf("kustomization.yaml resources = %T, want a list", doc["resources"])
	}
	got := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			t.Fatalf("kustomization.yaml resource entry = %T, want string", it)
		}
		got = append(got, s)
	}
	assertStringsEqual(t, "kustomization.yaml resources", got, []string{"./namespace.yaml", "./configmap.yaml"})
}

// assertKeys checks a mapping's key set exactly — the closed-schema check. An
// extra key is a failure, not something ignored, so a field the e2e does not
// compare cannot enter the fixture unnoticed.
func assertKeys(t *testing.T, what string, m map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	assertStringsEqual(t, what+" keys", got, sorted)
}

// assertField checks one scalar field.
func assertField(t *testing.T, what string, got any, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %q", what, got, want)
	}
}

// assertMapping asserts a field is a mapping and returns it.
func assertMapping(t *testing.T, what string, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want a mapping", what, v)
	}
	return m
}

// assertStringsEqual compares two string slices element-wise.
func assertStringsEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// TestSyncFixtureLoaderRejects locks the loader's own rejections. The closed
// schema is only as strong as what reaches it: a second YAML document in a
// declared file is an applied resource the key checks never see, and a
// duplicate key silently resolves to one of two values. Both are source
// content, not byte-only formatting, so both must fail.
func TestSyncFixtureLoaderRejects(t *testing.T) {
	const valid = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\ndata:\n  fixture: v\n"
	tests := []struct {
		name    string
		doc     string
		wantErr bool
	}{
		{name: "single document", doc: valid},
		{name: "trailing separator only", doc: valid + "---\n"},
		{name: "second document appended",
			doc:     valid + "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unexpected\ndata:\n  extra: unreviewed\n",
			wantErr: true},
		{name: "duplicate key",
			doc:     "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\ndata:\n  fixture: discarded\n  fixture: kept\n",
			wantErr: true},
		{name: "sequence, not a mapping", doc: "- a\n- b\n", wantErr: true},
		{name: "malformed", doc: "apiVersion: v1\n\tkind: ConfigMap\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fsys := fstest.MapFS{"doc.yaml": &fstest.MapFile{Data: []byte(tt.doc)}}
			_, err := loadFixtureDoc(fsys, "doc.yaml")
			if tt.wantErr && err == nil {
				t.Error("loadFixtureDoc = nil error, want a rejection")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("loadFixtureDoc = %v, want no error", err)
			}
		})
	}
}
