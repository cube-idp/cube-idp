package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

// The marker vocabulary and the block's fixed indentation. Caddyfile
// whitespace is insignificant, but idempotency needs *a* fixed choice, and
// 4 spaces for the block's outer lines with 8 for the rule body matches
// the surrounding kubeadm style. The replace path re-renders the canonical
// block, so a hand-edited indentation is normalized on the next splice —
// everything between the markers is this domain's.
const (
	markerBeginPrefix = "# cube-idp:begin "
	markerEndPrefix   = "# cube-idp:end "
	blockIndent       = "    "
	ruleIndent        = "        "
)

// defaultServerToken opens CoreDNS's default server block, which is where
// the rewrite must land.
const defaultServerToken = ".:53"

// CorefileSplice returns corefile with this cube's marker-delimited
// rewrite block present exactly once inside the default server block:
// replaced if the cube's markers are already there, inserted directly
// after the `.:53 {` opening line if not. Everything outside this cube's
// markers — a foreign cube's block included — is preserved byte for byte.
// It is pure: the edge performs the read-modify-write.
//
// Callers pass a document-validated domain (lowercase RFC 1123 labels);
// a malformed one is a CUBE-CFG-* document error at load, never this
// function's business. Every structural fault the splice cannot act on is
// CUBE-GWY-004 — it never guesses which of two server blocks was meant,
// never repairs corrupted markers, and refuses markers that sit above
// every server block (requireServerBlockAbove, which records why that
// check stops where it does).
func CorefileSplice(corefile, cubeName, domain string) (string, error) {
	lines := strings.Split(corefile, "\n")
	begin, end, found, err := markerRange(lines, cubeName)
	if err != nil {
		return "", err
	}
	block := renderBlock(cubeName, domain)
	if found {
		if err := requireServerBlockAbove(lines, begin); err != nil {
			return "", err
		}
		return spliceLines(lines[:begin], block, lines[end+1:]), nil
	}
	open, err := defaultServerLine(lines)
	if err != nil {
		return "", err
	}
	return spliceLines(lines[:open+1], block, lines[open+1:]), nil
}

// renderBlock renders this cube's marker-delimited rewrite block. The
// target is spelled from ServiceFQDN — the stable Service, never an
// implementation Service and never derived from a release identity — so
// implementation swaps and pack renames never touch a live Corefile.
// `answer auto` rewrites the response name back, without which clients
// reject answers issued for the Service name.
func renderBlock(cubeName, domain string) []string {
	return []string{
		blockIndent + markerBeginPrefix + cubeName,
		blockIndent + "rewrite stop {",
		ruleIndent + `name regex (.*)\.` + regexp.QuoteMeta(domain) + `\.$ ` + ServiceFQDN,
		ruleIndent + "answer auto",
		blockIndent + "}",
		blockIndent + markerEndPrefix + cubeName,
	}
}

// markerRange locates this cube's marker block: its inclusive line range
// and found=true when exactly one intact pair is present, found=false when
// the cube has no markers at all, and CUBE-GWY-004 for every corrupted
// arrangement in between.
//
// Matching is keyed on the exact cube name, never on the `# cube-idp:`
// prefix: a foreign cube's block is invisible to every branch here, which
// is what makes preserving it byte-for-byte automatic rather than a
// special case.
func markerRange(lines []string, cubeName string) (begin, end int, found bool, err error) {
	begins := findLines(lines, markerBeginPrefix+cubeName)
	ends := findLines(lines, markerEndPrefix+cubeName)
	switch {
	case len(begins) == 0 && len(ends) == 0:
		return 0, 0, false, nil
	case len(begins) > 1 || len(ends) > 1:
		return 0, 0, false, newCorefileStructureError(fmt.Sprintf(
			"cube %q has %d begin and %d end markers; replacing one of several would leave a stale block live",
			cubeName, len(begins), len(ends)))
	case len(ends) == 0:
		return 0, 0, false, newCorefileStructureError(fmt.Sprintf(
			"marker %q has no matching %q", markerBeginPrefix+cubeName, markerEndPrefix+cubeName))
	case len(begins) == 0:
		return 0, 0, false, newCorefileStructureError(fmt.Sprintf(
			"marker %q has no matching %q", markerEndPrefix+cubeName, markerBeginPrefix+cubeName))
	case ends[0] < begins[0]:
		return 0, 0, false, newCorefileStructureError(fmt.Sprintf(
			"marker %q appears before %q; there is no region to replace",
			markerEndPrefix+cubeName, markerBeginPrefix+cubeName))
	}
	return begins[0], ends[0], true, nil
}

