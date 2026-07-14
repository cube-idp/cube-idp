package cmd

import (
	"bytes"
	"errors"
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
