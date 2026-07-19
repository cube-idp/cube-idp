# Docs & Comment Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Tasks marked **[WORKFLOW]** are executed by the orchestrator via the Workflow tool, not dispatched as single subagents.

**Goal:** Reconcile all documentation and code comments across the cube-idp repos with the current implementation — Truth Index as oracle, multi-agent audit, ADR extraction, per-site comment rewrite, CI recurrence guard.

**Architecture:** A Go extractor (`hack/truthindex/`) emits a deterministic JSON oracle of the product surface. Multi-agent workflows fan out claim-auditing, decision-harvesting, and comment-classification (read-only); application of edits is sequential per batch with mechanical gates. A shell guard (`hack/check-docs.sh`) enforces the end state in CI across all four repos.

**Tech Stack:** Go 1.26.2 (module `github.com/cube-idp/cube-idp`), cobra, `internal/diag` registry, bash, python3 (diff checker), Claude Workflow orchestration.

**Spec:** `docs/superpowers/specs/2026-07-19-docs-comment-audit-design.md` (AUD-1…AUD-9). Read it before executing any task.

## Global Constraints

- **No behaviour changes** (spec §3). The only permitted code additions: `render.JSONSchemas()` (Task 2, additive helper), `hack/truthindex/` (new tool), `hack/docaudit/` + `hack/check-docs.sh` (new tooling), CI wiring, and approved `internal/diag/registry.go` summary-string rewrites (spec component B/D amendment).
- **`CUBE-[0-9]{4}` is an allowlisted product identifier everywhere** (AUD-6). Any task that would change the set of declared code IDs has failed.
- **Comment rewrites derive from the cited passage** in `docs/archive/superpowers/`, never from memory (AUD-8). Unlocatable + no self-contained meaning → delete; unlocatable + apparent unique meaning → escalate.
- **`codes.go` trailing comments are load-bearing**: `// reserved:` markers are parsed by `internal/diag/codes_test.go` (`parseDefinedCodes`). Preserve them verbatim.
- **CHANGELOG** (AUD-9): strip planning IDs, correct factual errors, otherwise released entries stay verbatim.
- **Gates are hard stops.** Steps labeled `GATE` require operator approval before the next task starts.
- Each phase lands as its own PR from a branch named `audit/<phase-slug>` off `main`.
- Truth-index JSON must be **deterministic**: sorted keys/slices, no timestamps, no host paths.
- All commits end with the harness `Co-Authored-By` trailer.
- Working repo root for Tasks 1–16: `/Users/rafal.pieniazek/github.com/cube-idp/cube-idp`. Siblings (Tasks 17–19): `../packs`, `../plugins`, `../cube-idp-web`.

## Coordination model (multi-agent, opted in)

| Where | Mode | Why |
| --- | --- | --- |
| Task 6 audit, Task 8 harvest, Task 9 validate, Task 12 classify | **[WORKFLOW]** parallel fan-out + adversarial verify | Independent read-only work items; refutation counters hallucinated agreement |
| Task 11 ADR drafting | Parallel subagents, one per ADR | Independent documents |
| Edit application (Tasks 13–15), archive move, guard flip | Sequential, single agent per batch | Shared working tree; ordering matters |

Workflow scripts receive file lists via `args` (scripts cannot touch the filesystem; agents read files themselves). Agent outputs use `schema` — no free-text parsing.

---

## Phase 1 — Truth Index, pattern list, audit, findings report

### Task 1: Truth-index extractor (`hack/truthindex/`)

**Files:**
- Create: `hack/truthindex/main.go`
- Create: `hack/truthindex/main_test.go`

**Interfaces:**
- Consumes: `cmd.NewRootCmd() *cobra.Command` (`cmd/root.go:26`), `diag.AllCodes() []diag.Code`, `diag.Describe(diag.Code) (diag.Desc, bool)` (`internal/diag/registry.go`), `config.Cube{}` (`internal/config/types.go:7`), `pack.Pack{}` (`internal/pack/`).
- Produces: `hack/truth-index.json`; CLI modes `-out <path>`, `-check`, `-codes-only`. Struct `Index{Commands []Command; DiagCodes []DiagCode; ConfigSchema []Field; PackContract []Field; ExitContract map[string]string; OutputSchemas map[string][]Field}`.

- [ ] **Step 1: Confirm the `pack.Pack` exported struct name**

Run: `grep -nE '^type (Pack|Catalog)' internal/pack/*.go | grep -v _test`
Expected: a `type Pack struct` line. If the loader's contract struct has a different name, substitute it consistently in the code below — nothing else about the task changes.

- [ ] **Step 2: Write the failing test**

```go
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./hack/truthindex/ -v`
Expected: FAIL — `buildIndex` undefined.

- [ ] **Step 4: Implement the extractor**

