package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestEffectiveValuesNoRefPassesInlineThrough(t *testing.T) {
	inline := map[string]any{"replicas": 3}
	got, pin, err := EffectiveValues(context.Background(), "", inline, t.TempDir())
	if err != nil || pin != "" {
		t.Fatalf("pin=%q err=%v", pin, err)
	}
	if !reflect.DeepEqual(got, inline) {
		t.Fatalf("got %#v", got)
	}
}

func TestEffectiveValuesMergesInlineOverFetched(t *testing.T) {
	f := filepath.Join(t.TempDir(), "base.yaml")
	if err := os.WriteFile(f, []byte("replicas: 1\nimage:\n  tag: v1\nextra: {a: 1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inline := map[string]any{"replicas": 3, "extra": nil} // override + RFC7386 delete
	got, pin, err := EffectiveValues(context.Background(), f, inline, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin == "" {
		t.Fatal("expected values pin")
	}
	want := map[string]any{"replicas": 3, "image": map[string]any{"tag": "v1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want) // ints, not float64 (normalizeIntegral)
	}
}

// Ref-only (valuesRef set, NO inline values) is the shape cube.lock records a
// valuesPin for, so the fetched base must survive intact. Regression: an
// absent inline map marshals to the JSON literal `null`, and RFC 7386 says a
// null patch REPLACES the whole document — which silently rendered pure chart
// defaults while the lock still claimed the pin had been applied.
func TestEffectiveValuesRefOnlyKeepsFetchedBase(t *testing.T) {
	f := filepath.Join(t.TempDir(), "base.yaml")
	if err := os.WriteFile(f, []byte("replicas: 2\nimage:\n  tag: v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, pin, err := EffectiveValues(context.Background(), f, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin == "" {
		t.Fatal("expected values pin")
	}
	want := map[string]any{"replicas": 2, "image": map[string]any{"tag": "v1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want) // ints, not float64 (normalizeIntegral)
	}
}

func TestEffectiveValuesWrapsFetchFailure(t *testing.T) {
	_, _, err := EffectiveValues(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"), nil, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackValuesRefFetch {
		t.Fatalf("err = %v, want CUBE-4021", err)
	}
}

// RenderResolved: valuesRef on a chartless pack is the values rule, checked
// BEFORE any network fetch (chartlessness is known once the pack is local).
// testdata/demo is the repo's chartless fixture (pack.cue + manifests/cm.yaml,
// no chart.yaml) — the same one TestRenderWithValuesOnChartlessPackIsCube4016
// uses for the inline-values half of the stone.
func TestRenderResolvedChartlessValuesRef(t *testing.T) {
	pk, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pref := config.PackRef{Ref: "testdata/demo", ValuesRef: "github.com/acme/values//x@v1"}
	_, _, err = RenderResolved(context.Background(), pk, pref, config.GatewaySpec{}, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackValuesChartless {
		t.Fatalf("err = %v, want CUBE-4016", err)
	}
}
