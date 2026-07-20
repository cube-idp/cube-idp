package cmd

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
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
	// The default profile is fully standalone — the gateway
	// pack resolves from the published oci ref, never a repo-relative path.
	if cube.Spec.Gateway.Ref != "oci://ghcr.io/cube-idp/packs/traefik:0.2.0" {
		t.Fatalf("gateway.ref must be the published oci ref, got %q", cube.Spec.Gateway.Ref)
	}
	if len(cube.Spec.Packs) != 2 || cube.Spec.Packs[0].Ref != "oci://ghcr.io/cube-idp/packs/gitea:0.2.0" {
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
	if len(cube.Spec.Packs) != 1 || cube.Spec.Packs[0].Ref != "oci://ghcr.io/cube-idp/packs/gitea:0.2.0" {
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

// TestFilterSelectedPacks pins the wizard's pack multi-select projection:
// deselecting a catalog pack drops it (OCI or local ref), a non-catalog ref is
// always kept, and the pre-existing withoutGiteaPack keeps its meaning.
func TestFilterSelectedPacks(t *testing.T) {
	packs := []config.PackRef{
		{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
		{Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.1.0"},
		{Ref: "oci://ghcr.io/cube-idp/packs/backstage:0.1.0"}, // non-catalog: always kept
	}
	got := filterSelectedPacks(packs, []string{"argocd"})
	if len(got) != 2 || got[0].Ref != packs[1].Ref || got[1].Ref != packs[2].Ref {
		t.Fatalf("deselecting gitea must drop only gitea, keep argocd + non-catalog: %+v", got)
	}
	if kept := withoutGiteaPack(packs); len(kept) != 2 || packCatalogName(kept[0].Ref) != "argocd" {
		t.Fatalf("withoutGiteaPack must still strip gitea: %+v", kept)
	}
}

// TestApplyWizardExistingProviderLoads is the CUE-parity guard (design doc
// §10: "the wizard must never accept what Load() rejects"): a wizard that
// selects the existing provider must produce a cube.yaml that config.Load
// accepts — i.e. the kind-only kubernetesVersion is cleared.
func TestApplyWizardExistingProviderLoads(t *testing.T) {
	cube := config.Default("dev")
	applyWizardToCube(cube, initWizardResult{
		Provider: "existing", Context: "kind-dev",
		GatewayHost: "cube.example", GatewayPort: 9443,
		Packs: []string{"gitea"}, // drop argocd
	})
	if cube.Spec.Cluster.Provider != "existing" || cube.Spec.Cluster.Context != "kind-dev" {
		t.Fatalf("provider/context not applied: %+v", cube.Spec.Cluster)
	}
	if cube.Spec.Cluster.KubernetesVersion != "" {
		t.Fatalf("existing provider must clear kubernetesVersion, got %q", cube.Spec.Cluster.KubernetesVersion)
	}
	if cube.Spec.Gateway.Host != "cube.example" || cube.Spec.Gateway.Port != 9443 {
		t.Fatalf("gateway not applied: %+v", cube.Spec.Gateway)
	}
	if len(cube.Spec.Packs) != 1 || packCatalogName(cube.Spec.Packs[0].Ref) != "gitea" {
		t.Fatalf("pack selection not applied: %+v", cube.Spec.Packs)
	}

	dir := t.TempDir()
	raw, err := yaml.Marshal(cube)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cube.yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("wizard-produced cube.yaml must load (CUE parity): %v\n%s", err, raw)
	}
}

// TestApplyWizardAppendsRemoteCatalogPacks pins the remote-catalog wizard semantics:
// a selected catalog pack OUTSIDE the built-in list (remote-discovered) is
// APPENDED with its index ref; a selected built-in name never appends — the
// default profile's membership stays engine/--local logic's decision (an
// engine-argocd cube keeps the argocd pack dropped even when "argocd" is
// ticked, so CUBE-0005 cannot be re-authored by the wizard).
func TestApplyWizardAppendsRemoteCatalogPacks(t *testing.T) {
	remote := []catalogEntry{
		{Name: "argocd", Version: "0.2.0", Desc: "delivery UI", Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.2.0"},
		{Name: "gitea", Version: "0.1.0", Desc: "in-cluster git server", Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
		{Name: "kargo", Version: "1.0.0", Desc: "promotion pipelines", Ref: "oci://ghcr.io/cube-idp/packs/kargo:1.0.0"},
	}

	cube := config.Default("dev")
	applyWizardToCube(cube, initWizardResult{
		Provider: "kind",
		Packs:    []string{"gitea", "argocd", "kargo"},
		Catalog:  remote,
	})
	refs := make([]string, 0, len(cube.Spec.Packs))
	for _, p := range cube.Spec.Packs {
		refs = append(refs, p.Ref)
	}
	want := []string{
		"oci://ghcr.io/cube-idp/packs/gitea:0.2.0",  // default profile, kept
		"oci://ghcr.io/cube-idp/packs/argocd:0.2.0", // default profile, kept
		"oci://ghcr.io/cube-idp/packs/kargo:1.0.0",  // remote-discovered, appended
	}
	if len(refs) != len(want) {
		t.Fatalf("packs = %v, want %v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("packs[%d] = %q, want %q (all: %v)", i, refs[i], want[i], refs)
		}
	}

	// Engine-argocd shape: the argocd pack was dropped BEFORE the wizard
	// answers apply; ticking "argocd" must not resurrect it.
	cube = config.Default("dev")
	cube.Spec.Packs = []config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"}}
	applyWizardToCube(cube, initWizardResult{
		Provider: "kind",
		Packs:    []string{"gitea", "argocd"},
		Catalog:  remote,
	})
	if len(cube.Spec.Packs) != 1 || packCatalogName(cube.Spec.Packs[0].Ref) != "gitea" {
		t.Fatalf("built-in selection must never append (CUBE-0005 guard), got %+v", cube.Spec.Packs)
	}
}

// TestValidateGatewayPortRejectsGarbage covers the wizard's inline port
// validation: non-numeric and out-of-range values are rejected; a free port
// passes.
func TestValidateGatewayPortRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "abc", "0", "70000", "-1"} {
		if validateGatewayPort(bad) == nil {
			t.Fatalf("validateGatewayPort(%q) must fail", bad)
		}
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if err := validateGatewayPort(strconv.Itoa(port)); err != nil {
		t.Fatalf("a free port must pass: %v", err)
	}
}

// TestInitLocalGatewayRefFollowsPack: init --local + --gateway-pack
// envoy-gateway writes ref packs/envoy-gateway AND pack envoy-gateway —
// the trap (ref traefik, pack envoy) can no longer be authored by init.
func TestInitLocalGatewayRefFollowsPack(t *testing.T) {
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetArgs([]string{"init", "--name", "dev", "--local", "/repo", "--gateway-pack", "envoy-gateway"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	cube, err := config.Load("cube.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cube.Spec.Gateway.Pack != "envoy-gateway" || cube.Spec.Gateway.Ref != filepath.Join("/repo", "packs", "envoy-gateway") {
		t.Fatalf("gateway source incoherent: pack=%q ref=%q", cube.Spec.Gateway.Pack, cube.Spec.Gateway.Ref)
	}
}

// TestInitPublishedGatewayPackOnly: without --local, choosing a gateway pack
// writes the published oci ref DERIVED FROM THAT PACK (the default
// profile is standalone; the §5.7a coherence rule — ref always follows the
// final chosen pack — holds in published mode too, so the trap of ref
// traefik + pack envoy cannot be authored by init).
func TestInitPublishedGatewayPackOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetArgs([]string{"init", "--name", "dev", "--gateway-pack", "envoy-gateway"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	cube, err := config.Load("cube.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cube.Spec.Gateway.Pack != "envoy-gateway" || cube.Spec.Gateway.Ref != "oci://ghcr.io/cube-idp/packs/envoy-gateway:0.2.0" {
		t.Fatalf("published mode must derive the oci ref from the final pack: pack=%q ref=%q", cube.Spec.Gateway.Pack, cube.Spec.Gateway.Ref)
	}
}

// TestInitRejectsUnknownGatewayPack: an unrecognized --gateway-pack value
// is a CUBE-0007 preflight error, same enum-flag pattern as --progress.
func TestInitRejectsUnknownGatewayPack(t *testing.T) {
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetArgs([]string{"init", "--name", "dev", "--gateway-pack", "nginx"})
	err := root.Execute()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-0007" {
		t.Fatalf("want CUBE-0007, got %v", err)
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