```go
// hack/truthindex/main.go
// Command truthindex emits a deterministic JSON description of cube-idp's
// user-visible surface (commands, flags, diagnostic codes, config schema,
// pack contract, exit contract, machine-output shapes), extracted from the
// real packages — never from prose. It is the oracle the docs audit and the
// check-docs guard compare documentation claims against.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cube-idp/cube-idp/cmd"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui/render"
)

type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

type Command struct {
	Path    string `json:"path"` // e.g. "cube-idp pack push"
	Use     string `json:"use"`
	Short   string `json:"short"`
	Aliases []string `json:"aliases,omitempty"`
	Flags   []Flag `json:"flags,omitempty"`
}

type DiagCode struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Range       string `json:"range"`
}

type Field struct {
	Path string `json:"path"` // dotted, e.g. "spec.cluster.provider"
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}

type Index struct {
	Commands      []Command          `json:"commands"`
	DiagCodes     []DiagCode         `json:"diagCodes"`
	ConfigSchema  []Field            `json:"configSchema"`
	PackContract  []Field            `json:"packContract"`
	ExitContract  map[string]string  `json:"exitContract"`
	OutputSchemas map[string][]Field `json:"outputSchemas"`
}

func walkCommands(c *cobra.Command, prefix string, out *[]Command) {
	name := c.Name()
	path := strings.TrimSpace(prefix + " " + name)
	var flags []Flag
	collect := func(f *pflag.Flag) {
		flags = append(flags, Flag{Name: f.Name, Shorthand: f.Shorthand,
			Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage})
	}
	c.LocalFlags().VisitAll(collect)
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	*out = append(*out, Command{Path: path, Use: c.Use, Short: c.Short,
		Aliases: append([]string(nil), c.Aliases...), Flags: flags})
	kids := c.Commands()
	sort.Slice(kids, func(i, j int) bool { return kids[i].Name() < kids[j].Name() })
	for _, k := range kids {
		if k.Hidden {
			continue
		}
		walkCommands(k, path, out)
	}
}

// reflectSchema flattens a struct into dotted field paths. It follows the
// yaml tag when present (that is the name users write in cube.yaml /
// pack.cue), else the json tag, else the Go field name.
func reflectSchema(t reflect.Type, prefix string, out *[]Field) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		tag := f.Tag.Get("yaml")
		if tag == "" {
			tag = f.Tag.Get("json")
		}
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if tag == "-" {
			continue
		}
		if tag != "" {
			name = tag
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Map {
			ft = ft.Elem()
		}
		*out = append(*out, Field{Path: path, Type: f.Type.String(), Tag: tag})
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "" &&
			strings.HasPrefix(ft.PkgPath(), "github.com/cube-idp/") {
			reflectSchema(ft, path, out)
		}
	}
}

func buildIndex() Index {
	var cmds []Command
	walkCommands(cmd.NewRootCmd(), "", &cmds)

	var codes []DiagCode
	for _, c := range diag.AllCodes() {
		d, _ := diag.Describe(c)
		codes = append(codes, DiagCode{ID: string(c), Summary: d.Summary,
			Detail: d.Detail, Remediation: d.Remediation, Range: diag.RangeMeaning(c)})
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i].ID < codes[j].ID })

	var cfg, pk []Field
	reflectSchema(reflect.TypeOf(config.Cube{}), "", &cfg)
	reflectSchema(reflect.TypeOf(pack.Pack{}), "", &pk)

	out := map[string][]Field{}
	for _, s := range render.JSONSchemas() {
		t := reflect.TypeOf(s)
		var fs []Field
		reflectSchema(t, "", &fs)
		out[t.Name()] = fs
	}

	return Index{
		Commands:     cmds,
		DiagCodes:    codes,
		ConfigSchema: cfg,
		PackContract: pk,
		// Static mirror of cmd.ExitCodeFor (cmd/exit.go): keep in sync by eye;
		// the mapping is three arms and changes rarely.
		ExitContract: map[string]string{
			"success":           "0",
			"diagnostic_error":  "1 (rendered)",
			"exit_sentinel":     "N (unrendered; diff/doctor/upgrade drift signals)",
			"plugin_exit_error": "N (unrendered; plugin's own code propagates verbatim)",
		},
		OutputSchemas: out,
	}
}

func main() {
	outPath := flag.String("out", "hack/truth-index.json", "output path")
	check := flag.Bool("check", false, "verify committed index matches a fresh extraction")
	codesOnly := flag.Bool("codes-only", false, "print sorted diagnostic-code IDs, one per line")
	flag.Parse()

	idx := buildIndex()

	if *codesOnly {
		for _, c := range idx.DiagCodes {
			fmt.Println(c.ID)
		}
		return
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *check {
		committed, err := os.ReadFile(*outPath)
		if err != nil || string(committed) != string(data) {
			fmt.Fprintln(os.Stderr, "truth-index drift: regenerate with `make truth-index`")
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Note: this will not compile until Task 2 adds `render.JSONSchemas()`. That is expected TDD order — Task 2 is the minimal change that makes this task's tests pass. If `pack.Pack` has a different name (Step 1), substitute it.

- [ ] **Step 5: Commit the red state**

```bash
git checkout -b audit/phase-1-oracle
git add hack/truthindex/
git commit -m "test: truth-index extractor skeleton (red)"
```

### Task 2: `render.JSONSchemas()` helper + green extractor

**Files:**
- Modify: `internal/ui/render/json.go` (append at end)
- Create: `internal/ui/render/schemas_test.go`
- Create: `hack/truth-index.json` (generated)
- Modify: `Makefile` (add `truth-index` target after `test:` block)

**Interfaces:**
- Produces: `func JSONSchemas() []any` returning zero values of every `json*` envelope struct in `internal/ui/render/json.go` (12 as of today: `jsonHead`, `jsonRunStarted`, `jsonStep`, `jsonStepFailed`, `jsonComponent`, `jsonHealthTick`, `jsonMsg`, `jsonEpilogue`, `jsonPack`, `jsonAccess`, `jsonRunDone`, `jsonDiagnosis`).

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/render/schemas_test.go
package render

import "testing"

func TestJSONSchemasCoversEveryEnvelopeType(t *testing.T) {
	// If a new json* envelope struct is added to json.go without being
	// registered here, the truth index silently under-reports the machine
	// output surface. Count is asserted so the failure names the gap.
	if got := len(JSONSchemas()); got != 12 {
		t.Fatalf("JSONSchemas() = %d entries, want 12 — update JSONSchemas() and this count together", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/render/ -run TestJSONSchemasCovers -v`
Expected: FAIL — `JSONSchemas` undefined.

- [ ] **Step 3: Implement**

```go
// appended to internal/ui/render/json.go

// JSONSchemas returns a zero value of every JSON envelope type this
// package emits under --output json. It exists for hack/truthindex, which
// reflects over these values to record the machine-output surface;
// behaviourally it is dead code.
func JSONSchemas() []any {
	return []any{
		jsonHead{}, jsonRunStarted{}, jsonStep{}, jsonStepFailed{},
		jsonComponent{}, jsonHealthTick{}, jsonMsg{}, jsonEpilogue{},
		jsonPack{}, jsonAccess{}, jsonRunDone{}, jsonDiagnosis{},
	}
}
```

- [ ] **Step 4: Run the full loop green**

Run: `go test ./internal/ui/render/ ./hack/truthindex/ -v && go build ./...`
Expected: all PASS. If `reflectSchema` panics on an unexpected kind in a field (e.g. `time.Time`), guard with the existing `PkgPath()` prefix check — only `github.com/cube-idp/` structs recurse.

- [ ] **Step 5: Add Makefile target and generate**

```makefile
truth-index:
	go run ./hack/truthindex -out hack/truth-index.json

truth-index-check:
	go run ./hack/truthindex -check
```

Run: `make truth-index && make truth-index-check && git diff --stat`
Expected: `hack/truth-index.json` created; check exits 0.

- [ ] **Step 6: Spot-check the oracle against three known facts**

Run: `python3 -c "
import json; idx=json.load(open('hack/truth-index.json'))
paths=[c['path'] for c in idx['commands']]
assert 'cube-idp up' in paths and 'cube-idp explain' in paths
assert not any('migrate' in p for p in paths), 'migrate must NOT exist'
assert any(d['id']=='CUBE-1003' for d in idx['diagCodes'])
print('oracle sane:', len(paths), 'commands,', len(idx['diagCodes']), 'codes')
"`
Expected: `oracle sane: <N> commands, <M> codes` with M ≥ 100.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/render/ hack/ Makefile
git commit -m "feat(hack): truth-index extractor — machine oracle of the product surface"
```

### Task 3: Shared pattern list + comment-site inventory

**Files:**
- Create: `hack/docaudit/patterns.txt`
- Create: `hack/docaudit/allowlist-paths.txt`
- Create: `hack/docaudit/inventory.sh`

**Interfaces:**
- Produces: `hack/docaudit/inventory.sh [repo-root]` → JSONL on stdout, one object per site: `{"file":"internal/up/up.go","line":459,"text":"...","pattern":"\\bP[0-9]\\b"}`. Consumed by Task 6 (stats), Task 12 (classification input), Task 16 (guard).

- [ ] **Step 1: Write the pattern and allowlist files**

```text
# hack/docaudit/patterns.txt — one ERE per line, '#' comments allowed.
# These are PLANNING-ARTIFACT patterns. CUBE-[0-9]{4} is a product
# identifier (diagnostic codes) and must never appear here (AUD-6).
\bTask [0-9]+(\.[0-9]+[a-z]?)?\b
\bTE-[0-9]+(\.[0-9]+)?\b
\bGT[0-9]{1,3}\b
\b[Pp]hase[- ][0-9]\b
\bP[0-9]\b
\bWP[0-9]+\b
\bcheckpoint [0-9]+(\.[0-9]+)?\b
\bdecision [0-9]+\b
\(D[0-9]+\)
\bF[0-9]{1,2}:\s
docs/superpowers/(plans|specs|research)/
spec §
design doc §
```

```text
# hack/docaudit/allowlist-paths.txt — path regexes exempt from pattern scan
^docs/superpowers/
^docs/archive/
^docs/adr/
^hack/docaudit/
^hack/truth-index\.json$
^CHANGELOG\.md$
/testdata/
^package-lock\.json$
\.git/
node_modules/
```

Known false-positive risk, accepted deliberately: `\bP[0-9]\b` and `\bF[0-9]{1,2}:` can
match non-planning text. The inventory is an *input to human/agent classification*
(Task 12 judges every site), not an auto-edit list, so precision beats recall only at
the guard stage — Task 16 revisits FP handling with an inline-waiver comment syntax.
`R[0-9]` (as in "TE-3.4 / R3") is deliberately absent: too FP-prone alone; its
co-occurring `TE-` pattern catches those lines.

- [ ] **Step 2: Write the inventory script**

```bash
#!/usr/bin/env bash
# hack/docaudit/inventory.sh [repo-root]
# Emits one JSON object per planning-pattern hit in source comments and
# scripts, excluding allowlisted paths. Read-only; used by the audit, the
# classification fan-out, and (report-only) the recurrence guard.
set -euo pipefail
root="${1:-.}"
here="$(cd "$(dirname "$0")" && pwd)"
cd "$root"

