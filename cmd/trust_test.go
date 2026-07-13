package cmd

import (
	"bytes"
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
