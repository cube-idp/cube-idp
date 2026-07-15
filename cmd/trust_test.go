package cmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestTrustRequiresConsent(t *testing.T) {
	installed := false
	restore := trustInstall
	trustInstall = func(dir string) error { installed = true; return nil }
	defer func() { trustInstall = restore }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatalf("declining consent must not be an error: %v", err)
	}
	if installed {
		t.Fatal("trust must not touch the OS store without consent (D6)")
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Fatalf("expected an aborted notice, got:\n%s", out.String())
	}

	root = NewRootCmd()
	root.SetOut(&out)
	root.SetIn(strings.NewReader("y\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("trust must install after explicit consent")
	}
}

// TestTrustConsentUsesConfiguredGatewayHost covers (h): the consent prompt
// must name the CONFIGURED gateway.host (loaded via the same -f/--file
// convention every other command uses), not a hardcoded
// "cube-idp.localtest.me" that's wrong the moment a user sets a different
// host.
func TestTrustConsentUsesConfiguredGatewayHost(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile("cube.yaml", []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  cluster: {provider: kind, kubernetesVersion: v1.33.1}
  engine: {type: flux}
  gateway: {pack: traefik, host: custom.example.internal, port: 8443}
`), 0o644)

	restore := trustInstall
	trustInstall = func(dir string) error { return nil }
	defer func() { trustInstall = restore }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "custom.example.internal") {
		t.Fatalf("expected the configured gateway.host in the consent prompt, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "cube-idp.localtest.me") {
		t.Fatalf("prompt must not hardcode the default host once a config overrides it, got:\n%s", out.String())
	}
}

// TestTrustConsentFallsBackWithoutConfig covers (h)'s fallback: with no
// cube.yaml loadable, the prompt must use generic wording rather than
// guessing a host (right or wrong) it can't back up.
func TestTrustConsentFallsBackWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir()) // no cube.yaml here

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "cube-idp.localtest.me") {
		t.Fatalf("with no loadable config, the prompt must not assert a specific host, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "your cube-idp gateway's HTTPS") {
		t.Fatalf("generic fallback wording missing:\n%s", out.String())
	}
}

// TestTrustYesSkipsConsent covers --yes: install must proceed without
// reading stdin at all (SetIn is left wired to an empty reader — any
// attempt to read a consent line would see EOF, not "y", and fail).
func TestTrustYesSkipsConsent(t *testing.T) {
	installed := false
	restore := trustInstall
	trustInstall = func(dir string) error { installed = true; return nil }
	defer func() { trustInstall = restore }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("")) // no input available — --yes must not need it
	root.SetArgs([]string{"trust", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("--yes must install without prompting for consent")
	}
	if strings.Contains(out.String(), "Proceed?") {
		t.Fatalf("--yes must not print the consent prompt, got:\n%s", out.String())
	}
}

// TestTrustUninstall covers `trust --uninstall`: it must call trustUninstall
// (never trustInstall) and report removal, with no consent prompt.
func TestTrustUninstall(t *testing.T) {
	installed := false
	uninstalled := false
	restoreInstall := trustInstall
	restoreUninstall := trustUninstall
	trustInstall = func(dir string) error { installed = true; return nil }
	trustUninstall = func(dir string) error { uninstalled = true; return nil }
	defer func() { trustInstall = restoreInstall; trustUninstall = restoreUninstall }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"trust", "--uninstall"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !uninstalled {
		t.Fatal("--uninstall must call trustUninstall")
	}
	if installed {
		t.Fatal("--uninstall must never call trustInstall")
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("expected a removal notice, got:\n%s", out.String())
	}
}

// TestTrustUninstallPropagatesError covers the failure path: a typed
// diag.Error from trustUninstall (e.g. CUBE-6003) must surface, not be
// swallowed.
func TestTrustUninstallPropagatesError(t *testing.T) {
	restore := trustUninstall
	trustUninstall = func(dir string) error { return errors.New("boom") }
	defer func() { trustUninstall = restore }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"trust", "--uninstall"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected trustUninstall's error to propagate")
	}
}
