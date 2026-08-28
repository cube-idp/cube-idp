package gateway_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// The cube and domain the captured fixtures were spliced for.
const (
	testCube   = "mycube"
	testDomain = "mycube.cube.test"
)

// The Corefile fixtures are derived from a live capture, not authored:
// kind-v1.35.0.txt is the kube-system/coredns Corefile of a cluster created
// from kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f
// (CoreDNS v1.13.1), and kind-v1.35.0-spliced.txt is the result of applying
// this cube's block to it — a file that was patched into that live cluster
// and parsed and reloaded cleanly. Every other fixture is that capture with
// marker lines moved or removed, so the byte-for-byte preservation
// assertions run against real kubeadm whitespace (including the 7-space
// indent on `lameduck 5s`) rather than retyped whitespace.

// TestCorefileSplice walks the decided failure taxonomy: one row per case,
// distinguished by its input and expected output. Errors are asserted by
// code alone — nine rows share CUBE-GWY-004, and pinning their details
// would freeze diagnostics that should stay free to improve.
func TestCorefileSplice(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "insert into a clean kubeadm Corefile", in: "kind-v1.35.0.txt", want: "kind-v1.35.0-spliced.txt"},
		{name: "replace this cube's own block", in: "kind-v1.35.0-spliced.txt", want: "kind-v1.35.0-spliced.txt"},
		{name: "insert beside a foreign cube's block", in: "foreign-cube.txt", want: "both-cubes.txt"},
		{name: "replace mine, preserve theirs", in: "both-cubes.txt", want: "both-cubes.txt"},
		{name: "begin marker without an end", in: "corrupt-begin-only.txt", wantErr: true},
		{name: "end marker without a begin", in: "corrupt-end-only.txt", wantErr: true},
		{name: "end marker before begin", in: "corrupt-reversed.txt", wantErr: true},
		{name: "duplicate blocks for this cube", in: "corrupt-duplicate.txt", wantErr: true},
		{name: "markers above every server block", in: "corrupt-markers-outside.txt", wantErr: true},
		{name: "no default server block", in: "no-default-server.txt", wantErr: true},
		{name: "two default server blocks", in: "two-default-servers.txt", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gateway.CorefileSplice(readFixture(t, tc.in), testCube, testDomain)
			if tc.wantErr {
				assertCorefileStructureError(t, err)
				if got != "" {
					t.Errorf("failed splice returned %d bytes, want the empty string", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("CorefileSplice: %v", err)
			}
			if want := readFixture(t, tc.want); got != want {
				t.Errorf("splice output differs from %s:\n got %q\nwant %q", tc.want, got, want)
			}
		})
	}
}

// TestCorefileSpliceEmptyInput covers the degenerate inputs through the
// same structural error: neither has a server block, so neither needs a
// special case.
func TestCorefileSpliceEmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n", "   \n\t\n"} {
		_, err := gateway.CorefileSplice(in, testCube, testDomain)
		assertCorefileStructureError(t, err)
	}
}

// TestCorefileSpliceIdempotent asserts the property the whole marker design
// exists for: splicing twice equals splicing once, byte for byte, so a
// re-run of bootstrap never accumulates blocks.
func TestCorefileSpliceIdempotent(t *testing.T) {
	base := readFixture(t, "kind-v1.35.0.txt")
	once, err := gateway.CorefileSplice(base, testCube, testDomain)
	if err != nil {
		t.Fatalf("first splice: %v", err)
	}
	twice, err := gateway.CorefileSplice(once, testCube, testDomain)
	if err != nil {
		t.Fatalf("second splice: %v", err)
	}
	if once != twice {
		t.Errorf("splice is not idempotent:\n once %q\ntwice %q", once, twice)
	}
}

