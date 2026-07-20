package factory

import (
	"errors"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestFactoryFlux(t *testing.T) {
	if _, err := New(config.EngineSpec{Type: "flux"}); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryArgoCD(t *testing.T) {
	if _, err := New(config.EngineSpec{Type: "argocd"}); err != nil {
		t.Fatalf("argocd engine must construct: %v", err)
	}
}

func TestFactoryUnknown(t *testing.T) {
	_, err := New(config.EngineSpec{Type: "jenkins"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-3001" {
		t.Fatalf("want CUBE-3001, got %v", err)
	}
}