pat="$(grep -v '^#' "$here/patterns.txt" | grep -v '^$' | paste -sd'|' -)"
allow="$(grep -v '^#' "$here/allowlist-paths.txt" | grep -v '^$' | paste -sd'|' -)"

git ls-files -- '*.go' '*.sh' '*.yaml' '*.yml' '*.mjs' '*.astro' '*.ts' '*.md' 'Makefile' \
  | grep -Ev "$allow" \
  | while IFS= read -r f; do
      # `|| true`: under `set -e -o pipefail` a no-match grep (exit 1) would
      # otherwise abort the whole scan at the first clean file.
      { grep -nE "$pat" "$f" 2>/dev/null || true; } | while IFS=: read -r ln text; do
        matched="$(printf '%s' "$text" | grep -oE "$pat" | head -1 || true)"
        python3 - "$f" "$ln" "$text" "$matched" <<'PY'
import json,sys
print(json.dumps({"file":sys.argv[1],"line":int(sys.argv[2]),
                  "text":sys.argv[3][:400],"pattern":sys.argv[4]}))
PY
      done
    done
```

Run: `chmod +x hack/docaudit/inventory.sh`

- [ ] **Step 3: Verify against known ground truth**

Run: `hack/docaudit/inventory.sh . > /tmp/sites.jsonl; wc -l /tmp/sites.jsonl; grep -c 'up.go' /tmp/sites.jsonl`
Expected: total in the 600–900 range (≈790 measured pre-plan, minus allowlisted CHANGELOG/superpowers hits); `internal/up/up.go` present. **If the count is 0 or absurdly low:** this host's BSD grep is mishandling `\b` — verify with `echo 'see Task 9 here' | grep -E '\bTask [0-9]+\b'`; if that prints nothing, replace every `\b` in `patterns.txt` with `(^|[^[:alnum:]_])` / `([^[:alnum:]_]|$)` guards (adjusting `grep -oE` extraction accordingly) or run via GNU grep (`ggrep`). Sanity: `grep 'CUBE-1003' /tmp/sites.jsonl | head -1` — a line may legitimately appear when it ALSO carries a planning pattern, but run `grep -E '"pattern":"CUBE' /tmp/sites.jsonl | wc -l` and expect **0** (CUBE never a pattern).

- [ ] **Step 4: Commit**

```bash
git add hack/docaudit/
git commit -m "feat(hack): docaudit pattern list + comment-site inventory"
```

### Task 4: Comment-only diff verifier

**Files:**
- Create: `hack/docaudit/comment_only_diff.py`

**Interfaces:**
- Produces: `python3 hack/docaudit/comment_only_diff.py [--allow-file <path>]... <base-ref>` — exits 1 if `git diff <base-ref>` touches any non-comment content in `.go` files (files given via `--allow-file` are exempt). Consumed by Tasks 13–15 batch verification.

- [ ] **Step 1: Write the checker**

```python
#!/usr/bin/env python3
"""Verify a git diff only changes Go comments.

Rules per changed .go file (unless --allow-file):
  - added/removed lines that are blank or whole-line comments: OK
  - paired +/- lines whose content BEFORE the first `//` is identical: OK
    (trailing-comment edits, e.g. codes.go constants)
  - anything else: violation.
Approximation is deliberate: `//` inside string literals is rare in this
codebase's comments and a false *positive* here just asks for human review.
"""
import argparse, re, subprocess, sys

def code_part(line: str) -> str:
    idx = line.find("//")
    return (line if idx < 0 else line[:idx]).rstrip()

def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--allow-file", action="append", default=[])
    ap.add_argument("base")
    a = ap.parse_args()
    diff = subprocess.run(["git", "diff", "-U0", a.base, "--", "*.go"],
                          capture_output=True, text=True, check=True).stdout
    violations, current, removed, added = [], None, [], []

    def flush():
        n = max(len(removed), len(added))
        for i in range(n):
            r = removed[i] if i < len(removed) else ""
            d = added[i] if i < len(added) else ""
            for ln in (r, d):
                s = ln.strip()
                if s == "" or s.startswith("//") or s.startswith("*") \
                   or s.startswith("/*") or s.endswith("*/"):
                    continue
                if code_part(r) == code_part(d) and code_part(d) != "":
                    continue  # trailing-comment edit, code identical
                violations.append(f"{current}: -{r!r} +{d!r}")
        removed.clear(); added.clear()

    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            flush(); current = line[6:]
        elif line.startswith("@@"):
            flush()
        elif current in a.allow_file:
            continue
        elif line.startswith("-") and not line.startswith("---"):
            removed.append(line[1:])
        elif line.startswith("+") and not line.startswith("+++"):
            added.append(line[1:])
    flush()
    for v in violations[:40]:
        print("NON-COMMENT CHANGE:", v)
    return 1 if violations else 0

if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Self-test with a synthetic violation**

Run:
```bash
sed -i '' 's/^func main/func main /' hack/truthindex/main.go   # whitespace-in-code change
python3 hack/docaudit/comment_only_diff.py HEAD; echo "exit=$?"
git checkout hack/truthindex/main.go
```
Expected: `NON-COMMENT CHANGE:` line printed, `exit=1`. Then clean tree → run again → `exit=0`.

- [ ] **Step 3: Commit**

```bash
git add hack/docaudit/comment_only_diff.py
git commit -m "feat(hack): comment-only diff verifier for the cleanup batches"
```

### Task 5: Baseline snapshot

**Files:**
- Create: `/private/tmp/…/scratchpad/audit/baseline-codes.txt` (scratchpad, not committed)

- [ ] **Step 1: Record baselines**

