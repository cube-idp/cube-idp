package pack

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// asJSON renders merged values deterministically: encoding/json sorts map keys,
// so the comparison is on content rather than on iteration order.
func asJSON(t *testing.T, values map[string]any) string {
	t.Helper()
	out, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("json.Marshal(%v) = error %v", values, err)
	}
	return string(out)
}

const baseValuesDoc = `image: nginx
replicas: 2
tls:
  enabled: true
  ca: internal
ports:
  - 80
  - 443
`

// The merge is RFC 7386: null deletes, mappings merge, everything else —
// arrays included — replaces.
func TestInstanceValues(t *testing.T) {
	tests := []struct {
		name      string
		valuesRef string
		inline    string
		want      string
		wantCode  cubeerr.Code
	}{
		{
			name: "no values at all leaves the pack with its own defaults",
			want: "null",
		},
		{
			name:      "valuesRef alone is the base",
			valuesRef: "./values.yaml",
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:   "inline alone needs no base",
			inline: `{"image":"redis"}`,
			want:   `{"image":"redis"}`,
		},
		{
			name:      "inline merges over the base",
			valuesRef: "./values.yaml",
			inline:    `{"replicas":3}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":3,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "nested mappings merge rather than replace",
			valuesRef: "./values.yaml",
			inline:    `{"tls":{"ca":"public"}}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"ca":"public","enabled":true}}`,
		},
		{
			name:      "null deletes a key",
			valuesRef: "./values.yaml",
			inline:    `{"replicas":null,"ports":null}`,
			want:      `{"image":"nginx","tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "null deletes a nested key",
			valuesRef: "./values.yaml",
			inline:    `{"tls":{"ca":null}}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"enabled":true}}`,
		},
		{
			name:      "null on an absent key is not an error",
			valuesRef: "./values.yaml",
			inline:    `{"absent":null}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "arrays replace wholesale",
			valuesRef: "./values.yaml",
			inline:    `{"ports":[8080]}`,
			want:      `{"image":"nginx","ports":[8080],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "a scalar replaces a mapping",
			valuesRef: "./values.yaml",
			inline:    `{"tls":"off"}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":"off"}`,
		},
		{
			name:      "a mapping replaces a scalar",
			valuesRef: "./values.yaml",
			inline:    `{"image":{"repo":"nginx","tag":"1.27"}}`,
			want:      `{"image":{"repo":"nginx","tag":"1.27"},"ports":[80,443],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "an empty patch changes nothing",
			valuesRef: "./values.yaml",
			inline:    `{}`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},
		{
			name:      "a null patch changes nothing",
			valuesRef: "./values.yaml",
			inline:    `null`,
			want:      `{"image":"nginx","ports":[80,443],"replicas":2,"tls":{"ca":"internal","enabled":true}}`,
		},

		{
			name:      "several documents are ambiguous",
			valuesRef: "./multi.yaml",
			wantCode:  CodeValuesDocument,
		},
		{
			name:      "an empty document is not a values mapping",
			valuesRef: "./empty.yaml",
			wantCode:  CodeValuesDocument,
		},
		{
			name:      "a list is not a values mapping",
			valuesRef: "./list.yaml",
			wantCode:  CodeValuesDocument,
		},
		{
			name:      "a scalar is not a values mapping",
			valuesRef: "./scalar.yaml",
			wantCode:  CodeValuesDocument,
		},
		{
			name:     "inline values that are a list are not a patch",
			inline:   `[1,2]`,
			wantCode: CodeValuesDocument,
		},
		{
			name:     "inline values that are a scalar are not a patch",
			inline:   `"nginx"`,
			wantCode: CodeValuesDocument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{docs: map[string]string{
				"./values.yaml": baseValuesDoc,
				"./multi.yaml":  "a: 1\n---\nb: 2\n",
				"./empty.yaml":  "# only a comment\n",
				"./list.yaml":   "- a\n- b\n",
				"./scalar.yaml": "nginx\n",
			}}

			got, err := instanceValues(t.Context(), instanceSpec(tt.valuesRef, tt.inline), resolver.resolve)
			if tt.wantCode != "" {
				wantCoded(t, err, tt.wantCode)
				return
			}
			if err != nil {
				t.Fatalf("instanceValues(%q, %s) = error %v, want %s", tt.valuesRef, tt.inline, err, tt.want)
			}
			if json := asJSON(t, got); json != tt.want {
				t.Errorf("instanceValues(%q, %s) = %s, want %s", tt.valuesRef, tt.inline, json, tt.want)
			}
		})
	}
}

// A resolver failure is the resolver's error, wrapped with the field it came
// from and never re-tagged as a pack code.
func TestInstanceValuesResolverFailurePropagates(t *testing.T) {
	resolver := &stubResolver{}

	_, err := instanceValues(t.Context(), instanceSpec("./gone.yaml", ""), resolver.resolve)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("instanceValues() = error %v, want the resolver's own error", err)
	}
	var coded *cubeerr.Coded
	if errors.As(err, &coded) {
		t.Errorf("resolver failure was re-tagged as %s, want it carried through as-is", coded.Code)
	}
}

// Whole numbers survive the JSON round trip as integers. YAML and JSON both
// decode every number as a float64, and a float does not satisfy a CUE `int`
// field — so without this a correctly written `replicas: 3` would be reported
// as a #Values violation the author never committed.
func TestInstanceValuesKeepsWholeNumbersIntegral(t *testing.T) {
	resolver := &stubResolver{docs: map[string]string{
		"./nums.yaml": "replicas: 3\nratio: 1.5\nports:\n  - 80\nlimits:\n  cpu: 2\n",
	}}

	values, err := instanceValues(t.Context(), instanceSpec("./nums.yaml", `{"replicas":4}`), resolver.resolve)
	if err != nil {
		t.Fatalf("instanceValues() = error %v, want values", err)
	}

	if got, want := values["replicas"], int64(4); got != want {
		t.Errorf("replicas = %[1]v (%[1]T), want %[2]v (%[2]T)", got, want)
	}
	if got, want := values["ratio"], 1.5; got != want {
		t.Errorf("ratio = %[1]v (%[1]T), want %[2]v (%[2]T)", got, want)
	}
	if got, want := values["ports"].([]any)[0], int64(80); got != want {
		t.Errorf("ports[0] = %[1]v (%[1]T), want %[2]v (%[2]T)", got, want)
	}
	if got, want := values["limits"].(map[string]any)["cpu"], int64(2); got != want {
		t.Errorf("limits.cpu = %[1]v (%[1]T), want %[2]v (%[2]T)", got, want)
	}
}

// The caller's inline values are opaque bytes, and a merged sub-mapping must be
// the merge's own — not an alias of the document it came from, which a second
// merge would then see already edited.
func TestInstanceValuesDoesNotMutateItsInputs(t *testing.T) {
	resolver := &stubResolver{docs: map[string]string{"./values.yaml": baseValuesDoc}}
	spec := instanceSpec("./values.yaml", `{"tls":{"ca":"public"}}`)
	raw := string(spec.Values.Raw)

	merged, err := instanceValues(t.Context(), spec, resolver.resolve)
	if err != nil {
		t.Fatalf("instanceValues() = error %v, want values", err)
	}
	merged["image"] = "mutated"
	merged["tls"].(map[string]any)["enabled"] = "mutated"

	if got := string(spec.Values.Raw); got != raw {
		t.Errorf("inline values = %s, want %s", got, raw)
	}
	if got, want := spec.Values.Raw, []byte(`{"tls":{"ca":"public"}}`); string(got) != string(want) {
		t.Errorf("inline patch = %s, want %s", got, want)
	}
	again, err := instanceValues(t.Context(), spec, resolver.resolve)
	if err != nil {
		t.Fatalf("instanceValues() = error %v, want values", err)
	}
	if again["image"] != "nginx" {
		t.Errorf("second merge saw image = %v, want nginx", again["image"])
	}
	if got := again["tls"].(map[string]any)["enabled"]; got != true {
		t.Errorf("second merge saw tls.enabled = %v, want true", got)
	}
}
