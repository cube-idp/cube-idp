package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/config"
)

func TestInitWritesDefaultOCIRefs(t *testing.T) {
	t.Chdir(t.TempDir())

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "dev"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	cube := readCube(t, "cube.yaml")
	if cube.Spec.Gateway.Ref != "" {
		t.Fatalf("gateway.ref should be unset without --local, got %q", cube.Spec.Gateway.Ref)
	}
	if len(cube.Spec.Packs) != 2 || cube.Spec.Packs[0].Ref != "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0" {
		t.Fatalf("expected default OCI pack refs, got %+v", cube.Spec.Packs)
	}
}

func TestInitLocalWritesRepoLocalRefs(t *testing.T) {
	t.Chdir(t.TempDir())
	repoRoot := t.TempDir()

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "dev", "--local", repoRoot})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	cube := readCube(t, "cube.yaml")
	wantGatewayRef := filepath.Join(repoRoot, "packs", "traefik")
	if cube.Spec.Gateway.Ref != wantGatewayRef {
		t.Fatalf("gateway.ref = %q, want %q", cube.Spec.Gateway.Ref, wantGatewayRef)
	}
	wantPacks := []string{
		filepath.Join(repoRoot, "packs", "gitea"),
		filepath.Join(repoRoot, "packs", "argocd"),
	}
	if len(cube.Spec.Packs) != len(wantPacks) {
		t.Fatalf("packs = %+v, want refs %v", cube.Spec.Packs, wantPacks)
	}
	for i, want := range wantPacks {
		if cube.Spec.Packs[i].Ref != want {
			t.Fatalf("packs[%d].ref = %q, want %q", i, cube.Spec.Packs[i].Ref, want)
		}
	}
}

func TestInitEngineArgoCDDropsArgoPack(t *testing.T) { // CUBE-0005 avoidance
	t.Chdir(t.TempDir())

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "dev", "--engine", "argocd"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	cube := readCube(t, "cube.yaml")
	if cube.Spec.Engine.Type != "argocd" {
		t.Fatalf("engine.type = %q, want argocd", cube.Spec.Engine.Type)
	}
	if len(cube.Spec.Packs) != 1 || cube.Spec.Packs[0].Ref != "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0" {
		t.Fatalf("expected only the gitea pack (argocd pack would trip CUBE-0005), got %+v", cube.Spec.Packs)
	}
}

func TestInitLocalEngineArgoCDDropsArgoPack(t *testing.T) {
	t.Chdir(t.TempDir())
	repoRoot := t.TempDir()

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "dev", "--engine", "argocd", "--local", repoRoot})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	cube := readCube(t, "cube.yaml")
	wantPacks := []string{filepath.Join(repoRoot, "packs", "gitea")}
	if len(cube.Spec.Packs) != len(wantPacks) {
		t.Fatalf("packs = %+v, want refs %v", cube.Spec.Packs, wantPacks)
	}
	for i, want := range wantPacks {
		if cube.Spec.Packs[i].Ref != want {
			t.Fatalf("packs[%d].ref = %q, want %q", i, cube.Spec.Packs[i].Ref, want)
		}
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("cube.yaml", []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err == nil {
		t.Fatal("want error when cube.yaml already exists, got nil")
	}
}

func readCube(t *testing.T, path string) *config.Cube {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c config.Cube
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return &c
}