```bash
S=/private/tmp/claude-501/-Users-rafal-pieniazek-Library-CloudStorage-Dropbox-github-com-cube-idp/c08d12b0-e00a-472a-8c94-fa3349426db6/scratchpad/audit
mkdir -p "$S"
go run ./hack/truthindex -codes-only > "$S/baseline-codes.txt"
wc -l "$S/baseline-codes.txt"          # expect ≥ 100
go build ./... && go test ./... 2>&1 | tail -3   # expect ok across packages
go run . explain CUBE-0007 > "$S/baseline-explain-0007.txt"
go run . explain CUBE-0003 > "$S/baseline-explain-0003.txt"   # a leak-carrying summary
hack/docaudit/inventory.sh . > "$S/sites.jsonl"
```
Expected: tests pass (record any pre-existing failures verbatim in the findings report — they are baseline, not caused by this project). GATE-relevant artifacts now exist.

### Task 6: **[WORKFLOW]** Contradiction audit fan-out

**Files:**
- Create: `/…/scratchpad/audit/findings-raw.json` (workflow output, scratchpad)

**Interfaces:**
- Consumes: `hack/truth-index.json`, `$S/sites.jsonl`.
- Produces: verified findings array `{repo,file,line,claim,codeReality,codeRef,bucket,proposedResolution,confidence,verdicts[]}` — buckets exactly `stale-doc|suspected-bug|planning-leak|dangling|unverifiable` (spec component B).

- [ ] **Step 1: Launch the audit workflow**

Orchestrator runs the Workflow tool with this script, passing clusters via `args`:

```js
export const meta = {
  name: 'docs-contradiction-audit',
  description: 'Extract checkable claims per doc cluster, adversarially verify every finding',
  phases: [
    { title: 'Extract', detail: 'one auditor per doc cluster' },
    { title: 'Verify', detail: 'two independent skeptics per finding' },
  ],
}
const FINDINGS = { type:'object', required:['findings'], properties:{ findings:{ type:'array', items:{
  type:'object',
  required:['repo','file','line','claim','codeReality','codeRef','bucket','proposedResolution','confidence'],
  properties:{
    repo:{type:'string'}, file:{type:'string'}, line:{type:'integer'},
    claim:{type:'string'}, codeReality:{type:'string'}, codeRef:{type:'string'},
    bucket:{enum:['stale-doc','suspected-bug','planning-leak','dangling','unverifiable']},
    proposedResolution:{type:'string'}, confidence:{enum:['high','medium','low']},
  }}}}}
const VERDICT = { type:'object', required:['refuted','reason'],
  properties:{ refuted:{type:'boolean'}, reason:{type:'string'},
               reclassify:{enum:['stale-doc','suspected-bug','planning-leak','dangling','unverifiable','']}}}

const results = await pipeline(args.clusters,
  c => agent(
`You are auditing documentation against code reality for the cube-idp project.
Cluster: ${c.id}. Repo root: ${c.root}. Files to audit exhaustively: ${c.files.join(', ')}.
Oracle: read ${args.oracle} (truth-index.json) — commands/flags, diagnostic codes,
config schema, pack contract, exit contract, machine-output shapes, all extracted from code.

For EVERY factual claim in these files (a command exists, a flag does X, a config field
means Y, an error code fires when Z, a file exists at path P, behaviour B happens):
1. Check it against the oracle first; where the oracle is silent, read the implementing
   code directly and cite it as file:line in codeRef.
