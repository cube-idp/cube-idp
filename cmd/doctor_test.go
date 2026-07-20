package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/doctor"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// TestWriteDoctorJSON pins the gh-style doctor document — one final object,
// not a stream, because doctor answers once: the
// findings array carries codes and severities, and errors reflects whether any
// finding is an error (the exit-code driver).
func TestWriteDoctorJSON(t *testing.T) {
	var b bytes.Buffer
	errs := writeDoctorJSON(&b, []diag.Finding{
		{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"},
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	}, nil)
	if !errs {
		t.Fatal("an error finding must set errors=true")
	}
	var doc doctorDoc
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, b.String())
	}
	if doc.V != 1 || !doc.Errors || len(doc.Findings) != 2 {
		t.Fatalf("doc head/verdict/findings wrong: %+v", doc)
	}
	if doc.Findings[1].Code != "CUBE-0101" || doc.Findings[1].Severity != "error" {
		t.Fatalf("finding fields: %+v", doc.Findings[1])
	}
}

// TestWriteDoctorJSONNoErrors confirms a clean run reports errors=false with
// empty (non-null) findings and checks arrays.
func TestWriteDoctorJSONNoErrors(t *testing.T) {
	var b bytes.Buffer
	if writeDoctorJSON(&b, nil, nil) {
		t.Fatal("no findings must report errors=false")
	}
	if !bytes.Contains(b.Bytes(), []byte(`"findings": []`)) {
		t.Fatalf("empty findings must marshal as [], got: %s", b.String())
	}
	if !bytes.Contains(b.Bytes(), []byte(`"checks": []`)) {
		t.Fatalf("empty checks must marshal as [], got: %s", b.String())
	}
}

// ——— tri-state checklist ———

// stubDoctorChecks swaps the host-check assembly seam (doctorChecks — the
// statusConnect pattern) for a fixed set, so command tests control every
// row deterministically.
func stubDoctorChecks(t *testing.T, checks ...doctor.Check) {
	t.Helper()
	restore := doctorChecks
	doctorChecks = func(*config.Cube, bool) []doctor.Check { return checks }
	t.Cleanup(func() { doctorChecks = restore })
}

// stubDoctorCluster silences the cluster-side probe seam so command tests
// never touch docker, kubeconfigs, or the network.
func stubDoctorCluster(t *testing.T) {
	t.Helper()
	restore := doctorProbeCluster
	doctorProbeCluster = func(context.Context, *config.Cube) (bool, []diag.Finding, []doctor.CheckResult) {
		return false, nil, nil
	}
	t.Cleanup(func() { doctorProbeCluster = restore })
}

func greenCheck(name, detail string) doctor.Check {
	return doctor.Check{Name: name, Run: func() (string, []diag.Finding) { return detail, nil }}
}

func findingCheck(name string, sev diag.Severity, code diag.Code, msg string) doctor.Check {
	return doctor.Check{Name: name, Run: func() (string, []diag.Finding) {
		return "", []diag.Finding{{Code: code, Severity: sev, Message: msg, Remediation: "do the thing"}}
	}}
}

// checklistRow finds the one checklist row naming a check (rows start with
// the tri-state word in plain mode; styled rows carry a glyph first).
func checklistRow(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, name) {
			continue
		}
		trimmed := strings.TrimLeft(line, "✔⚠✗ \x1b[0123456789;m")
		for _, word := range []string{"ok", "warn", "fail"} {
			if strings.HasPrefix(trimmed, word) {
				return line
			}
		}
	}
	t.Fatalf("no checklist row for %q in:\n%s", name, out)
	return ""
}

// TestDoctorChecklistRowsPlainAndExit: every stubbed check renders
// exactly one row; plain rows are word-only (ok/warn/fail, no glyphs); a
// red row keeps the exit-1 contract, and the findings block below still
// carries the remediation.
func TestDoctorChecklistRowsPlainAndExit(t *testing.T) {
	p := writeSpokeFixture(t)
	stubDoctorCluster(t)
	stubDoctorChecks(t,
		greenCheck("container-runtime", "docker on PATH"),
		findingCheck("disk-space", diag.SeverityWarning, "CUBE-0103", "low disk"),
		findingCheck("gateway-port", diag.SeverityError, "CUBE-0102", "port 8443 busy"),
	)
	out, err := runCLI(t, "doctor", "-f", p)
	if err == nil {
		t.Fatalf("a red row must exit non-zero:\n%s", out)
	}
	if code, render := ExitCodeFor(err); code != 1 || render {
		t.Fatalf("doctor exit contract drifted: code=%d render=%v (%v)", code, render, err)
	}
	if row := checklistRow(t, out, "container-runtime"); !strings.HasPrefix(row, "ok") ||
		!strings.Contains(row, "docker on PATH") || strings.Contains(row, "✔") {
		t.Fatalf("plain green row must be word-only with the detail: %q", row)
	}
	if row := checklistRow(t, out, "disk-space"); !strings.HasPrefix(row, "warn") ||
		!strings.Contains(row, "low disk — CUBE-0103") {
		t.Fatalf("plain warn row must pair message with code: %q", row)
	}
	if row := checklistRow(t, out, "gateway-port"); !strings.HasPrefix(row, "fail") ||
		!strings.Contains(row, "port 8443 busy — CUBE-0102") {
		t.Fatalf("plain fail row must pair message with code: %q", row)
	}
	if !strings.Contains(out, "fix: do the thing") {
		t.Fatalf("findings block with remediation must follow the checklist:\n%s", out)
	}
}

