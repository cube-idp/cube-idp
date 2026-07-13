package factory

import (
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestFactoryFlux(t *testing.T) {
	if _, err := New("flux"); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryArgoCD(t *testing.T) {
	if _, err := New("argocd"); err != nil {
		t.Fatalf("argocd engine must construct in Phase 2 (D2): %v", err)
	}
}

func TestFactoryUnknown(t *testing.T) {
	_, err := New("jenkins")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-3001" {
		t.Fatalf("want CUBE-3001, got %v", err)
	}
}