2. Classify into exactly one bucket:
   stale-doc (doc describes old behaviour; code is ground truth),
   suspected-bug (doc describes DELIBERATE specified behaviour code doesn't do — do not propose doc edits for these),
   planning-leak (process residue: task/phase/gate IDs like Task 9, P7, GT16, (D5), WP8, references to docs/superpowers/ paths — including USER-FACING strings in internal/diag/registry.go summaries),
   dangling (points at a file/command/code that does not exist),
   unverifiable (cannot be proven either way — say why).
3. CUBE-[0-9]{4} identifiers are PRODUCT diagnostic codes, never planning residue.
4. proposedResolution: the exact edit you would make (or 'report only' for suspected-bug).
Only claims that are WRONG or residue become findings — do not report claims that check out.
Return only the structured findings.`,
    { label:`audit:${c.id}`, phase:'Extract', schema:FINDINGS }),
  (r, c) => parallel((r?.findings ?? []).map(f => () =>
    parallel(['code-reality','doc-intent'].map(lens => () => agent(
`Adversarially verify this docs-audit finding through the ${lens} lens. Try to REFUTE it.
Finding: ${JSON.stringify(f)}
${lens === 'code-reality'
  ? 'Read the cited code (and oracle at '+args.oracle+') yourself. Is codeReality actually what the code does? Is the claim actually in the doc at that file:line?'
  : 'Read the surrounding doc context. Is the claim quoted fairly, or torn from a context that makes it true? Is the bucket right — e.g. is this deliberate spec (suspected-bug) rather than stale-doc?'}
If the finding is wrong, refuted=true with the disproof. If the bucket is wrong but the
finding is real, refuted=false and set reclassify. Default to refuted=true if uncertain.`,
      { label:`verify:${(f.file||'').split('/').pop()}:${f.line}`, phase:'Verify', schema:VERDICT })))
      .then(vs => ({ ...f, cluster:c.id, verdicts: vs.filter(Boolean) }))
  ))
)
const flat = results.filter(Boolean).flat().filter(Boolean)
const kept = flat.filter(f => f.verdicts.filter(v => v.refuted).length < 2)
log(`findings: ${flat.length} raw, ${kept.length} survived adversarial verify`)
return { findings: kept, refuted: flat.length - kept.length }
```

`args`:
```json
{
  "oracle": "/Users/rafal.pieniazek/github.com/cube-idp/cube-idp/hack/truth-index.json",
  "clusters": [
    {"id":"cli-readme",  "root":"/Users/rafal.pieniazek/github.com/cube-idp/cube-idp",      "files":["README.md"]},
    {"id":"cli-docs",    "root":"/Users/rafal.pieniazek/github.com/cube-idp/cube-idp",      "files":["docs/pack-contract-v1.md","docs/machine-readable-output.md","docs/outstanding-todos.md","tests/e2e/PACKS.md","CHANGELOG.md"]},
    {"id":"diag-registry","root":"/Users/rafal.pieniazek/github.com/cube-idp/cube-idp",     "files":["internal/diag/registry.go","internal/diag/codes.go"]},
    {"id":"packs",       "root":"/Users/rafal.pieniazek/github.com/cube-idp/packs",          "files":["CONTRACT.md","README.md","packs/*/README.md","packs/*/pack.cue",".github/workflows/*.yml","hack/*.sh"]},
    {"id":"plugins",     "root":"/Users/rafal.pieniazek/github.com/cube-idp/plugins",        "files":["CONTRACT-PLUGINS.md","USE-CASES.md","README.md","hack/*.sh",".github/workflows/*.yml"]},
    {"id":"web",         "root":"/Users/rafal.pieniazek/github.com/cube-idp/cube-idp-web",   "files":["README.md","src/pages/index.astro","src/pages/docs/get-started.astro","src/pages/docs/json.md","src/pages/404.astro"]}
  ]
}
```
(A verdict-majority of 2/2 refutations kills a finding; 1/2 keeps it flagged with both verdicts attached. `reclassify` votes are applied at assembly when both verifiers agree.)

- [ ] **Step 2: Persist raw output**

Save the workflow return value to `$S/findings-raw.json`. Read the workflow `journal.jsonl` if the result looks empty before diagnosing.

### Task 7: Findings report assembly + **GATE 1**

**Files:**
- Create: `docs/superpowers/research/2026-07-20-docs-audit-findings.md`

- [ ] **Step 1: Assemble the report**

Single agent merges `$S/findings-raw.json` + `$S/sites.jsonl` stats into the report, structured as:

```markdown
# Docs Audit Findings — 2026-07-20
## Summary table (counts per repo × bucket)
## 1. suspected-bug (report-only — operator triage required)   ← FIRST, most important
## 2. stale-doc
## 3. dangling
## 4. planning-leak (doc prose + registry summaries)
## 5. unverifiable (operator judgement required)
## 6. Comment-site inventory stats (from sites.jsonl, per package × pattern)
## 7. Baseline notes (pre-existing test failures, if any)
```
Each row: `repo · file:line · claim · code reality (code file:line) · proposed resolution · confidence · verifier notes`. Apply agreed `reclassify` votes. Sort each section by file then line. Findings with 1/2 refutation get a `⚠ contested` marker.

- [ ] **Step 2: Commit and stop**

```bash
git add docs/superpowers/research/2026-07-20-docs-audit-findings.md
git commit -m "docs(audit): phase-1 findings report"
git push -u origin audit/phase-1-oracle
```

**GATE 1 (AUD-5): STOP.** Open the PR for phase 1, present the findings report to the operator. No document or comment edit happens until the operator approves the report, triages `suspected-bug` and `unverifiable` rows, and marks each `planning-leak` registry-summary rewrite approved/rejected. Record decisions inline in the report (a `Decision:` suffix per row), commit the annotated report.

---

## Phase 2 — ADRs + archive

### Task 8: **[WORKFLOW]** Decision harvest

**Interfaces:**
- Produces: `$S/decision-candidates.json`: array of `{id,statement,sourceFile,sourceLine,quote,markers[]}`.

- [ ] **Step 1: Launch harvest workflow**

```js
export const meta = {
  name: 'decision-harvest',
  description: 'Mine plans/specs/research for decision-shaped statements',
  phases: [{ title: 'Harvest', detail: 'one agent per document' }],
}
const CANDIDATES = { type:'object', required:['candidates'], properties:{ candidates:{ type:'array', items:{
  type:'object', required:['statement','sourceFile','sourceLine','quote'],
  properties:{ statement:{type:'string'}, sourceFile:{type:'string'}, sourceLine:{type:'integer'},
               quote:{type:'string'}, markers:{type:'array', items:{type:'string'}} }}}}}
const out = await parallel(args.files.map(f => () => agent(
`Read ${f} in full. Extract every DESIGN DECISION it records — a choice between
alternatives that constrains the system (naming, schema shapes, error-code policy,
delivery mechanisms, frozen APIs, guarantees). The corpus self-marks many:
'decision N', '(DN)', 'GTn', 'spec decision', 'we chose', 'instead of', 'MUST'.
For each: a one-sentence normative statement (present tense, testable), the exact
source line, a ≤3-line verbatim quote, and any marker IDs. Do NOT include task
sequencing, estimates, or process instructions — only decisions about the product.
Return only structured candidates.`,
  { label:`harvest:${f.split('/').pop()}`, phase:'Harvest', schema:CANDIDATES }
)))
const all = out.filter(Boolean).flatMap(r => r.candidates)
  .map((c,i) => ({ id:`CAND-${String(i+1).padStart(3,'0')}`, ...c }))
log(`harvested ${all.length} candidates from ${args.files.length} documents`)
return { candidates: all }
```

`args.files`: every file under `docs/superpowers/plans/`, `docs/superpowers/specs/`, `docs/superpowers/research/` **except** the audit spec, this plan, and the findings report (they stay live). ~33 files, one agent each. Save output to `$S/decision-candidates.json`.

### Task 9: **[WORKFLOW]** Candidate validation + **GATE 2**

**Interfaces:**
- Produces: `$S/decision-verdicts.json`: candidates + `{status: binding|superseded|abandoned|unverifiable, evidence, codeRef, supersededBy}`.

- [ ] **Step 1: Dedup mechanically, then validate**

Orchestrator dedups candidates by normalized statement (same file ± same marker → keep first) in plain code, then runs:

```js
export const meta = {
  name: 'decision-validate',
  description: 'Validate each decision candidate against the truth index and code',
  phases: [{ title: 'Validate', detail: 'one verifier per candidate' }],
}
const VERDICT = { type:'object', required:['status','evidence'],
  properties:{ status:{enum:['binding','superseded','abandoned','unverifiable']},
    evidence:{type:'string'}, codeRef:{type:'string'}, supersededBy:{type:'string'} }}
const out = await parallel(args.candidates.map(c => () => agent(
`Decision candidate ${c.id}: "${c.statement}" (from ${c.sourceFile}:${c.sourceLine}).
Determine its status TODAY, conservatively:
- binding: the code implements it now. Cite the implementing code file:line in codeRef
  (verify against ${args.oracle} where applicable). No proof → NOT binding.
- superseded: a later decision replaced it — name what replaced it in supersededBy.
- abandoned: never built, or built and removed.
- unverifiable: cannot be proven from code either way. When torn, choose this.
evidence: 2-3 sentences with concrete file:line references.`,
  { label:`validate:${c.id}`, phase:'Validate', schema:VERDICT }
).then(v => v && ({ ...c, ...v }))))
const done = out.filter(Boolean)
log(`binding:${done.filter(c=>c.status==='binding').length} superseded:${done.filter(c=>c.status==='superseded').length} abandoned:${done.filter(c=>c.status==='abandoned').length} unverifiable:${done.filter(c=>c.status==='unverifiable').length}`)
return { candidates: done }
```

- [ ] **Step 2: GATE 2 — operator triage**

Present the operator: the `binding` list (will become ADRs) and the `unverifiable` queue (expected ~15–40). The operator promotes/demotes; record the final list in `$S/decision-verdicts.json`. **STOP until answered.**

### Task 10: Write ADRs

**Files:**
- Create: `docs/adr/` (bootstrap via `adr-skill`: index + template per its conventions)
- Create: one `docs/adr/NNNN-<slug>.md` per approved binding decision

- [ ] **Step 1: Bootstrap the ADR folder** — invoke `adr-skill` to scaffold `docs/adr/` with its index format. Status of migrated records: `accepted` (they describe decisions already in force; the origin plan date is the decision date).

- [ ] **Step 2: Draft in parallel** — one subagent per ADR. Prompt template (verbatim per dispatch, filling `{}`):

> Write ADR `{NNNN}-{slug}.md` in the repo's ADR format for this accepted decision: "{statement}". Context comes from {sourceFile}:{sourceLine} (quote: "{quote}") — read the surrounding section in `docs/superpowers/…` for the why. The Consequences/Evidence section MUST cite the implementing code `{codeRef}` and, where relevant, the oracle field in `hack/truth-index.json`. Origin section MUST cite the source plan file:line. Do not invent rationale absent from the source — write "rationale not recorded" instead.

- [ ] **Step 3: Verify every ADR mechanically**

Run: `python3 -c "
import re,glob,os,sys
bad=[]
for f in glob.glob('docs/adr/[0-9]*.md'):
    t=open(f).read()
    for m in re.findall(r'\`([a-zA-Z0-9_/.-]+\.(?:go|cue|md|sh|yaml)):[0-9]+\`', t):
        if not os.path.exists(m): bad.append((f,m))
sys.exit(1 if bad else print('all ADR code citations resolve') or 0)"`
Expected: `all ADR code citations resolve`. A human-review pass of each ADR against its source quote follows (dispatch one reviewer subagent over the whole set: "does each ADR say only what its origin says?").

- [ ] **Step 4: GATE 3 — operator reviews the ADR set.** STOP. Then commit:

```bash
git checkout -b audit/phase-2-adrs
git add docs/adr/
git commit -m "docs(adr): extract still-binding decisions from planning corpus"
```

### Task 11: Archive move

**Files:**
- Create: `docs/archive/superpowers/README.md`
- Move: `docs/superpowers/{plans,specs,research}/**` → `docs/archive/superpowers/{plans,specs,research}/**` — **except** the audit spec, this plan, and the findings report (exempt until phase 7).

- [ ] **Step 1: Move with history**

```bash
mkdir -p docs/archive/superpowers
git mv docs/superpowers/plans docs/archive/superpowers/plans
git mv docs/superpowers/specs docs/archive/superpowers/specs
git mv docs/superpowers/research docs/archive/superpowers/research
mkdir -p docs/superpowers/plans docs/superpowers/specs docs/superpowers/research
git mv docs/archive/superpowers/specs/2026-07-19-docs-comment-audit-design.md docs/superpowers/specs/
git mv docs/archive/superpowers/plans/2026-07-20-docs-comment-audit.md docs/superpowers/plans/
git mv docs/archive/superpowers/research/2026-07-20-docs-audit-findings.md docs/superpowers/research/
```

- [ ] **Step 2: Write the archive README**

```markdown
# Archived planning corpus

Everything under this directory is **historical and non-authoritative**. These
plans, specs, and research notes describe the system as it was being designed,
phase by phase — not as it is. They are kept for archaeology, verbatim.

Authoritative sources today:

- what the product does: the code, and `hack/truth-index.json` (generated from it)
- why it does it that way: `docs/adr/`
- how to use it: `README.md`, `docs/pack-contract-v1.md`, `docs/machine-readable-output.md`

Nothing in here may be cited as authority for new work. If a decision recorded
here still matters and has no ADR, that is a gap in `docs/adr/` — fix it there.
```

- [ ] **Step 3: Verify no live doc links into the moved paths, then commit**

Run: `grep -rn 'docs/superpowers/\(plans\|specs\|research\)/' README.md docs/*.md docs/adr/ --include='*.md' | grep -v superpowers/research/2026-07-20 | grep -v specs/2026-07-19-docs-comment | grep -v plans/2026-07-20-docs-comment`
Expected: hits only in `docs/outstanding-todos.md` (fixed in phase 4, Task 15) — record them; anything else gets re-pointed to `docs/archive/...` now.

```bash
git add -A docs/
git commit -m "docs: archive planning corpus — superseded by docs/adr/"
git push -u origin audit/phase-2-adrs
```
Open the phase-2 PR (ADRs + archive together — the ADR set is reviewable against the corpus it replaces).

---

## Phase 3 — Comment cleanup (`cube-idp`)

Batches, in order (delicate first, mechanical last):

| # | Scope | Sites (approx, from `$S/sites.jsonl`) |
| --- | --- | --- |
| B1 | `internal/diag/` (codes.go + registry.go + tests) | ~120 |
| B2 | `internal/config/` | ~60 |
| B3 | `internal/pack/` | ~50 |
| B4 | `internal/up/` + `internal/engine/` + `internal/bundle/` | ~80 |
| B5 | `internal/cluster/` + `internal/gitea/` + remaining `internal/` | ~90 |
| B6 | `cmd/` | ~120 |
| B7 | `tests/e2e/` | ~60 |
| B8 | `Makefile`, `.github/workflows/`, `docs/vhs/`, misc | ~10 |

### Task 12: **[WORKFLOW]** Classification fan-out (read-only)

**Interfaces:**
- Consumes: `$S/sites.jsonl` (grouped per batch by the orchestrator), archive corpus, GATE-1-approved registry rewrites.
- Produces: `$S/decisions-B<N>.json` per batch: array of `{file,line,outcome: rewrite|delete|keep, replacement, sourceCitation, note}`.

- [ ] **Step 1: Launch classification per batch**

```js
export const meta = {
  name: 'comment-classify',
  description: 'Classify every planning-referencing comment site: rewrite/delete/keep',
  phases: [{ title: 'Classify', detail: 'one read-only agent per batch chunk' }],
}
const DECISIONS = { type:'object', required:['decisions'], properties:{ decisions:{ type:'array', items:{
  type:'object', required:['file','line','outcome'],
  properties:{ file:{type:'string'}, line:{type:'integer'},
    outcome:{enum:['rewrite','delete','keep','escalate']},
    replacement:{type:'string'}, sourceCitation:{type:'string'}, note:{type:'string'} }}}}}
const out = await parallel(args.chunks.map(ch => () => agent(
`You classify Go comment sites that reference planning artifacts (Task N, P7, GT16,
Phase 2, WP8, checkpoint/decision numbers, docs/superpowers paths). Repo root: ${args.root}.
Sites (file:line + text): ${JSON.stringify(ch.sites)}.
For each site, read the surrounding code AND resolve the planning reference in the
archived corpus under docs/archive/superpowers/ (grep for the marker; plans name tasks
as headings). Decide:
- rewrite: the comment explains non-obvious behaviour → produce the full replacement
  comment line(s) with the planning ID replaced by plain prose DERIVED FROM THE CITED
  PASSAGE (quote its file:line in sourceCitation), or by an ADR reference docs/adr/NNNN
  when the rationale is long. Keep the comment's original indentation and prefix.
- delete: the comment only cites a plan, or restates the code.
- keep: the flagged token is NOT planning residue (e.g. product string, test fixture
  input, CUBE-#### context) — say why in note.
- escalate: passage unlocatable AND the comment seems to carry unique meaning.
HARD RULES: never alter CUBE-[0-9]{4} identifiers; in internal/diag/codes.go keep
'// reserved:' trailing markers verbatim; in internal/diag/registry.go, Summary strings
are USER-FACING — outcome must be 'rewrite' ONLY for sites pre-approved in
${args.approvedRegistryList}, else 'escalate'. Comments must not name plan files.
Return only structured decisions covering EVERY input site.`,
  { label:`classify:${ch.id}`, phase:'Classify', schema:DECISIONS })))
const all = out.filter(Boolean).flatMap(r => r.decisions)
log(`classified ${all.length} sites; escalations: ${all.filter(d=>d.outcome==='escalate').length}`)
return { decisions: all }
```

Chunks: ≤40 sites per agent within a batch. Run for all batches B1–B8 (B8 sites are shell/yaml — same rules, comment syntax `#`).

- [ ] **Step 2: Coverage check + escalation triage**

Run a plain-code check: every site in `$S/sites.jsonl` for the batch has exactly one decision. Missing → re-dispatch those sites. Present `escalate` rows to the operator (**mini-gate**, batched with GATE reviews — do not silently resolve).

### Task 13: Apply batch B1 (`internal/diag`) — the delicate one

**Files:**
- Modify: `internal/diag/codes.go`, `internal/diag/registry.go`, `internal/diag/*_test.go` (comments only, plus approved Summary strings)

- [ ] **Step 1: Branch and read the test harness first**

```bash
git checkout -b audit/phase-3-comments main   # after phase-2 PR merges
```
Read `internal/diag/codes_test.go` fully (`TestCatalogWellFormed`, `parseDefinedCodes`, `TestNoCubeLiteralsOutsideCatalog`, `TestCatalogExhaustive`) — these parse `codes.go` comment structure. Constraints extracted there override any classification decision that conflicts; conflicting sites revert to `escalate`.

- [ ] **Step 2: Apply decisions from `$S/decisions-B1.json`** — one edit per site, exactly the `replacement` text. Registry `Summary` strings: only GATE-1-approved rewrites, applied to **both** the `codes.go` trailing comment and the mirrored `registry.go` Summary in the same commit.

- [ ] **Step 3: Verify the batch**

```bash
go build ./... && go test ./internal/diag/ ./cmd/ -count=1
go run ./hack/truthindex -codes-only | diff - "$S/baseline-codes.txt"      # MUST be empty
python3 hack/docaudit/comment_only_diff.py --allow-file internal/diag/registry.go main
go run . explain CUBE-0007 | diff - "$S/baseline-explain-0007.txt"          # unchanged (no approved rewrite)
go run . explain CUBE-0003                                                  # changed ONLY per approved text
make truth-index && git add hack/truth-index.json                           # summaries live in the index — regen
hack/docaudit/inventory.sh . | grep -c 'internal/diag' || true              # expect 0
```
Expected: all clean; the diag site count drops to 0. Any codes-only diff → **revert the batch**, diagnose, reapply.

- [ ] **Step 4: Commit**

```bash
git add internal/diag/ hack/truth-index.json
git commit -m "chore(diag): rewrite planning-artifact comments as self-explanatory prose"
```

### Task 14: Apply batches B2–B7

For each batch in order, repeat the Task 13 protocol exactly, with these substitutions:

- [ ] **B2–B7 per batch:** apply `$S/decisions-B<N>.json` → verify:
```bash
go build ./... && go test ./<batch packages>/... -count=1
go run ./hack/truthindex -codes-only | diff - "$S/baseline-codes.txt"
python3 hack/docaudit/comment_only_diff.py <last-batch-commit>
hack/docaudit/inventory.sh . | grep -c '<batch path prefix>'   # expect 0
git commit -m "chore(<pkg>): rewrite planning-artifact comments as self-explanatory prose"
```
No `--allow-file` outside B1 — every other batch is comments-only, zero tolerance. After B7, run the full suite once: `go test ./... -count=1` and `make test-apply` if a cluster is available (record if skipped and why).

- [ ] **Step: B8 (Makefile/CI/vhs)** — comment syntax `#`; `comment_only_diff.py` does not cover non-Go files, so verify by eyeball diff + `git diff main --stat` showing only expected files, then `make build test`.

- [ ] **Step: Final phase-3 verification + PR**

```bash
hack/docaudit/inventory.sh . | tee "$S/sites-after.jsonl" | wc -l    # expect 0
git push -u origin audit/phase-3-comments
```
Open the phase-3 PR; the PR description lists per-batch site counts before/after and links `$S` artifacts pasted into the PR body (scratchpad is not durable).

---

## Phase 4 — Reference-doc fixes (`cube-idp`)

### Task 15: Apply approved findings to live docs

**Files:**
- Modify: `README.md`, `docs/pack-contract-v1.md`, `docs/machine-readable-output.md`, `docs/outstanding-todos.md`, `CHANGELOG.md`, `tests/e2e/PACKS.md` — exactly the GATE-1-approved resolutions, nothing else.

- [ ] **Step 1: Branch; apply per file, one commit per file**

```bash
git checkout -b audit/phase-4-docs main
```
Work from the annotated findings report only. Known fixed points (from pre-plan sampling, must appear among the edits): `README.md:85` — `cube-idp migrate` claim replaced with what actually exists (per the approved resolution; if none exists, state the apiVersion is frozen pre-1.0 without naming a nonexistent command) and `(D5)` marker removed or replaced by an ADR link; `docs/outstanding-todos.md` — the two dangling `cluster-forprovider` citations re-pointed to their ADR (if the underlying decision got one) or rewritten as self-contained descriptions with code file:line only.
CHANGELOG per AUD-9: strip planning IDs; factual corrections only; released entries otherwise verbatim.

- [ ] **Step 2: Re-verify every previously-failing claim**

**[WORKFLOW]** re-run the Task 6 audit workflow with clusters narrowed to the edited files only. Expected: `findings: 0 survived` (or only rows the operator explicitly chose to keep). Non-zero → fix and re-run.

- [ ] **Step 3: Link check + PR**

```bash
grep -rnoE '\]\(([^)#]+\.md)' README.md docs/ --include='*.md' | sed -E 's/.*\]\(//' | sort -u | while read -r p; do [ -f "$p" ] || echo "DANGLING: $p"; done   # expect no output
git push -u origin audit/phase-4-docs
```

---

## Phase 5 — Recurrence guard

### Task 16: `hack/check-docs.sh` + CI wiring

**Files:**
- Create: `hack/check-docs.sh`
- Modify: `.github/workflows/ci.yaml` (add `docs-guard` job)
- Modify: `.github/workflows/release.yaml` (upload `hack/truth-index.json` as a release asset)

- [ ] **Step 1: Write the guard**

```bash
#!/usr/bin/env bash
# hack/check-docs.sh — CI recurrence guard for the docs & comment audit.
# Modes: cube-idp repo (default) runs all checks; sibling repos set
# CHECK_DOCS_INDEX=path/to/truth-index.json (downloaded from a pinned
# cube-idp release) and skip the drift check.
# Inline waiver for a false-positive line:  <token>  [docs-guard: allow]
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"; fail=0

# 1. planning-pattern scan (CUBE-#### is product namespace, never flagged)
if hits="$("$here/docaudit/inventory.sh" . | grep -v 'docs-guard: allow')" && [ -n "$hits" ]; then
  echo "── planning-artifact references found:"; echo "$hits" | head -40; fail=1
fi

# 2. dangling relative markdown links
while IFS= read -r f; do
  dir="$(dirname "$f")"
  grep -noE '\]\(([^)#[:space:]]+\.md)[#)]' "$f" | sed -E 's/:\]\(/:/; s/[#)]$//' | \
  while IFS=: read -r ln p; do
    [[ "$p" == http* ]] && continue
    [ -f "$dir/$p" ] || [ -f "$p" ] || { echo "DANGLING $f:$ln → $p"; exit 9; }
  done || fail=1
done < <(git ls-files '*.md' | grep -vE '^docs/archive/|/node_modules/')

# 3. documented commands exist (backtick spans `cube-idp <sub>` vs oracle)
idx="${CHECK_DOCS_INDEX:-hack/truth-index.json}"
if [ -f "$idx" ]; then
  known="$(python3 -c "import json;print('\n'.join(c['path'] for c in json.load(open('$idx'))['commands']))")"
  while IFS=: read -r f ln span; do
    sub="$(printf '%s' "$span" | sed -E 's/^\`(cube-idp( [a-z][a-z-]+)*).*/\1/')"
    grep -qxF "$sub" <<<"$known" || { echo "PHANTOM COMMAND $f:$ln → $span"; fail=1; }
  done < <(git ls-files '*.md' | grep -vE '^docs/archive/' | \
           xargs grep -noE '\`cube-idp [a-z][a-z-]+[^\`]*\`' 2>/dev/null || true)
fi

# 4. truth-index drift (cube-idp repo only)
if [ -z "${CHECK_DOCS_INDEX:-}" ] && [ -d hack/truthindex ]; then
  go run ./hack/truthindex -check || fail=1
fi

exit $fail
```

- [ ] **Step 2: Red/green test the guard**

```bash
chmod +x hack/check-docs.sh
hack/check-docs.sh; echo "clean exit=$?"                         # expect 0 on the cleaned tree
echo '// leftover from Task 99' >> internal/diag/diag.go
hack/check-docs.sh; echo "seeded exit=$?"                        # expect 1, names the line
git checkout internal/diag/diag.go
```

- [ ] **Step 3: Wire CI**

`ci.yaml` — add job (match the existing jobs' Go setup steps verbatim):
```yaml
  docs-guard:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: hack/check-docs.sh
```
`release.yaml` — in the release job, after artifacts build: regenerate and attach the index (`make truth-index`, add `hack/truth-index.json` to the release assets per the existing goreleaser/upload mechanism — inspect `release.yaml` and `.goreleaser.yaml` and use whichever asset path exists; `extra_files` in goreleaser if present).

- [ ] **Step 4: Commit + PR**

```bash
git checkout -b audit/phase-5-guard main
git add hack/check-docs.sh .github/workflows/
git commit -m "ci: docs recurrence guard — patterns, links, phantom commands, index drift"
git push -u origin audit/phase-5-guard
```
Gate: the guard job is green on this PR.

---

## Phase 6 — Sibling repos

### Task 17: `packs` repo

- [ ] **Step 1:** Branch `audit/docs-cleanup`. Apply GATE-1-approved findings for the `packs` cluster (`CONTRACT.md` claims vs the truth index's `packContract` fields; pack READMEs; `publish.yml`/`conformance.yml` comments; `pack.cue` comments in `envoy-gateway`, `chart.yaml` in `backstage`, `10-install.yaml` in argocd — ~18 sites, same classify-then-apply protocol, single agent, no workflow needed at this volume).
- [ ] **Step 2:** Copy `hack/docaudit/{patterns.txt,allowlist-paths.txt,inventory.sh}` and `hack/check-docs.sh` into `hack/` (they are repo-agnostic). Wire a `docs-guard` job into `conformance.yml` with:
```yaml
      - name: fetch truth index
        run: |
          curl -fsSL -o /tmp/truth-index.json \
            https://github.com/cube-idp/cube-idp/releases/download/${TRUTH_INDEX_TAG}/truth-index.json
        env: { TRUTH_INDEX_TAG: <latest cube-idp tag carrying the asset> }
      - run: CHECK_DOCS_INDEX=/tmp/truth-index.json hack/check-docs.sh
```
**Ordering dependency:** this step requires one cube-idp release after phase 5 (the first tag carrying the asset). Until it exists, wire the job with the index steps commented out and a `# TODO(docs-guard): enable at first release ≥ <tag>` marker — the pattern/link checks still run. This is the single allowed TODO, removed in Task 20.
- [ ] **Step 3:** `hack/check-docs.sh` green locally → commit per concern (`docs:` / `ci:`) → PR.

### Task 18: `plugins` repo

- [ ] Same protocol as Task 17. Findings cluster `plugins` (~5 sites: `CONTRACT-PLUGINS.md`, `hack/genindex.sh`, `USE-CASES.md` claims vs plugin commands in the index). Guard wired into `ci.yml`. PR.

### Task 19: `cube-idp-web` repo

- [ ] Same protocol. Findings cluster `web`: astro pages' rendered claims (commands shown in `get-started.astro`, JSON examples in `docs/json.md` vs `outputSchemas`, diagnostic codes rendered on `index.astro`/`404.astro` must exist in `diagCodes`). Guard added as a job in `link-check.yaml` (it already runs on md/astro changes). No Go here: guard runs with `CHECK_DOCS_INDEX` fetched as in Task 17. PR.

---

## Phase 7 — Close-out

### Task 20: Definition of done + archive audit artifacts

- [ ] **Step 1: DoD sweep** — run in each of the four repos:

```bash
hack/check-docs.sh && echo GREEN            # all four repos
hack/docaudit/inventory.sh . | wc -l        # 0 in all four
```
Plus: cube-idp `go test ./... -count=1` green; every findings-report row has a `Decision:` annotation of `fixed`/`deferred(owner)`/`kept`; the suspected-bug list handed off (linked issue or explicit operator acknowledgment); Task 17's TODO removed (index fetch enabled after the release).

- [ ] **Step 2: Archive the audit artifacts**

```bash
git checkout -b audit/phase-7-closeout main
git mv docs/superpowers/specs/2026-07-19-docs-comment-audit-design.md docs/archive/superpowers/specs/
git mv docs/superpowers/plans/2026-07-20-docs-comment-audit.md docs/archive/superpowers/plans/
git mv docs/superpowers/research/2026-07-20-docs-audit-findings.md docs/archive/superpowers/research/
```
Promote to ADRs (via `adr-skill`, same protocol as Task 10) the two decisions the spec flags as binding beyond the project: the `CUBE-XXXX` allowlist rule (AUD-6) and the changelog policy (AUD-9). If `docs/superpowers/` is now empty, remove the empty dirs.

- [ ] **Step 3: Final commit + summary**

```bash
git add -A && git commit -m "docs: close out docs & comment audit — artifacts archived, DoD green"
git push -u origin audit/phase-7-closeout
```
Report to the operator: per-phase PR links, final counts (findings by bucket → resolution), the two promoted ADRs, and the suspected-bug hand-off location.

---

## Self-review notes (spec → plan coverage)

- Spec §5.A Truth Index → Tasks 1–2 (extractor, determinism, drift check, Makefile). Pack-contract row implemented as reflection over the loader's `pack.Pack` struct — the loader *is* the enforcement; `pack.cue` CUE evaluation happens implicitly through what the loader accepts.
- §5.B audit + buckets + registry-summary amendment → Tasks 6–7; report format and location per spec.
- §5.C harvest/validate/ADR/archive → Tasks 8–11 (conservative default: unverifiable ≠ ADR; GATE 2).
- §5.D cleanup + AUD-8 discipline + codes.go load-bearing comments + explain-gate amendment → Tasks 12–14.
- §5.E guard incl. report-only-first (inventory.sh existed from Task 3; enforcement flips in Task 16), shared pattern list, index drift, honest-limit → Tasks 3, 16.
- §6 phases 1–7 → Tasks 1–7 / 8–11 / 12–14 / 15 / 16 / 17–19 / 20. Gates: GATE 1 (findings), GATE 2 (decisions), GATE 3 (ADR set), per-batch mechanical gates, DoD.
- §8 multi-agent opt-in → Workflow scripts in Tasks 6, 8, 9, 12; adversarial verify in Task 6; parallel drafting in Task 10.
- Known deviation, deliberate: sibling-repo guard depends on a cube-idp release carrying the asset; interim state is explicit and removed at close-out (Task 17/20).
