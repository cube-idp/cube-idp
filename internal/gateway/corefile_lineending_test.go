package gateway_test

import (
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestCorefileSpliceWithoutTrailingNewline asserts the splice adds no
// trailing newline of its own on either path. A ConfigMap value is
// whatever the last writer stored, so a Corefile that ends without one
// must come back without one — the read-modify-write at the edge would
// otherwise report a diff on every bootstrap of an unchanged cube.
func TestCorefileSpliceWithoutTrailingNewline(t *testing.T) {
	bare := strings.TrimSuffix(readFixture(t, "kind-v1.35.0.txt"), "\n")
	wantBare := strings.TrimSuffix(readFixture(t, "kind-v1.35.0-spliced.txt"), "\n")

	inserted, err := gateway.CorefileSplice(bare, testCube, testDomain)
	if err != nil {
		t.Fatalf("insert path: %v", err)
	}
	if inserted != wantBare {
		t.Errorf("insert path:\n got %q\nwant %q", inserted, wantBare)
	}

	replaced, err := gateway.CorefileSplice(wantBare, testCube, testDomain)
	if err != nil {
		t.Fatalf("replace path: %v", err)
	}
	if replaced != wantBare {
		t.Errorf("replace path:\n got %q\nwant %q", replaced, wantBare)
	}
	if strings.HasSuffix(inserted, "\n") || strings.HasSuffix(replaced, "\n") {
		t.Error("the splice appended a trailing newline the input did not carry")
	}
}

// TestCorefileSpliceCRLF pins current behavior rather than endorsing it.
// No kubeadm Corefile is CRLF, so this is decided rather than accidental:
// the splice splits on "\n", marker matching tolerates the stray "\r"
// through TrimSpace, the rejoin hands every original line its "\r" back
// untouched, and the freshly rendered block carries LF endings — so a CRLF
// input comes back with mixed endings and no error. CoreDNS accepts both,
// and normalizing would rewrite bytes outside this cube's markers, which
// the preservation clause forbids.
func TestCorefileSpliceCRLF(t *testing.T) {
	lf := readFixture(t, "kind-v1.35.0.txt")
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")

	got, err := gateway.CorefileSplice(crlf, testCube, testDomain)
	if err != nil {
		t.Fatalf("CorefileSplice over a CRLF Corefile: %v", err)
	}
	if want := readFixture(t, "kind-v1.35.0-spliced.txt"); strings.ReplaceAll(got, "\r\n", "\n") != want {
		t.Errorf("normalized output differs from the LF fixture:\n got %q\nwant %q",
			strings.ReplaceAll(got, "\r\n", "\n"), want)
	}
	if !strings.Contains(got, "    errors\r\n") {
		t.Error("an original line lost its CR; content outside the markers must survive byte for byte")
	}
	if !strings.Contains(got, "    # cube-idp:begin "+testCube+"\n") ||
		strings.Contains(got, "    # cube-idp:begin "+testCube+"\r\n") {
		t.Error("the rendered block no longer carries LF endings; this test pins mixed endings, not normalization")
	}
}
