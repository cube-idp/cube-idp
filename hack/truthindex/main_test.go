// hack/truthindex/main_test.go
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildIndexKnownSurface(t *testing.T) {
	idx := buildIndex()

	var upFound bool
	for _, c := range idx.Commands {
		if c.Path == "cube-idp up" {
			upFound = true
		}
	}
	if !upFound {
		t.Fatal("index must contain the `cube-idp up` command")
	}

	var code0007 bool
	for _, d := range idx.DiagCodes {
		if d.ID == "CUBE-0007" {
			code0007 = true
			if d.Summary == "" {
				t.Fatal("CUBE-0007 must carry its registry summary")
			}
		}
	}
	if !code0007 {
		t.Fatal("index must contain CUBE-0007")
	}

	if len(idx.ConfigSchema) == 0 || len(idx.PackContract) == 0 {
		t.Fatal("config schema and pack contract must be non-empty")
	}
	if idx.ExitContract["diagnostic_error"] != "1 (rendered)" {
		t.Fatalf("exit contract wrong: %v", idx.ExitContract)
	}
}

func TestIndexDeterministic(t *testing.T) {
	a, _ := json.MarshalIndent(buildIndex(), "", "  ")
	b, _ := json.MarshalIndent(buildIndex(), "", "  ")
	if string(a) != string(b) {
		t.Fatal("index must be byte-deterministic across runs")
	}
	if strings.Contains(string(a), "/Users/") {
		t.Fatal("index must not contain host paths")
	}
}
