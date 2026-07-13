package pack

import (
	"context"
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestFetchLocalDirAndMetadata(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Version != "0.1.0" {
		t.Fatalf("metadata: %+v", p)
	}
}

func TestFetchUnknownScheme(t *testing.T) {
	_, err := Fetch(context.Background(), "svn://old/school", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4001" {
		t.Fatalf("want CUBE-4001, got %v", err)
	}
}

func TestRenderValidatesValues(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	_, err := p.Render(map[string]any{"replicas": -3})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4002" {
		t.Fatalf("want CUBE-4002, got %v", err)
	}
}

func TestRenderManifests(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	r, err := p.Render(map[string]any{"replicas": 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Objects) != 1 || r.Objects[0].GetKind() != "ConfigMap" {
		t.Fatalf("objects: %+v", r.Objects)
	}
}

func TestRenderHelmChart(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	p, err := Fetch(context.Background(), "testdata/demo-helm", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range r.Objects {
		if o.GetKind() == "Deployment" {
			found = true
		}
	}
	if !found {
		t.Fatal("helm render produced no Deployment")
	}
}
