package refval

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestResolveEmptyRef(t *testing.T) {
	m, pin, err := Resolve(context.Background(), "", t.TempDir())
	if err != nil || pin != "" || m == nil || len(m) != 0 {
		t.Fatalf("got m=%v pin=%q err=%v; want empty non-nil map, no pin", m, pin, err)
	}
}

func TestResolveLocalFile(t *testing.T) {
	f := write(t, "v.yaml", "a:\n  b: 2\n")
	m, pin, err := Resolve(context.Background(), f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin == "" {
		t.Fatal("expected a pin")
	}
	if m["a"].(map[string]any)["b"] != float64(2) { // sigs.k8s.io/yaml JSON typing
		t.Fatalf("m = %#v", m)
	}
}

func TestResolveRejectsNonMapping(t *testing.T) {
	f := write(t, "v.yaml", "- just\n- a\n- list\n")
	if _, _, err := Resolve(context.Background(), f, t.TempDir()); err == nil {
		t.Fatal("expected error for non-mapping document")
	}
}

func TestMergeNullDeletesAndArraysReplace(t *testing.T) {
	base := map[string]any{"keep": 1.0, "drop": 1.0, "arr": []any{1.0, 2.0}, "nest": map[string]any{"x": 1.0}}
	patch := map[string]any{"drop": nil, "arr": []any{9.0}, "nest": map[string]any{"y": 2.0}}
	got, err := Merge(base, patch)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"keep": 1.0, "arr": []any{9.0}, "nest": map[string]any{"x": 1.0, "y": 2.0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if _, still := base["drop"]; !still {
		t.Fatal("Merge mutated its base input")
	}
}

// A nil or empty patch is a no-op, never a wipe: json.Marshal of a nil map is
// the literal `null`, and RFC 7386 reads a null patch as "replace the whole
// document with null". Merge must not inherit that reading.
func TestMergeNilOrEmptyPatchKeepsBase(t *testing.T) {
	want := map[string]any{"keep": 1.0, "nest": map[string]any{"x": 1.0}}
	for _, patch := range []map[string]any{nil, {}} {
		base := map[string]any{"keep": 1.0, "nest": map[string]any{"x": 1.0}}
		got, err := Merge(base, patch)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("patch %#v: got %#v want %#v", patch, got, want)
		}
	}
}

func TestNormalizeIntegral(t *testing.T) {
	in := map[string]any{"r": float64(3), "f": 3.5, "deep": []any{float64(7)}}
	got := NormalizeIntegral(in).(map[string]any)
	if got["r"] != 3 || got["f"] != 3.5 || got["deep"].([]any)[0] != 7 {
		t.Fatalf("got %#v", got)
	}
}
