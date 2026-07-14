package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

// TestWriteDoctorJSON pins the gh-style doctor document (design doc §10): the
// findings array carries codes and severities, and errors reflects whether any
// finding is an error (the exit-code driver).
func TestWriteDoctorJSON(t *testing.T) {
	var b bytes.Buffer
	errs := writeDoctorJSON(&b, []diag.Finding{
		{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"},
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	})
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

// TestWriteDoctorJSONNoErrors confirms a clean run reports errors=false with an
// empty (non-null) findings array.
func TestWriteDoctorJSONNoErrors(t *testing.T) {
	var b bytes.Buffer
	if writeDoctorJSON(&b, nil) {
		t.Fatal("no findings must report errors=false")
	}
	if !bytes.Contains(b.Bytes(), []byte(`"findings": []`)) {
		t.Fatalf("empty findings must marshal as [], got: %s", b.String())
	}
}
