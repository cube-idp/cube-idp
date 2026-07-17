package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestExplainKnownCode(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"explain", "CUBE-0007"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CUBE-0007") {
		t.Fatalf("no code in output: %s", out.String())
	}
}

// An unknown code is a diag error naming where the ranges live — not a
// panic, not a bare fmt.Errorf.
func TestExplainUnknownCodeFails(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"explain", "CUBE-9999"})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("explain CUBE-9999 must fail")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unknown code must return a diag.Error, got %T: %v", err, err)
	}
	if de.Code != diag.CodeBadFlagValue {
		t.Fatalf("unknown code should reuse CUBE-0007 (bad enum value), got %s", de.Code)
	}
	if !strings.Contains(de.Remediation, "explain --list") {
		t.Fatalf("remediation must point at explain --list: %q", de.Remediation)
	}
}

// Case is normalized: lowercase input resolves to the same code.
func TestExplainNormalizesCase(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"explain", "cube-0007"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CUBE-0007") {
		t.Fatalf("lowercase input not normalized: %s", out.String())
	}
}

// --list prints every registered code with its summary.
func TestExplainListAllCodes(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"explain", "--list"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, c := range diag.AllCodes() {
		if !strings.Contains(got, string(c)) {
			t.Fatalf("--list missing %s", c)
		}
	}
}
