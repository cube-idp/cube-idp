// Partial-lifecycle diagnostics (issue #212): the delete half. Fixtures
// and helpers (foreignKubeconfig, sealDir, contextNames,
// assertForeignStateIntact) live in lifecycle_partial_test.go.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestDeletePartialFailureNamesCompletedPhase is issue #212's second half.
// The seam Delete succeeded — the cluster is gone — but the kubeconfig
// rewrite failed. Two defects at once before the fix: the error said only
// "kubeconfig update failed", discarding the fact that the cluster went
// away; and Delete returned changed == true even though writeKubeconfig is
// atomic (temp file + rename), so a failed write leaves the target exactly
// as it was. `changed` is documented as "whether the kubeconfig was
// modified" (internal/cluster/delete.go), which true contradicts.
func TestDeletePartialFailureNamesCompletedPhase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kubeconfig")
	t.Setenv("KUBECONFIG", target)
	if err := os.WriteFile(target, []byte(foreignKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Init(t.Context(), &mockProvisioner{}, InitOptions{Spec: Spec{Name: "dev"}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	sealDir(t, dir)

	changed, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})

	if changed {
		t.Error("changed = true, but the atomic write failed — nothing was modified")
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("err = %v, want a *cubeerr.Coded", err)
	}
	if coded.Code != CodeKubeconfigFailed {
		t.Fatalf("Code = %s, want %s", coded.Code, CodeKubeconfigFailed)
	}
	if !strings.Contains(coded.Summary, "cluster is gone") {
		t.Errorf("Summary must state the cluster is gone, got %q", coded.Summary)
	}
	if !strings.Contains(coded.Remediation, "cube-idp delete") {
		t.Errorf("remediation must name the recovery command, got %q", coded.Remediation)
	}
	if coded.Unwrap() == nil {
		t.Error("the technical cause must survive the phase wording")
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target must still exist: %v", err)
	}
	// Semantic first — the cube context is still installed, which is what
	// changed == false claims. Byte equality is additionally meaningful
	// here precisely because no rewrite happened at all.
	if !slicesContains(contextNames(t, after), "cube-idp.dev/dev") {
		t.Errorf("the cube context vanished despite the failed write:\n%s", after)
	}
	assertForeignStateIntact(t, after)
	if string(after) != string(before) {
		t.Errorf("the target changed despite the failed atomic write:\n%s", after)
	}
}

// TestDeleteRetrySucceedsAfterPermissionFixed is the issue's own recovery
// path: the operator fixes the cause and re-runs the same command.
func TestDeleteRetrySucceedsAfterPermissionFixed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kubeconfig")
	t.Setenv("KUBECONFIG", target)
	if err := os.WriteFile(target, []byte(foreignKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Init(t.Context(), &mockProvisioner{}, InitOptions{Spec: Spec{Name: "dev"}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sealDir(t, dir)
	if _, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"}); err == nil {
		t.Fatal("Delete must fail while the directory is sealed")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	changed, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})
	if err != nil {
		t.Fatalf("retry after fixing the cause: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true once the rewrite succeeds")
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if slicesContains(contextNames(t, after), "cube-idp.dev/dev") {
		t.Errorf("the cube context survived a successful delete:\n%s", after)
	}
	assertForeignStateIntact(t, after)
}

// TestDeleteMalformedTargetNamesCompletedPhase covers the delete-side
// phase diagnostic WITHOUT a sealed directory, so the direction stays
// covered on a root runner where every chmod-based case skips. A
// malformed cleanup target fails inside Remove, after the seam Delete has
// already succeeded.
func TestDeleteMalformedTargetNamesCompletedPhase(t *testing.T) {
	target := filepath.Join(t.TempDir(), "kubeconfig")
	t.Setenv("KUBECONFIG", target)
	if err := os.WriteFile(target, []byte(":\tnot yaml"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})

	if changed {
		t.Error("changed = true, but nothing could be parsed, let alone written")
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("err = %v, want a *cubeerr.Coded", err)
	}
	if coded.Code != CodeKubeconfigFailed {
		t.Fatalf("Code = %s, want %s", coded.Code, CodeKubeconfigFailed)
	}
	if !strings.Contains(coded.Summary, "cluster is gone") {
		t.Errorf("Summary must state the cluster is gone, got %q", coded.Summary)
	}
	if coded.Unwrap() == nil {
		t.Error("the technical cause must survive the phase wording")
	}
}

// TestDeleteSeamFailureMakesNoPhaseClaim: a failed seam Delete promises no
// rollback and proves nothing, so it must never say the cluster is gone.
func TestDeleteSeamFailureMakesNoPhaseClaim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig"))
	mock := &mockProvisioner{DeleteFunc: func(_ context.Context, name string) error {
		return NewProvisionFailedError("delete", name, fmt.Errorf("boom"))
	}}

	_, err := Delete(t.Context(), mock, DeleteOptions{Name: "dev"})

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeProvisionFailed {
		t.Fatalf("err = %v, want code %s", err, CodeProvisionFailed)
	}
	if strings.Contains(coded.Summary, "cluster is gone") {
		t.Errorf("a failed seam Delete must not claim the cluster is gone: %q", coded.Summary)
	}
}
