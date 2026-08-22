package v1alpha1_test

import (
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// packConfig wraps pack entries in an otherwise valid document, so a failure
// can only come from spec.packs.
func packConfig(packs ...v1alpha1.PackSpec) *v1alpha1.Config {
	cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Packs: packs}}
	cfg.Name = "dev"
	cfg.Default()
	return cfg
}

func raw(s string) *runtime.RawExtension { return &runtime.RawExtension{Raw: []byte(s)} }

const inlineConfigMap = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"c"}}`

func TestPacksValid(t *testing.T) {
	tests := []struct {
		name  string
		packs []v1alpha1.PackSpec
	}{
		{
			name:  "absent packs",
			packs: nil,
		},
		{
			name:  "minimal entry is just a packRef",
			packs: []v1alpha1.PackSpec{{PackRef: "./packs/hello"}},
		},
		{
			name: "every field set",
			packs: []v1alpha1.PackSpec{{
				ID:        "monitoring-prod",
				PackRef:   "oci://registry.example/monitoring:2.1.0",
				ValuesRef: "https://example.com/values.yaml",
				Values:    raw(`{"replicas":"3"}`),
				ExternalManifests: []v1alpha1.ExternalManifest{
					{Ref: "https://example.com/crds.yaml", Lifecycle: v1alpha1.LifecyclePre},
					{Manifest: raw(inlineConfigMap), Lifecycle: v1alpha1.LifecycleWith},
				},
				DependsOn: []string{"cert-manager"},
			}},
		},
		{
			// Distinct explicit IDs are how two copies of one pack coexist.
			name: "same packRef twice with distinct ids",
			packs: []v1alpha1.PackSpec{
				{ID: "monitoring-a", PackRef: "oci://registry.example/monitoring:2.1.0"},
				{ID: "monitoring-b", PackRef: "oci://registry.example/monitoring:2.1.0"},
			},
		},
		{
			// The effective ID is not known without resolving the pack, so
			// this layer cannot object — internal/pack does.
			name: "no ids at all",
			packs: []v1alpha1.PackSpec{
				{PackRef: "./a"},
				{PackRef: "./b"},
			},
		},
		{
			// A scheme typo is internal/ref's to report at resolution.
			name:  "a malformed scheme is not this layer's business",
			packs: []v1alpha1.PackSpec{{PackRef: "oci//missing-colon"}},
		},
		{
			name:  "dependsOn a name that this layer cannot resolve",
			packs: []v1alpha1.PackSpec{{PackRef: "./a", DependsOn: []string{"something-else"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if errs := packConfig(tt.packs...).Validate(); len(errs) != 0 {
				t.Errorf("Validate() = %v, want no errors", errs)
			}
		})
	}
}

func TestPacksInvalid(t *testing.T) {
	tests := []struct {
		name      string
		packs     []v1alpha1.PackSpec
		wantField string
	}{
		{
			name:      "packRef missing",
			packs:     []v1alpha1.PackSpec{{ID: "a"}},
			wantField: "spec.packs[0].packRef",
		},
		{
			name:      "packRef contains whitespace",
			packs:     []v1alpha1.PackSpec{{PackRef: "./two words"}},
			wantField: "spec.packs[0].packRef",
		},
		{
			name:      "valuesRef contains whitespace",
			packs:     []v1alpha1.PackSpec{{PackRef: "./a", ValuesRef: "./v a.yaml"}},
			wantField: "spec.packs[0].valuesRef",
		},
		{
			name:      "id is not a DNS label",
			packs:     []v1alpha1.PackSpec{{ID: "Not A Label", PackRef: "./a"}},
			wantField: "spec.packs[0].id",
		},
		{
			name: "duplicate explicit ids",
			packs: []v1alpha1.PackSpec{
				{ID: "same", PackRef: "./a"},
				{ID: "same", PackRef: "./b"},
			},
			wantField: "spec.packs[1].id",
		},
		{
			name:      "dependsOn itself by explicit id",
			packs:     []v1alpha1.PackSpec{{ID: "a", PackRef: "./a", DependsOn: []string{"a"}}},
			wantField: "spec.packs[0].dependsOn[0]",
		},
		{
			name:      "empty dependsOn entry",
			packs:     []v1alpha1.PackSpec{{PackRef: "./a", DependsOn: []string{"  "}}},
			wantField: "spec.packs[0].dependsOn[0]",
		},
		{
			name: "external manifest with neither ref nor manifest",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Lifecycle: v1alpha1.LifecycleWith}}}},
			wantField: "spec.packs[0].externalManifests[0]",
		},
		{
			name: "external manifest with both ref and manifest",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{
					Ref: "./crds.yaml", Manifest: raw(inlineConfigMap), Lifecycle: v1alpha1.LifecycleWith,
				}}}},
			wantField: "spec.packs[0].externalManifests[0]",
		},
		{
			name: "inline manifest without apiVersion",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Manifest: raw(`{"kind":"ConfigMap"}`)}}}},
			wantField: "spec.packs[0].externalManifests[0].manifest.apiVersion",
		},
		{
			name: "inline manifest without kind",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Manifest: raw(`{"apiVersion":"v1"}`)}}}},
			wantField: "spec.packs[0].externalManifests[0].manifest.kind",
		},
		{
			name: "inline manifest is not an object",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Manifest: raw(`"just a string"`)}}}},
			wantField: "spec.packs[0].externalManifests[0].manifest",
		},
		{
			name: "external manifest ref contains whitespace",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Ref: "./two words.yaml"}}}},
			wantField: "spec.packs[0].externalManifests[0].ref",
		},
		{
			name: "unknown lifecycle",
			packs: []v1alpha1.PackSpec{{PackRef: "./a",
				ExternalManifests: []v1alpha1.ExternalManifest{{Ref: "./crds.yaml", Lifecycle: "post"}}}},
			wantField: "spec.packs[0].externalManifests[0].lifecycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := packConfig(tt.packs...).Validate()
			if len(errs) == 0 {
				t.Fatalf("Validate() = no errors, want one on %s", tt.wantField)
			}
			var fields []string
			for _, e := range errs {
				fields = append(fields, e.Field)
			}
			if !slices.Contains(fields, tt.wantField) {
				t.Errorf("Validate() reported %v, want an error on %s", fields, tt.wantField)
			}
		})
	}
}

// Every problem is reported in one run, not one per re-run.
func TestPacksReportEveryProblem(t *testing.T) {
	errs := packConfig(
		v1alpha1.PackSpec{ID: "BAD ID"},
		v1alpha1.PackSpec{PackRef: "./a",
			ExternalManifests: []v1alpha1.ExternalManifest{{Lifecycle: "post"}}},
	).Validate()

	if len(errs) < 3 {
		t.Errorf("Validate() = %d errors (%v), want the id, the missing packRef, "+
			"the empty external manifest and the bad lifecycle all reported", len(errs), errs)
	}
	if strings.Contains(errs.ToAggregate().Error(), "spec.packs[1].packRef") {
		t.Errorf("Validate() = %v, want no packRef error on the entry that has one", errs)
	}
}

// Lifecycle defaults to "with"; Default is idempotent, as the loader relies on.
func TestPackDefaults(t *testing.T) {
	cfg := packConfig(v1alpha1.PackSpec{PackRef: "./a",
		ExternalManifests: []v1alpha1.ExternalManifest{
			{Ref: "./crds.yaml"},
			{Manifest: raw(inlineConfigMap), Lifecycle: v1alpha1.LifecyclePre},
		}})

	got := cfg.Spec.Packs[0].ExternalManifests
	if got[0].Lifecycle != v1alpha1.LifecycleWith {
		t.Errorf("externalManifests[0].lifecycle = %q, want %q", got[0].Lifecycle, v1alpha1.LifecycleWith)
	}
	if got[1].Lifecycle != v1alpha1.LifecyclePre {
		t.Errorf("externalManifests[1].lifecycle = %q, want it left alone", got[1].Lifecycle)
	}

	cfg.Default()
	if cfg.Spec.Packs[0].ExternalManifests[1].Lifecycle != v1alpha1.LifecyclePre {
		t.Error("Default() is not idempotent")
	}
}

// The effective ID is never defaulted here: it needs the pack's own name,
// which needs I/O this layer must not do.
func TestPackIDIsNotDefaulted(t *testing.T) {
	cfg := packConfig(v1alpha1.PackSpec{PackRef: "./hello"})

	if got := cfg.Spec.Packs[0].ID; got != "" {
		t.Errorf("spec.packs[0].id = %q, want it left empty for internal/pack to derive", got)
	}
}
