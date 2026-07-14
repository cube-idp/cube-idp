package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	sigyaml "sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
)

func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *diag.Error, got %T: %v", err, err)
	}
	return de.Code
}

func TestLoadMinimalAppliesDefaults(t *testing.T) {
	c, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "kind" || c.Spec.Engine.Type != "flux" {
		t.Fatalf("defaults not applied: %+v", c.Spec)
	}
	if c.Spec.Gateway.Host != "cube-idp.localtest.me" || c.Spec.Gateway.Port != 8443 || c.Spec.Gateway.Pack != "traefik" {
		t.Fatalf("gateway defaults: %+v", c.Spec.Gateway)
	}
	if c.Spec.Cluster.KubernetesVersion != "v1.33.1" {
		t.Fatalf("kubernetesVersion default not applied: %+v", c.Spec.Cluster)
	}
}

func TestLoadFullRoundTrips(t *testing.T) {
	c, err := Load("testdata/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spec.Cluster.ExtraPorts) != 1 || c.Spec.Cluster.ExtraPorts[0].HostPort != 32222 {
		t.Fatalf("extraPorts: %+v", c.Spec.Cluster.ExtraPorts)
	}
	if c.Spec.Cluster.Registry.Mirrors["docker.io"] != "https://mirror.corp.example" {
		t.Fatalf("mirrors: %+v", c.Spec.Cluster.Registry)
	}
	if len(c.Spec.Packs) != 2 || c.Spec.Packs[1].Values["replicas"] != 2 {
		t.Fatalf("packs: %+v", c.Spec.Packs)
	}
	if c.Spec.Gateway.Ref != "/repo/packs/traefik" {
		t.Fatalf("gateway.ref did not round-trip: %+v", c.Spec.Gateway)
	}
}

func TestLoadGatewayRefDefaultsEmpty(t *testing.T) {
	c, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Gateway.Ref != "" {
		t.Fatalf("gateway.ref should default to empty (falls back to packs/<pack> in `up`), got %q", c.Spec.Gateway.Ref)
	}
}

func TestLoadAcceptsK3dProvider(t *testing.T) {
	c, err := Load("testdata/k3d.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "k3d" {
		t.Fatalf("provider: %q", c.Spec.Cluster.Provider)
	}
}

func TestLoadRejectsBadProvider(t *testing.T) {
	_, err := Load("testdata/bad-provider.yaml")
	if codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("want CUBE-0002, got %v", err)
	}
}

func TestLoadRejectsNodeFieldsOnExisting(t *testing.T) {
	_, err := Load("testdata/existing-with-ports.yaml")
	if codeOf(t, err) != "CUBE-1003" {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadRejectsKubernetesVersionOnExisting(t *testing.T) { // D10, spec §4.1
	dir := t.TempDir()
	path := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: remote
spec:
  cluster:
    provider: existing
    context: my-eks
    kubernetesVersion: v1.30.0
`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if codeOf(t, err) != "CUBE-1003" {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadRejectsArgoPackWithArgoEngine(t *testing.T) { // CUBE-0005
	_, err := Load("testdata/argocd-engine-with-pack.yaml")
	if codeOf(t, err) != "CUBE-0005" {
		t.Fatalf("want CUBE-0005, got %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("testdata/nope.yaml")
	if codeOf(t, err) != "CUBE-0001" {
		t.Fatalf("want CUBE-0001, got %v", err)
	}
}

// TestDefaultRoundTripsThroughLoad pins the bug class fixed by the omitempty
// tags in types.go: `cube-idp init` marshals config.Default with
// sigs.k8s.io/yaml, and any optional (`?` in schema.cue) slice/map field
// WITHOUT omitempty serializes its nil zero value as an explicit YAML null,
// which CUE re-validation rejects (mismatched types list/map and null) —
// making every init-generated cube.yaml unloadable. A future optional field
// added without omitempty fails this test.
func TestDefaultRoundTripsThroughLoad(t *testing.T) {
	writeAndLoad := func(t *testing.T, c *Cube) *Cube {
		t.Helper()
		raw, err := sigyaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "cube.yaml")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load rejected marshaled config:\n%s\nerror: %v", raw, err)
		}
		return loaded
	}

	t.Run("default profile", func(t *testing.T) {
		def := Default("dev")
		loaded := writeAndLoad(t, def)
		if loaded.Spec.Cluster.Provider != def.Spec.Cluster.Provider {
			t.Fatalf("provider: got %q, want %q", loaded.Spec.Cluster.Provider, def.Spec.Cluster.Provider)
		}
		if loaded.Spec.Engine != def.Spec.Engine {
			t.Fatalf("engine: got %+v, want %+v", loaded.Spec.Engine, def.Spec.Engine)
		}
		if loaded.Spec.Gateway != def.Spec.Gateway {
			t.Fatalf("gateway: got %+v, want %+v", loaded.Spec.Gateway, def.Spec.Gateway)
		}
		if !reflect.DeepEqual(loaded.Spec.Packs, def.Spec.Packs) {
			t.Fatalf("packs: got %+v, want %+v", loaded.Spec.Packs, def.Spec.Packs)
		}
	})

	// packs? is optional in schema.cue, so a Cube without any packs (nil or
	// explicitly empty slice) must also round-trip — omitempty on Spec.Packs
	// keeps both out of the output instead of emitting `packs: null`.
	t.Run("empty packs slice", func(t *testing.T) {
		c := Default("dev")
		c.Spec.Packs = []PackRef{}
		loaded := writeAndLoad(t, c)
		if len(loaded.Spec.Packs) != 0 {
			t.Fatalf("packs should be absent, got %+v", loaded.Spec.Packs)
		}
	})

	t.Run("nil packs", func(t *testing.T) {
		c := Default("dev")
		c.Spec.Packs = nil
		loaded := writeAndLoad(t, c)
		if len(loaded.Spec.Packs) != 0 {
			t.Fatalf("packs should be absent, got %+v", loaded.Spec.Packs)
		}
	})
}

func TestDefaultProfileIncludesGitea(t *testing.T) { // D9
	c := Default("dev")
	found := false
	for _, p := range c.Spec.Packs {
		if p.Ref == "oci://ghcr.io/cube-idp/packs/gitea:0.1.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default profile must include gitea (D9): %+v", c.Spec.Packs)
	}
}