// TestDoctorChecklistGreenWarnExitsZero: warnings alone never exit 1 —
// the pre-U5 exit semantics preserved (exit 1 iff any red).
func TestDoctorChecklistGreenWarnExitsZero(t *testing.T) {
	p := writeSpokeFixture(t)
	stubDoctorCluster(t)
	stubDoctorChecks(t,
		greenCheck("container-runtime", "docker on PATH"),
		findingCheck("disk-space", diag.SeverityWarning, "CUBE-0103", "low disk"),
	)
	out := mustRunCLI(t, "doctor", "-f", p)
	if row := checklistRow(t, out, "container-runtime"); !strings.HasPrefix(row, "ok") {
		t.Fatalf("green row missing: %q", row)
	}
	if row := checklistRow(t, out, "disk-space"); !strings.HasPrefix(row, "warn") {
		t.Fatalf("warn row missing: %q", row)
	}
}

// TestDoctorChecklistStyledPairsGlyphWithWord: --progress live forces the
// styled render into the test buffer (rung 3); every row pairs the themed
// glyph with its word — color and symbol never carry meaning alone.
func TestDoctorChecklistStyledPairsGlyphWithWord(t *testing.T) {
	t.Cleanup(func() { ui.SetMode(ui.ModeStyled) })
	p := writeSpokeFixture(t)
	stubDoctorCluster(t)
	stubDoctorChecks(t,
		greenCheck("container-runtime", "docker on PATH"),
		findingCheck("disk-space", diag.SeverityWarning, "CUBE-0103", "low disk"),
		findingCheck("gateway-port", diag.SeverityError, "CUBE-0102", "port 8443 busy"),
	)
	out, err := runCLI(t, "doctor", "--progress", "live", "-f", p)
	if err == nil {
		t.Fatalf("a red row must exit non-zero in styled mode too:\n%s", out)
	}
	for _, pair := range []struct{ name, glyph, word string }{
		{"container-runtime", "✔", "ok"},
		{"disk-space", "⚠", "warn"},
		{"gateway-port", "✗", "fail"},
	} {
		row := checklistRow(t, out, pair.name)
		if !strings.Contains(row, pair.glyph) || !strings.Contains(row, pair.word) {
			t.Fatalf("styled row must pair glyph %q with word %q: %q", pair.glyph, pair.word, row)
		}
	}
}

// TestDoctorJSONChecksArrayAdditive: -o json gains the additive checks
// array — one row per executed check, additive to the existing document:
// name/status plus detail (ok) or code+message
// (warn/fail); the findings array and errors verdict are unchanged.
func TestDoctorJSONChecksArrayAdditive(t *testing.T) {
	p := writeSpokeFixture(t)
	stubDoctorCluster(t)
	stubDoctorChecks(t,
		greenCheck("container-runtime", "docker on PATH"),
		findingCheck("disk-space", diag.SeverityWarning, "CUBE-0103", "low disk"),
		findingCheck("gateway-port", diag.SeverityError, "CUBE-0102", "port 8443 busy"),
	)
	out, err := runCLI(t, "doctor", "-o", "json", "-f", p)
	if err == nil {
		t.Fatalf("errors=true must still exit 1 with -o json:\n%s", out)
	}
	if !strings.Contains(out, `"checks": [`) {
		t.Fatalf("checks array missing from document:\n%s", out)
	}
	var doc doctorDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, out)
	}
	if len(doc.Checks) != 3 {
		t.Fatalf("want 3 check rows, got %+v", doc.Checks)
	}
	if c := doc.Checks[0]; c.Name != "container-runtime" || c.Status != "ok" || c.Detail != "docker on PATH" || c.Code != "" {
		t.Fatalf("green check row wrong: %+v", c)
	}
	if c := doc.Checks[1]; c.Name != "disk-space" || c.Status != "warn" || c.Code != "CUBE-0103" || c.Message != "low disk" || c.Detail != "" {
		t.Fatalf("warn check row wrong: %+v", c)
	}
	if c := doc.Checks[2]; c.Name != "gateway-port" || c.Status != "fail" || c.Code != "CUBE-0102" {
		t.Fatalf("fail check row wrong: %+v", c)
	}
	if len(doc.Findings) != 2 || !doc.Errors {
		t.Fatalf("findings array/verdict must be unchanged: findings=%+v errors=%v", doc.Findings, doc.Errors)
	}
}

// TestDoctorRowTextFoldsMultiFinding: a multi-finding check (inotify,
// spoke-reachability) folds to one row naming the worst finding plus a
// (+N more) marker; every finding still reaches the findings array.
func TestDoctorRowTextFoldsMultiFinding(t *testing.T) {
	r := doctor.CheckResult{Name: "spoke-reachability", Findings: []diag.Finding{
		{Code: "CUBE-8006", Severity: diag.SeverityWarning, Message: `spoke "a" unreachable`},
		{Code: "CUBE-8006", Severity: diag.SeverityWarning, Message: `spoke "b" unreachable`},
	}}
	if got, want := doctorRowText(r), `spoke "a" unreachable — CUBE-8006 (+1 more)`; got != want {
		t.Fatalf("multi-finding fold drifted:\ngot:  %q\nwant: %q", got, want)
	}
	green := doctor.CheckResult{Name: "x", Detail: "fine"}
	if got := doctorRowText(green); got != "fine" {
		t.Fatalf("green row text must be the detail, got %q", got)
	}
}