// TestCorefileSplicePreservesForeign asserts the trap the exact-cube-name
// matching exists to avoid: re-splicing this cube with a new domain must
// rewrite only this cube's block and leave every other line — a foreign
// cube's block included, and the trailing newline — byte for byte.
func TestCorefileSplicePreservesForeign(t *testing.T) {
	before := readFixture(t, "both-cubes.txt")
	after, err := gateway.CorefileSplice(before, testCube, "renamed.cube.test")
	if err != nil {
		t.Fatalf("CorefileSplice: %v", err)
	}
	if got, want := outsideMine(t, after), outsideMine(t, before); got != want {
		t.Errorf("content outside this cube's markers changed:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(after, `(.*)\.renamed\.cube\.test\.$`) {
		t.Error("this cube's own block was not rewritten to the new domain")
	}
	if strings.Contains(after, testDomain) {
		t.Errorf("the old domain survives the replace:\n%s", after)
	}
	if !strings.HasSuffix(after, "}\n") {
		t.Errorf("the trailing newline was not preserved: %q", after[len(after)-4:])
	}
}

// TestCorefileSpliceReplacesInMultiServerCorefile pins a decided
// asymmetry: the `.:53` count is consulted on the insert path only. Once
// this cube's markers fix the position there is nothing to choose, so a
// Corefile with two server blocks whose block is already spliced updates
// cleanly rather than failing.
func TestCorefileSpliceReplacesInMultiServerCorefile(t *testing.T) {
	in := readFixture(t, "kind-v1.35.0-spliced.txt") + readFixture(t, "kind-v1.35.0.txt")
	out, err := gateway.CorefileSplice(in, testCube, "renamed.cube.test")
	if err != nil {
		t.Fatalf("CorefileSplice over an already-spliced multi-server Corefile: %v", err)
	}
	if !strings.Contains(out, `(.*)\.renamed\.cube\.test\.$`) {
		t.Errorf("block not replaced:\n%s", out)
	}
}

// TestDomainEscaping covers the exotic end of what validation admits —
// lowercase RFC 1123 labels — and asserts the rewrite rule carries the
// escaped domain and never the raw one. The alphabet is why the fixture
// set is finite: the only character in it with meaning to regexp is the
// dot, and no brace, quote, whitespace, or newline can reach the splice.
func TestDomainEscaping(t *testing.T) {
	domains := []string{
		"cube",
		"mycube.cube.test",
		"a.b.c.d.e.f.example.internal",
		"cube-01.9lives-idp.test",
		"x.0",
		strings.Repeat("a", 63) + ".test",
	}
	base := readFixture(t, "kind-v1.35.0.txt")
	for _, domain := range domains {
		t.Run(domain, func(t *testing.T) {
			out, err := gateway.CorefileSplice(base, testCube, domain)
			if err != nil {
				t.Fatalf("CorefileSplice: %v", err)
			}
			rule := ruleRegex(t, out)
			want := `(.*)\.` + regexp.QuoteMeta(domain) + `\.$`
			if rule != want {
				t.Fatalf("rewrite regex = %q, want %q", rule, want)
			}
			// Everything between the rule's own capture group and anchor
			// is the domain, and once its escaped dots are removed no
			// regexp metacharacter may remain. The set deliberately
			// excludes the hyphen, which QuoteMeta leaves bare because it
			// means nothing outside a character class.
			body := strings.TrimSuffix(strings.TrimPrefix(rule, `(.*)\.`), `\.$`)
			if i := strings.IndexAny(strings.ReplaceAll(body, `\.`, ""), `.+*?()|[]{}^$\`); i >= 0 {
				t.Errorf("escaped domain %q carries an unescaped metacharacter at %d", body, i)
			}
			if strings.Contains(out, " "+domain+`\.$`) {
				t.Errorf("the raw domain reached the rule unescaped:\n%s", out)
			}
		})
	}
}

// readFixture reads a Corefile fixture verbatim.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "corefile", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

// assertCorefileStructureError asserts err is this domain's structural
// Corefile error, by code — never by message.
func assertCorefileStructureError(t *testing.T, err error) {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != gateway.CodeCorefileStructure {
		t.Fatalf("code = %s, want %s", coded.Code, gateway.CodeCorefileStructure)
	}
}

// outsideMine returns every line of corefile that is not inside this
// cube's marker block, which is the region the splice promises to leave
// untouched.
func outsideMine(t *testing.T, corefile string) string {
	t.Helper()
	var kept []string
	inside := false
	for _, line := range strings.Split(corefile, "\n") {
		switch strings.TrimSpace(line) {
		case "# cube-idp:begin " + testCube:
			inside = true
			continue
		case "# cube-idp:end " + testCube:
			inside = false
			continue
		}
		if !inside {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// ruleRegex returns the regex token of the spliced rewrite rule.
func ruleRegex(t *testing.T, corefile string) string {
	t.Helper()
	for _, line := range strings.Split(corefile, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == "name" && fields[1] == "regex" {
			if fields[3] != "gateway.gateway-system.svc.cluster.local." {
				t.Errorf("rewrite target = %q, want the stable Service's absolute FQDN", fields[3])
			}
			return fields[2]
		}
	}
	t.Fatalf("no rewrite rule in:\n%s", corefile)
	return ""
}
