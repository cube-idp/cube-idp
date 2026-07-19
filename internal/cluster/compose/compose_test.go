package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestMergeVectors(t *testing.T) {
	cases := []struct {
		name              string
		base, patch, want map[string]any
	}{
		{"maps deep-merge",
			map[string]any{"networking": map[string]any{"ipFamily": "ipv4", "podSubnet": "10.244.0.0/16"}},
			map[string]any{"networking": map[string]any{"ipFamily": "dual"}},
			map[string]any{"networking": map[string]any{"ipFamily": "dual", "podSubnet": "10.244.0.0/16"}}},
		{"lists replace wholesale",
			map[string]any{"nodes": []any{map[string]any{"role": "control-plane"}, map[string]any{"role": "worker"}}},
			map[string]any{"nodes": []any{map[string]any{"role": "control-plane", "labels": map[string]any{"tier": "system"}}}},
			map[string]any{"nodes": []any{map[string]any{"role": "control-plane", "labels": map[string]any{"tier": "system"}}}}},
		{"null deletes a key",
			map[string]any{"featureGates": map[string]any{"A": true}, "name": "x"},
			map[string]any{"featureGates": nil},
			map[string]any{"name": "x"}},
		{"empty patch is identity",
			map[string]any{"a": float64(1)}, map[string]any{}, map[string]any{"a": float64(1)}},
		{"empty base takes patch",
			map[string]any{}, map[string]any{"a": "b"}, map[string]any{"a": "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Merge(tc.base, tc.patch)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestResolveEmptyRef(t *testing.T) {
	m, _, err := Resolve(context.Background(), "", t.TempDir())
	if err != nil || m == nil || len(m) != 0 {
		t.Fatalf("got %v, %v; want empty map, nil", m, err)
	}
}

func TestResolveLocalFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "base.yaml")
	os.WriteFile(p, []byte("featureGates:\n  MyFeature: true\n"), 0o644)
	m, _, err := Resolve(context.Background(), p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"featureGates": map[string]any{"MyFeature": true}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %#v", m)
	}
}

func TestResolveFetchErrorWraps1005(t *testing.T) {
	_, _, err := Resolve(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"), t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeProviderConfigRefFetch {
		t.Fatalf("want CUBE-1005, got %v", err)
	}
}

func TestResolveNonMappingDoc(t *testing.T) {
	p := filepath.Join(t.TempDir(), "list.yaml")
	os.WriteFile(p, []byte("- just\n- a list\n"), 0o644)
	_, _, err := Resolve(context.Background(), p, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeProviderConfigRefFetch {
		t.Fatalf("want CUBE-1005, got %v", err)
	}
}

func TestResolveReturnsPin(t *testing.T) {
	f := filepath.Join(t.TempDir(), "base.yaml")
	if err := os.WriteFile(f, []byte("kind: Cluster\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, pin, err := Resolve(context.Background(), f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m["kind"] != "Cluster" || pin == "" {
		t.Fatalf("m=%v pin=%q", m, pin)
	}
	// empty ref: no pin, empty non-nil map (existing contract preserved)
	m, pin, err = Resolve(context.Background(), "", t.TempDir())
	if err != nil || pin != "" || len(m) != 0 || m == nil {
		t.Fatalf("empty ref: m=%v pin=%q err=%v", m, pin, err)
	}
}

func TestComposeRefPlusForProvider(t *testing.T) {
	p := filepath.Join(t.TempDir(), "base.yaml")
	os.WriteFile(p, []byte("networking:\n  ipFamily: ipv4\n  disableDefaultCNI: true\n"), 0o644)
	m, _, err := Compose(context.Background(), p, map[string]any{
		"networking": map[string]any{"ipFamily": "dual"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"networking": map[string]any{"ipFamily": "dual", "disableDefaultCNI": true}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %#v", m)
	}
}