// requireServerBlockAbove asserts this cube's marker block sits below a
// server-block opening line. The splice only ever writes inside one, so
// markers above every `.:53 {` are corruption, not a layout the replace
// path may quietly honour: replacing there would emit a rewrite rule at
// Corefile top level, which CoreDNS rejects, and the operator would see a
// parse failure rather than a coded diagnosis.
//
// The check is deliberately prefix-only — "is there an opening line before
// the markers" — and never brace-depth containment. Caddyfile plugin
// arguments carry braces the splice has no grammar for (a regex quantifier
// such as {2} inside a rewrite rule is the obvious case), so counting
// braces is unsound; it would raise CUBE-GWY-004 on a valid Corefile, and
// a false structural error fails every bootstrap. Catching the gross
// corruption cheaply beats catching every case unsoundly.
func requireServerBlockAbove(lines []string, begin int) error {
	for _, line := range lines[:begin] {
		if isDefaultServerOpen(line) {
			return nil
		}
	}
	return newCorefileStructureError(
		"this cube's marker block sits above every `" + defaultServerToken +
			"` server block; the splice only ever writes inside one")
}

// defaultServerLine returns the index of the line opening the default
// server block. Absence and multiplicity are both CUBE-GWY-004: the block
// goes inside that server block, and silently picking one of two servers
// is exactly the silence this repo removes.
//
// The count is consulted on the insert path only — on the replace path the
// markers already fix the position, so a multi-server Corefile whose block
// is already spliced updates cleanly.
func defaultServerLine(lines []string) (int, error) {
	var opens []int
	for i, line := range lines {
		if isDefaultServerOpen(line) {
			opens = append(opens, i)
		}
	}
	switch len(opens) {
	case 0:
		return 0, newCorefileStructureError("no `" + defaultServerToken + "` server block to splice into")
	case 1:
		return opens[0], nil
	default:
		return 0, newCorefileStructureError(fmt.Sprintf(
			"found %d `%s` server blocks; the splice will not guess which one serves this cube", len(opens), defaultServerToken))
	}
}

// isDefaultServerOpen reports whether line opens the default server block.
//
// The brace spacing is deliberately not load-bearing: the insert path runs
// on every first bootstrap and has no fallback, so a future kind or
// kubeadm release emitting `.:53{` must not fail every fresh bootstrap
// with a structural error over a Corefile that looks perfectly normal. A
// genuinely different structure still fails loudly.
func isDefaultServerOpen(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, defaultServerToken) || !strings.HasSuffix(t, "{") {
		return false
	}
	return strings.TrimSpace(t[len(defaultServerToken):len(t)-1]) == ""
}

// findLines returns the indexes of every line equal to want once trimmed.
func findLines(lines []string, want string) []int {
	var found []int
	for i, line := range lines {
		if strings.TrimSpace(line) == want {
			found = append(found, i)
		}
	}
	return found
}

// spliceLines rejoins the Corefile around a freshly rendered block,
// copying rather than appending in place so the caller's slice is never
// aliased.
func spliceLines(before, block, after []string) string {
	out := make([]string, 0, len(before)+len(block)+len(after))
	out = append(out, before...)
	out = append(out, block...)
	out = append(out, after...)
	return strings.Join(out, "\n")
}
