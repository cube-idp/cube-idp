# valuesRef / tuningRef + remote `-f` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remote-fetchable pack values (`packs[].valuesRef`) and cube config (`-f <ref>`), both riding the existing pack ref grammar with pins recorded in `cube.lock`. (`engine.tuningRef` was DROPPED — see Amendment 4; the engine-values successor is a post-p7 plan.)

**Architecture:** Extend `pack.FetchFile` to surface the pin it already computes; add a shared `internal/refval` resolver (fetch one YAML → map + pin) that `providerConfigRef`, `valuesRef`, and remote `-f` all consume; merge inline-over-fetched via RFC 7386 (`compose.Merge` precedent); resolve at desired-state build time (never `config.Load`), record pins in `cube.lock`, and attribute changes in `upgrade --plan`.

**Tech Stack:** Go 1.26, CUE (config schema), `github.com/evanphx/json-patch/v5` (RFC 7386), forked `hashicorp/go-getter`, ORAS, `sigs.k8s.io/yaml`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md`

## Global Constraints

- **CLI freeze (F1):** no new/renamed cobra flags, no changed defaults or `Short` texts. `go test ./cmd/ -run TestCommandTreeGolden` must pass WITHOUT `-update` at every commit.
- **Diag codes:** every new code needs a constant in `internal/diag/codes.go` AND a `Desc` entry in `internal/diag/registry.go` (`TestRegistryCoversEveryDeclaredCode` enforces both directions). New codes (POST-p7 ACTUALS — see Amendment 5): `CUBE-4021` (T5), `CUBE-7007` (T6), `CUBE-0014` = `CodeConfigRemoteReadOnly` + `CUBE-0015` = `CodeConfigRemoteFetch` (T8). `CUBE-3012` is NOT allocated (T7 GATED_SKIP). Errors are built with `diag.New(code, summary, fix)` / `diag.Wrap(err, code, summary, fix)`.
- **omitempty round-trip discipline:** every new optional config/lock field carries `yaml:"…,omitempty" json:"…,omitempty"` — a nil/empty value must marshal as an ABSENT key, never an explicit `null`/`""` (CUE re-validation rejects explicit nulls; `schema.cue` optional strings are `!=""`).
- **GT15 (values stone):** `values:`/`valuesRef:` are Helm values only — chartless pack + either = `CUBE-4016` at render time. Engine tuning is NOT values.
- **Import direction:** `pack` imports `config`; `config` must NOT import `pack` (that is why remote `-f` dispatch lives in a new `internal/cfgload` package, a deliberate deviation from spec §7.1's "inside config.Load" — same observable contract).
- **Spec deviation ledger (agreed at planning):** (1) remote-`-f` dispatch in `internal/cfgload`, not `config.Load` (import cycle); (2) a local *file* ref pins as `file:<sha256-hex>` (spec table only covered directories); (3) the remote-config info line prints from `up`/`diff` (which own UI sinks), not from every command.
- Commits end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and use explicit pathspecs (`git commit -- <paths>`), never bare `git commit -a`.
- Verify with real `go build ./...` / `go test`, not editor diagnostics (stale-LSP gotcha from p6).

## Amendments — 2026-07-19 capability assessment

Applied after reviewing the existing config surface (dependsOn, extraManifests,
providerConfigRef/forProvider, delivery, spokes, bundle mode) against this plan:

1. **Task 7 is GATED** on the engine-as-pack decision
   (`docs/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md`,
   PROPOSED): that spec deletes `engine.tuning` in favor of `engine.ref` +
   open `engine.values`. Do NOT execute Task 7 until the owner rules.
   Execution order: 1-6, 8-12, then 7 (or its replacement). Details in the
   Task 7 banner; Task 11's `tuning(…)` attribution block moves with it.
2. **Bundle-mode rails guard** (`CUBE-7007`): `up --bundle` promises no
   network, but remote values/config sources are not vendored. Task 6 adds
   a `bundleRailsCheck` (valuesRef clause), Task 10 extends it (remote `-f`
   origin clause), Task 7 extends it (tuningRef clause, gated with the task).
3. **Recorded as out of scope** (no task): gateway pack takes no
   `valuesRef` (it takes no `values` today either — the CUBE-4020 "gateway
   is special" precedent); spoke `cluster.providerConfigRef` pins are NOT
   recorded in `cube.lock` (hub cluster only, Task 11); no
   `extraManifestsRef` (future symmetry candidate).

## Amendments — 2026-07-19 p7 merge (owner decision, post-PR#3)

Applied after PR #3 (`p7/engine-as-pack`) merged to main and was merged into
this branch (`0fe2f74`). These are OWNER DECISIONS and are NORMATIVE — they
override the task bodies below wherever they conflict.

- **Amendment 4 — `engine.tuningRef` is DROPPED from Task 4; do NOT add it.** Task 4
   ships `packs[].valuesRef` ONLY. Rationale: (a) `engine.tuning` no longer
   exists — p7 deleted `internal/engine/tune.go` and replaced the whole
   block with `engine.ref` + open `engine.values`, so Task 4's stated CUE
   anchor ("after the `tuning?` closing brace") is GONE; (b) `TuningRef`'s
   only consumers were Task 7 (GATED_SKIP) and Task 11's `tuning(…)` block
   (SKIPPED), so the field would be user-settable, CUE-valid, and silently
   ignored by every code path — and `CUBE-7007`'s `tuningRef` clause is
   gated with Task 7, so `up --bundle` would not even flag it as an
   un-vendored remote source, quietly breaking the offline-rails promise;
   (c) it would be born deprecated into a block a ratified spec just
   removed. Concretely, Task 4 MUST NOT touch `EngineSpec`, MUST NOT add
   `tuningRef?` to `schema.cue`, and its test asserts `valuesRef` only. The
   remote-engine-values successor (`engine.valuesRef`, riding p7's open
   `engine.values`) is a post-p7 plan, NOT this one.
- **Amendment 5 — p7 is merged; diag numbers are now FIXED ACTUALS.** p7 took
   `CUBE-0012` (`CodeEngineTuningRemoved`) and `CUBE-0013`
   (`CodeEnginePackMismatch`), so the RENUMBER RULE has fired: Task 8
   allocates **`CUBE-0014` = `CodeConfigRemoteReadOnly`** and
   **`CUBE-0015` = `CodeConfigRemoteFetch`**. T9/T10/T12 consume those two
   numbers verbatim and never re-derive them. `CUBE-4021` (T5) and
   `CUBE-7007` (T6) were re-verified FREE and keep their planned numbers.
   Every remaining task MUST re-verify its line anchors against the real
   post-merge code before editing: p7 moved `internal/up/up.go`,
   `internal/diff/diff.go`, `internal/lock/lock.go`,
   `internal/config/types.go`, `internal/config/load.go`, `internal/config/schema.cue`,
   `internal/pack/helm.go`, and `internal/engine/*` (the `engine.Engine`
   interface lost `Install`/`InstallManifests` — engines are pure
   translators now). Anchor drift is expected: use the escape hatch
   (verify against real code, minimal correction, FINDINGS entry), not a
   BLOCKED status.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/pack/fetchfile.go` (modify) | `FetchFile` returns `(bytes, pin, error)` |
| `internal/pack/getter.go` (modify) | export `IsRemoteRef` |
| `internal/pack/values.go` (create) | `EffectiveValues` + `RenderResolved` (guard → fetch → merge → render) |
| `internal/refval/refval.go` (create) | `Resolve` (YAML→map+pin), `Merge` (RFC 7386), `NormalizeIntegral` |
| `internal/cluster/compose/compose.go` (modify) | delegate to `refval`, surface pin |
| `internal/config/types.go` (modify) | `PackRef.ValuesRef`, `Cube` origin (NO `EngineSpec.TuningRef` — Amendment 4) |
| `internal/config/schema.cue` (modify) | `valuesRef?` only (NO `tuningRef?` — Amendment 4) |
| `internal/config/load.go` (modify) | `LoadBytes` split, `SaveValidated` remote guard |
| `internal/cfgload/cfgload.go` (create) | local-vs-remote `-f` dispatch |
| ~~`internal/engine/factory/factory.go`~~ | ~~`NewResolved` (tuningRef resolve+merge)~~ — NOT TOUCHED (Task 7 GATED_SKIP, Amendments 1+4) |
| `internal/lock/lock.go` (modify) | `ValuesRef/ValuesPin`, `Cluster` section (NO `TuningRef/TuningPin` — Amendment 4) |
| `internal/up/up.go` (modify) | wire `RenderResolved`, `NewResolved`, lock fields, lock CWD path |
| `internal/diff/diff.go` (modify) | same wiring for `desiredState` |
| `internal/upgrade/plan.go` (modify) | values/providerConfig change attribution (tuning block SKIPPED — Amendments 1+4) |
| `internal/diag/codes.go` + `registry.go` (modify) | 4 new codes |
| `cmd/*.go` (modify) | `config.Load(file)` → `cfgload.Load(cmd.Context(), file)` |

---

### Task 1: `pack.FetchFile` returns the pin

**Files:**
- Modify: `internal/pack/fetchfile.go`
- Modify: `internal/cluster/compose/compose.go:29` (compile fix only — real migration is Task 3)
- Test: `internal/pack/fetchfile_pin_test.go` (create)

**Interfaces:**
- Produces: `func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, string, error)` — pin forms: `oci:<digest>`, `git+<sha>`, `dir:<dirhash-h1>`, `file:<sha256-hex>` (local single file).

- [x] **Step 1: Write the failing test**

```go
// internal/pack/fetchfile_pin_test.go
package pack

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// Local refs are the network-free slice of FetchFile's grammar; oci/git pin
// plumbing is exercised by the existing fetch tests' fixtures once the
// signature carries it through (same helpers, same seams).
func TestFetchFilePinLocalFile(t *testing.T) {
    dir := t.TempDir()
    f := filepath.Join(dir, "values.yaml")
    content := []byte("replicas: 3\n")
    if err := os.WriteFile(f, content, 0o644); err != nil {
        t.Fatal(err)
    }
    b, pin, err := FetchFile(context.Background(), f, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if string(b) != "replicas: 3\n" {
        t.Fatalf("bytes = %q", b)
    }
    sum := sha256.Sum256(content)
    want := "file:" + hex.EncodeToString(sum[:])
    if pin != want {
        t.Fatalf("pin = %q, want %q", pin, want)
    }
}

func TestFetchFilePinLocalDir(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("a: 1\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    _, pin, err := FetchFile(context.Background(), dir, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if !strings.HasPrefix(pin, "dir:") {
        t.Fatalf("pin = %q, want dir:<dirhash>", pin)
    }
    // Same content → same pin (dirhash is deterministic).
    _, pin2, err := FetchFile(context.Background(), dir, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if pin2 != pin {
        t.Fatalf("pin not deterministic: %q vs %q", pin2, pin)
    }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pack/ -run 'TestFetchFilePin' -v`
Expected: FAIL — compile error `assignment mismatch: 3 variables but pack.FetchFile returns 2 values`

- [x] **Step 3: Widen `FetchFile` to return the pin**

In `internal/pack/fetchfile.go`, change the signature and every branch (`pullOCI` and `fetchGitTree` already return the pin components; getter/local-dir use the existing `dirPin`; local file hashes its bytes). Full replacement for the function body:

```go
import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/cube-idp/cube-idp/internal/diag"
)

// FetchFile resolves ref — the same grammar Fetch accepts (local path,
// oci://host/repo:tag, <host>/<org>/<repo>[//subdir]@rev, git::/s3::/http(s)
// getter forms) — to the bytes of exactly ONE YAML file plus the cube.lock
// pin of what was fetched (oci:<digest> / git+<sha> / dir:<dirhash>;
// file:<sha256-hex> for a direct local file). It is the fetch primitive for
// spec.cluster.providerConfigRef, packs[].valuesRef, engine.tuningRef and
// remote -f: unlike Fetch it never parses pack.cue, and a ref that yields a
// directory must contain exactly one top-level *.yaml/*.yml or the fetch
// fails (a config/values document is one file, not a tree).
func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, string, error) {
    switch {
    case strings.HasPrefix(ref, "oci://"):
        dir, digest, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
        if err != nil {
            return nil, "", err
        }
        b, err := singleYAML(ref, dir)
        return b, "oci:" + digest, err
    case isGitRef(ref):
        dir, pin, err := fetchGitTree(ctx, ref, cacheDir)
        if err != nil {
            return nil, "", err
        }
        b, err := singleYAML(ref, dir)
        return b, pin, err
    case isGetterRef(ref):
        dst := filepath.Join(cacheDir, "getter", sanitizeRef(ref))
        if err := fetchGetter(ctx, ref, dst); err != nil {
            return nil, "", err
        }
        pin, err := dirPin(dst)
        if err != nil {
            return nil, "", err
        }
        b, err := singleYAML(ref, dst)
        return b, pin, err
    case strings.Contains(ref, "://"):
        return nil, "", diag.New(diag.CodePackRefInvalid, fmt.Sprintf("unsupported ref scheme in %q", ref),
            "use a local path, oci://host/repo:tag, github.com/org/repo//path@rev, or an explicit go-getter URL (git::…, s3::…, https://…)")
    default:
        abs, err := filepath.Abs(ref)
        if err != nil {
            return nil, "", diag.Wrap(err, diag.CodePackRefInvalid, "bad ref path", "use a valid file or directory path")
        }
        info, err := os.Stat(abs)
        if err != nil {
            return nil, "", diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
                "point the ref at a readable YAML file or a directory containing exactly one")
        }
        if info.IsDir() {
            pin, err := dirPin(abs)
            if err != nil {
                return nil, "", err
            }
            b, err := singleYAML(ref, abs)
            return b, pin, err
        }
        b, err := os.ReadFile(abs)
        if err != nil {
            return nil, "", diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
                "check file permissions")
        }
        sum := sha256.Sum256(b)
        return b, "file:" + hex.EncodeToString(sum[:]), nil
    }
}
```

(`singleYAML` unchanged. The pin is returned even when `singleYAML` errors on the oci/git/getter branches — callers must check err first, standard Go.)

Compile-fix the one production call site, `internal/cluster/compose/compose.go:29`:

```go
    raw, _, err := pack.FetchFile(ctx, ref, cacheDir)
```

Also grep for test call sites and fix the same way: `grep -rn "FetchFile(" --include="*_test.go" internal/`.

- [x] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/pack/ ./internal/cluster/... -count=1`
Expected: PASS (all — including pre-existing fetchfile/compose tests)

- [x] **Step 5: Commit**

```bash
git add internal/pack/fetchfile.go internal/pack/fetchfile_pin_test.go internal/cluster/compose/compose.go
git commit -m "feat(pack): FetchFile returns the reproducibility pin (RV1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/pack/fetchfile.go internal/pack/fetchfile_pin_test.go internal/cluster/compose/compose.go
```

---

### Task 2: `internal/refval` — shared single-YAML resolver

**Files:**
- Create: `internal/refval/refval.go`
- Test: `internal/refval/refval_test.go` (create)

**Interfaces:**
- Consumes: `pack.FetchFile(ctx, ref, cacheDir) ([]byte, string, error)` (Task 1)
- Produces:
  - `func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error)` — `("", …)` → `(empty non-nil map, "", nil)`; non-mapping YAML → error; errors pass through the pack layer's diag codes for callers to wrap.
  - `func Merge(base, patch map[string]any) (map[string]any, error)` — RFC 7386, inputs untouched, never returns nil map.
  - `func NormalizeIntegral(v any) any` — post-JSON-round-trip repair for Helm consumers: `float64` with an integral value in int range → `int`; recurses maps/slices.

- [x] **Step 1: Write the failing test**

```go
// internal/refval/refval_test.go
package refval

import (
    "context"
    "os"
    "path/filepath"
    "reflect"
    "testing"
)

func write(t *testing.T, name, content string) string {
    t.Helper()
    f := filepath.Join(t.TempDir(), name)
    if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
    return f
}

func TestResolveEmptyRef(t *testing.T) {
    m, pin, err := Resolve(context.Background(), "", t.TempDir())
    if err != nil || pin != "" || m == nil || len(m) != 0 {
        t.Fatalf("got m=%v pin=%q err=%v; want empty non-nil map, no pin", m, pin, err)
    }
}

func TestResolveLocalFile(t *testing.T) {
    f := write(t, "v.yaml", "a:\n  b: 2\n")
    m, pin, err := Resolve(context.Background(), f, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if pin == "" {
        t.Fatal("expected a pin")
    }
    if m["a"].(map[string]any)["b"] != float64(2) { // sigs.k8s.io/yaml JSON typing
        t.Fatalf("m = %#v", m)
    }
}

func TestResolveRejectsNonMapping(t *testing.T) {
    f := write(t, "v.yaml", "- just\n- a\n- list\n")
    if _, _, err := Resolve(context.Background(), f, t.TempDir()); err == nil {
        t.Fatal("expected error for non-mapping document")
    }
}

func TestMergeNullDeletesAndArraysReplace(t *testing.T) {
    base := map[string]any{"keep": 1.0, "drop": 1.0, "arr": []any{1.0, 2.0}, "nest": map[string]any{"x": 1.0}}
    patch := map[string]any{"drop": nil, "arr": []any{9.0}, "nest": map[string]any{"y": 2.0}}
    got, err := Merge(base, patch)
    if err != nil {
        t.Fatal(err)
    }
    want := map[string]any{"keep": 1.0, "arr": []any{9.0}, "nest": map[string]any{"x": 1.0, "y": 2.0}}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %#v want %#v", got, want)
    }
    if _, still := base["drop"]; !still {
        t.Fatal("Merge mutated its base input")
    }
}

func TestNormalizeIntegral(t *testing.T) {
    in := map[string]any{"r": float64(3), "f": 3.5, "deep": []any{float64(7)}}
    got := NormalizeIntegral(in).(map[string]any)
    if got["r"] != 3 || got["f"] != 3.5 || got["deep"].([]any)[0] != 7 {
        t.Fatalf("got %#v", got)
    }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/refval/ -v`
Expected: FAIL — package does not exist / undefined symbols

- [x] **Step 3: Implement `refval`**

```go
// internal/refval/refval.go
//
// Package refval resolves a "one YAML document" ref — the pack ref grammar
// (local path, oci://, bare git@rev, git::/s3::/http(s) getter forms) — to
// a JSON-typed map plus its reproducibility pin. It is the shared resolver
// behind cluster.providerConfigRef (compose), packs[].valuesRef, and
// remote -f (spec 2026-07-19 §4). Errors pass through
// the pack layer's diag codes untouched; each consumer wraps with its own
// domain code (CUBE-1005 / CUBE-4021 / CUBE-0015).
package refval

import (
    "context"
    "encoding/json"
    "fmt"
    "math"

    jsonpatch "github.com/evanphx/json-patch/v5"
    sigyaml "sigs.k8s.io/yaml"

    "github.com/cube-idp/cube-idp/internal/pack"
)

// Resolve fetches ref and decodes it to a JSON-typed map. An empty ref
// resolves to an empty, non-nil map and no pin so callers need no special
// case. A document that is valid YAML but not a mapping is an error —
// every consumer (values, tuning, provider config, cube.yaml) is
// object-shaped.
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error) {
    if ref == "" {
        return map[string]any{}, "", nil
    }
    raw, pin, err := pack.FetchFile(ctx, ref, cacheDir)
    if err != nil {
        return nil, "", err
    }
    j, err := sigyaml.YAMLToJSON(raw)
    if err != nil {
        return nil, "", fmt.Errorf("ref %q is not valid YAML: %w", ref, err)
    }
    var m map[string]any
    if err := json.Unmarshal(j, &m); err != nil {
        return nil, "", fmt.Errorf("ref %q is not a YAML mapping document: %w", ref, err)
    }
    if m == nil { // empty file decodes to JSON null
        m = map[string]any{}
    }
    return m, pin, nil
}

// Merge applies patch onto base per RFC 7386: maps deep-merge, lists
// replace wholesale, null deletes. Inputs stay untouched. (Lifted verbatim
// from compose.Merge, which now delegates here — one merge algorithm for
// every inline-over-fetched ladder.)
func Merge(base, patch map[string]any) (map[string]any, error) {
    bj, err := json.Marshal(base)
    if err != nil {
        return nil, err
    }
    pj, err := json.Marshal(patch)
    if err != nil {
        return nil, err
    }
    mj, err := jsonpatch.MergePatch(bj, pj)
    if err != nil {
        return nil, err
    }
    var m map[string]any
    if err := json.Unmarshal(mj, &m); err != nil {
        return nil, err
    }
    if m == nil {
        m = map[string]any{}
    }
    return m, nil
}

// NormalizeIntegral rewrites float64 leaves that hold integral values back
// to int, recursively. JSON round-trips (Resolve, Merge) type every number
// float64; Helm values want plain ints (the same reason config.Load
// normalizes CUE's int64 — see normalizePackValues). NOT for engine tuning:
// unstructured SSA forbids plain int, so tuning keeps JSON typing.
func NormalizeIntegral(v any) any {
    switch t := v.(type) {
    case float64:
        if t == math.Trunc(t) && t >= math.MinInt32 && t <= math.MaxInt32 {
            return int(t)
        }
        return t
    case map[string]any:
        for k, vv := range t {
            t[k] = NormalizeIntegral(vv)
        }
        return t
    case []any:
        for i, vv := range t {
            t[i] = NormalizeIntegral(vv)
        }
        return t
    default:
        return v
    }
}
```

Note the test asserts `Merge` output pre-normalization (floats) — normalization is a separate, explicit step for the Helm path only.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/refval/ -v -count=1`
Expected: PASS (5 tests)

- [x] **Step 5: Commit**

```bash
git add internal/refval/
git commit -m "feat(refval): shared one-YAML ref resolver — Resolve/Merge/NormalizeIntegral (RV1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/refval/
```

---

### Task 3: migrate `compose` onto `refval`, surface the providerConfig pin

**Files:**
- Modify: `internal/cluster/compose/compose.go`
- Modify: `internal/cluster/kindp/merge.go:69`, `internal/cluster/k3dp/merge.go:66` (call sites)
- Test: `internal/cluster/compose/compose_test.go` (extend existing)

**Interfaces:**
- Consumes: `refval.Resolve`, `refval.Merge` (Task 2)
- Produces: `compose.Resolve(ctx, ref, cacheDir) (map[string]any, string, error)` and `compose.Compose(ctx, ref, forProvider map[string]any, cacheDir string) (map[string]any, string, error)` — second return is the pin (`""` when ref is empty). `compose.Merge` stays exported, delegating to `refval.Merge`.

- [ ] **Step 1: Write the failing test** (append to the existing compose test file)

```go
func TestResolveReturnsPin(t *testing.T) {
    f := filepath.Join(t.TempDir(), "base.yaml")
    if err := os.WriteFile(f, []byte("kind: Cluster\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    m, pin, err := Resolve(context.Background(), f, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if m["kind"] != "Cluster" || pin == "" {
        t.Fatalf("m=%v pin=%q", m, pin)
    }
    // empty ref: no pin, empty non-nil map (existing contract preserved)
    m, pin, err = Resolve(context.Background(), "", t.TempDir())
    if err != nil || pin != "" || len(m) != 0 || m == nil {
        t.Fatalf("empty ref: m=%v pin=%q err=%v", m, pin, err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/compose/ -run TestResolveReturnsPin -v`
Expected: FAIL — compile error (2-value return)

- [ ] **Step 3: Rewrite `Resolve`/`Compose` as `refval` wrappers**

```go
// Resolve fetches ref (pack ref grammar, one YAML file — refval.Resolve)
// and decodes it to a JSON-typed map plus its pin. An empty ref resolves to
// an empty, non-nil map so Merge and the provider decode need no special
// case. Every failure wraps as CUBE-1005 with the pack-layer cause preserved.
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error) {
    m, pin, err := refval.Resolve(ctx, ref, cacheDir)
    if err != nil {
        return nil, "", diag.Wrap(err, diag.CodeProviderConfigRefFetch,
            fmt.Sprintf("cannot fetch providerConfigRef %q", ref),
            "the ref must resolve to one readable YAML mapping document; inspect with `cube-idp config render-cluster`")
    }
    return m, pin, nil
}

// Merge applies patch onto base per RFC 7386 (decision 4). One algorithm
// for every inline-over-fetched ladder — the implementation lives in refval.
func Merge(base, patch map[string]any) (map[string]any, error) {
    return refval.Merge(base, patch)
}

// Compose is Resolve + Merge: the full generic half of the ladder, plus
// the pin `up` records in cube.lock's cluster section (spec 2026-07-19 §6).
func Compose(ctx context.Context, ref string, forProvider map[string]any, cacheDir string) (map[string]any, string, error) {
    base, pin, err := Resolve(ctx, ref, cacheDir)
    if err != nil {
        return nil, "", err
    }
    if len(forProvider) == 0 {
        return base, pin, nil
    }
    m, err := Merge(base, forProvider)
    return m, pin, err
}
```

Drop the now-unused `encoding/json`/`jsonpatch`/`sigyaml`/`pack` imports; add `refval`. Update the two call sites (`kindp/merge.go:69`, `k3dp/merge.go:66`) to discard the pin: `merged, _, err := compose.Compose(…)`. Fix compose/kindp/k3dp test call sites the same way (`grep -rn "compose.Compose\|compose.Resolve" --include="*.go" internal/`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cluster/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/
git commit -m "refactor(compose): delegate to refval, surface providerConfig pin (RV1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/cluster/
```

---

### Task 4: config surface — `valuesRef` field

> **AMENDMENT 4 — NORMATIVE, overrides anything below:** `engine.tuningRef`
> is DROPPED. This task adds `packs[].valuesRef` ONLY. Do NOT touch
> `EngineSpec`; do NOT add `tuningRef?` to `schema.cue`. `engine.tuning` no
> longer exists (p7 deleted it in favour of `engine.ref` + open
> `engine.values`), and `TuningRef`'s only consumers were the skipped Task 7
> and Task 11's skipped tuning block.

**Files:**
- Modify: `internal/config/types.go` (`PackRef` — p7 moved this file; re-verify the anchor, do not trust the old line number)
- Modify: `internal/config/schema.cue` (the `packs?` entry — p7 moved this file; re-verify the anchor)
- Test: `internal/config/load_test.go` (extend — follow the file's existing table/temp-file conventions)

**Interfaces:**
- Produces: `config.PackRef.ValuesRef string` — `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"`.

- [ ] **Step 1: Write the failing test**

```go
func TestLoadValuesRef(t *testing.T) {
    f := filepath.Join(t.TempDir(), "cube.yaml")
    doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: demo}
spec:
  cluster: {provider: kind}
  engine:
    type: flux
  gateway: {}
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/traefik:0.2.0
      valuesRef: github.com/acme/values//traefik@v1.0.0
      values: {replicas: 3}
`
    if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
        t.Fatal(err)
    }
    c, err := Load(f)
    if err != nil {
        t.Fatal(err)
    }
    if got := c.Spec.Packs[0].ValuesRef; got != "github.com/acme/values//traefik@v1.0.0" {
        t.Fatalf("ValuesRef = %q", got)
    }
    // Round-trip: an absent ref must not serialize (omitempty discipline).
    c.Spec.Packs[0].ValuesRef = ""
    if err := SaveValidated(f, c); err != nil {
        t.Fatal(err)
    }
    raw, _ := os.ReadFile(f)
    if strings.Contains(string(raw), "valuesRef") {
        t.Fatalf("empty ref serialized: %s", raw)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadValuesRef -v`
Expected: FAIL — `c.Spec.Packs[0].ValuesRef undefined` (compile), then after types-only fix, CUE rejection `valuesRef: field not allowed`

- [ ] **Step 3: Add the fields**

`internal/config/types.go` — inside `PackRef`, after `Ref`:

```go
    // ValuesRef optionally fetches a BASE values document via the pack ref
    // grammar (one YAML mapping — pack.FetchFile); inline Values are RFC
    // 7386 merge-patched on top (null deletes, arrays replace) before the
    // chart-defaults merge (spec 2026-07-19 §5.1). Same GT15 stone as
    // Values: set on a chartless pack it is CUBE-4016 at render time. The
    // resolved pin is recorded in cube.lock (valuesPin).
    ValuesRef string `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"`
```

`internal/config/schema.cue` — the `packs?` entry (verified present post-p7 at
line ~41; re-check before editing), add `valuesRef?: string & !=""` after `ref`:

```cue
        packs?: [...{ref: string & !="", valuesRef?: string & !="", values?: {...}, extraManifests?: string & !="", delivery?: "oci" | "repo", dependsOn?: [...string & !=""]}]
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/types.go internal/config/schema.cue internal/config/load_test.go
git commit -m "feat(config): packs[].valuesRef field (RV2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/config/types.go internal/config/schema.cue internal/config/load_test.go
```

---

### Task 5: `pack.EffectiveValues` + `pack.RenderResolved` + `CUBE-4021`

**Files:**
- Create: `internal/pack/values.go`
- Modify: `internal/diag/codes.go` (4xxx block, after `CodePackDepGateway`), `internal/diag/registry.go` (matching entry)
- Test: `internal/pack/values_test.go` (create)

**Interfaces:**
- Consumes: `refval.Resolve/Merge/NormalizeIntegral` (Task 2), `(*Pack).HasChart()`, `(*Pack).RenderWith(values, extraManifests, gw)` (existing).
- Produces:
  - `func EffectiveValues(ctx context.Context, valuesRef string, inline map[string]any, cacheDir string) (map[string]any, string, error)` — no ref → `(inline, "", nil)` untouched.
  - `func RenderResolved(ctx context.Context, pk *Pack, pref config.PackRef, gw config.GatewaySpec, cacheDir string) (*Rendered, string, error)` — the single render entry `up` AND `diff` share (Task 6); returns the values pin.
  - `diag.CodePackValuesRefFetch Code = "CUBE-4021"`

- [ ] **Step 1: Add the diag code (registry test would fail otherwise the moment the constant exists — do both together)**

`internal/diag/codes.go`, inside the 4xxx const block after `CodePackDepGateway`:

```go
    // Remote values (spec 2026-07-19 §5.1, §8).
    CodePackValuesRefFetch Code = "CUBE-4021" // packs[].valuesRef fetch failed, not a YAML mapping, or merge with inline values failed
```

`internal/diag/registry.go`, 4xxx section:

```go
    CodePackValuesRefFetch: {Summary: "packs[].valuesRef fetch failed, not a YAML mapping, or merge with inline values failed"},
```

Run: `go test ./internal/diag/ -count=1` — Expected: PASS (registry coverage holds)

- [ ] **Step 2: Write the failing test**

```go
// internal/pack/values_test.go
package pack

import (
    "context"
    "os"
    "path/filepath"
    "reflect"
    "testing"

    "github.com/cube-idp/cube-idp/internal/config"
    "github.com/cube-idp/cube-idp/internal/diag"
)

func TestEffectiveValuesNoRefPassesInlineThrough(t *testing.T) {
    inline := map[string]any{"replicas": 3}
    got, pin, err := EffectiveValues(context.Background(), "", inline, t.TempDir())
    if err != nil || pin != "" {
        t.Fatalf("pin=%q err=%v", pin, err)
    }
    if !reflect.DeepEqual(got, inline) {
        t.Fatalf("got %#v", got)
    }
}

func TestEffectiveValuesMergesInlineOverFetched(t *testing.T) {
    f := filepath.Join(t.TempDir(), "base.yaml")
    os.WriteFile(f, []byte("replicas: 1\nimage:\n  tag: v1\nextra: {a: 1}\n"), 0o644)
    inline := map[string]any{"replicas": 3, "extra": nil} // override + RFC7386 delete
    got, pin, err := EffectiveValues(context.Background(), f, inline, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if pin == "" {
        t.Fatal("expected values pin")
    }
    want := map[string]any{"replicas": 3, "image": map[string]any{"tag": "v1"}}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %#v want %#v", got, want) // ints, not float64 (NormalizeIntegral)
    }
}

func TestEffectiveValuesWrapsFetchFailure(t *testing.T) {
    _, _, err := EffectiveValues(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"), nil, t.TempDir())
    if err == nil || !diag.HasCode(err, diag.CodePackValuesRefFetch) {
        t.Fatalf("err = %v, want CUBE-4021", err)
    }
}

// RenderResolved: valuesRef on a chartless pack is the GT15 stone, checked
// BEFORE any network fetch (chartlessness is known once the pack is local).
func TestRenderResolvedChartlessValuesRef(t *testing.T) {
    dir := t.TempDir() // a pack with manifests/ but no chart.yaml
    os.MkdirAll(filepath.Join(dir, "manifests"), 0o755)
    os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(`name: "plain"`+"\n"+`version: "0.1.0"`+"\n"), 0o644)
    os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
        []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: cm}\n"), 0o644)
    pk, err := Fetch(context.Background(), dir, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    pref := config.PackRef{Ref: dir, ValuesRef: "github.com/acme/values//x@v1"}
    _, _, err = RenderResolved(context.Background(), pk, pref, config.GatewaySpec{}, t.TempDir())
    if err == nil || !diag.HasCode(err, diag.CodePackValuesChartless) {
        t.Fatalf("err = %v, want CUBE-4016", err)
    }
}
```

(If `diag.HasCode` doesn't exist, check `internal/diag/diag.go` for the code-inspection helper the existing tests use — e.g. `diag.CodeOf(err) == …` — and use that instead; do NOT invent a new helper. Same for the `pack.cue` fixture shape: copy the minimal fixture pattern from an existing test in `internal/pack/` — e.g. `discovery_test.go` — rather than the sketch above if it differs.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/pack/ -run 'TestEffectiveValues|TestRenderResolved' -v`
Expected: FAIL — `undefined: EffectiveValues`, `undefined: RenderResolved`

- [ ] **Step 4: Implement `internal/pack/values.go`**

```go
// Package-file for the valuesRef half of GT15: fetching a remote base
// values document and merging inline values over it (spec 2026-07-19 §5.1).
package pack

import (
    "context"
    "fmt"

    "github.com/cube-idp/cube-idp/internal/config"
    "github.com/cube-idp/cube-idp/internal/diag"
    "github.com/cube-idp/cube-idp/internal/refval"
)

// EffectiveValues resolves valuesRef (pack ref grammar, one YAML mapping)
// and RFC 7386 merge-patches inline over it: null deletes, arrays replace —
// the same ladder direction as forProvider over providerConfigRef. The
// result is int-normalized for the Helm consumer (refval.NormalizeIntegral,
// the JSON-round-trip cousin of config.Load's normalizePackValues). No ref
// → inline passes through untouched with no pin.
func EffectiveValues(ctx context.Context, valuesRef string, inline map[string]any, cacheDir string) (map[string]any, string, error) {
    if valuesRef == "" {
        return inline, "", nil
    }
    base, pin, err := refval.Resolve(ctx, valuesRef, cacheDir)
    if err != nil {
        return nil, "", diag.Wrap(err, diag.CodePackValuesRefFetch,
            fmt.Sprintf("cannot fetch valuesRef %q", valuesRef),
            "the ref must resolve to one readable YAML mapping document (helm values shape)")
    }
    merged, err := refval.Merge(base, inline)
    if err != nil {
        return nil, "", diag.Wrap(err, diag.CodePackValuesRefFetch,
            fmt.Sprintf("cannot merge inline values over valuesRef %q", valuesRef),
            "check that both documents are plain YAML mappings")
    }
    return refval.NormalizeIntegral(merged).(map[string]any), pin, nil
}

// RenderResolved is the shared render entry for up.Run and diff's
// desiredState: the GT15 chartless guard extended to valuesRef (checked
// BEFORE any fetch — no network on a doomed render), EffectiveValues, then
// RenderWith. Returns the rendered objects plus the values pin for
// cube.lock's valuesPin column.
func RenderResolved(ctx context.Context, pk *Pack, pref config.PackRef, gw config.GatewaySpec, cacheDir string) (*Rendered, string, error) {
    if pref.ValuesRef != "" && !pk.HasChart() {
        return nil, "", diag.New(diag.CodePackValuesChartless,
            fmt.Sprintf("pack %s has no chart.yaml — valuesRef/values are helm values only (GT15)", pk.Name),
            "use extraManifests to add raw resources, or remove valuesRef")
    }
    values, pin, err := EffectiveValues(ctx, pref.ValuesRef, pref.Values, cacheDir)
    if err != nil {
        return nil, "", err
    }
    r, err := pk.RenderWith(values, pref.ExtraManifests, gw)
    if err != nil {
        return nil, "", err
    }
    return r, pin, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/pack/ ./internal/diag/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/pack/values.go internal/pack/values_test.go internal/diag/codes.go internal/diag/registry.go
git commit -m "feat(pack): EffectiveValues + RenderResolved — valuesRef fetch/merge, CUBE-4021 (RV2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/pack/values.go internal/pack/values_test.go internal/diag/codes.go internal/diag/registry.go
```

---

### Task 6: wire `valuesRef` into `up` + `diff`, lock `valuesRef`/`valuesPin`

**Files:**
- Modify: `internal/lock/lock.go` (Entry struct, ~line 30)
- Modify: `internal/up/up.go` (pass-1 loop, ~lines 356-383)
- Modify: `internal/diff/diff.go` (`desiredState` pack loop, ~lines 228-240 + its entries append)
- Test: `internal/diff/diff_test.go` (extend), `internal/lock/lock_test.go` (extend)

**Interfaces:**
- Consumes: `pack.RenderResolved` (Task 5).
- Produces: `lock.Entry.ValuesRef string` + `lock.Entry.ValuesPin string` (both omitempty). Both `up` and `diff` render EVERY pack through `RenderResolved` (which is a `RenderWith` pass-through when `ValuesRef == ""`).

- [ ] **Step 1: Write the failing tests**

`internal/lock/lock_test.go` — append:

```go
// Ref-less entries must serialize byte-identically to pre-RV2 locks (the
// p6 "stock records unchanged" discipline): absent valuesRef/valuesPin
// keys, never empty strings.
func TestLockEntryValuesFieldsOmitEmpty(t *testing.T) {
    f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
        Engine: EngineLock{Type: "flux"},
        Packs:  []Entry{{Ref: "packs/x", Name: "x", Version: "1", Resolved: "dir:h", RenderedHash: "h"}}}
    p := filepath.Join(t.TempDir(), "cube.lock")
    if err := Write(p, f); err != nil {
        t.Fatal(err)
    }
    raw, _ := os.ReadFile(p)
    if strings.Contains(string(raw), "valuesRef") || strings.Contains(string(raw), "valuesPin") {
        t.Fatalf("empty values fields serialized:\n%s", raw)
    }
    // And populated fields round-trip.
    f.Packs[0].ValuesRef, f.Packs[0].ValuesPin = "github.com/a/v//x@v1", "git+abc"
    if err := Write(p, f); err != nil {
        t.Fatal(err)
    }
    got, err := Read(p)
    if err != nil {
        t.Fatal(err)
    }
    if got.Packs[0].ValuesRef != "github.com/a/v//x@v1" || got.Packs[0].ValuesPin != "git+abc" {
        t.Fatalf("round-trip lost values fields: %+v", got.Packs[0])
    }
}
```

`internal/diff/diff_test.go` — append (uses the file's existing cube-fixture helpers; the values file is served from local disk so the test is network-free, and the same code path covers http/oci by grammar symmetry):

```go
// desiredState is where valuesRef must take effect for BOTH diff and up
// (up shares pack.RenderResolved): fetched base + inline override visible
// in the rendered objects, pin recorded in the lock entries.
func TestDesiredStateValuesRef(t *testing.T) {
    vals := filepath.Join(t.TempDir(), "values.yaml")
    // Pick a key the pack fixture's chart actually templates — reuse the
    // same chart fixture TestDesiredStateMatchesUpAppliedSet's cube uses
    // and set one of its known values keys here.
    if err := os.WriteFile(vals, []byte("replicas: 2\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    cube := testCube(t) // the file's existing fixture-cube helper
    cube.Spec.Packs[0].ValuesRef = vals
    _, _, entries, err := desiredState(context.Background(), cube, fakeEngine{})
    if err != nil {
        t.Fatal(err)
    }
    var e *lock.Entry
    for i := range entries {
        if entries[i].Ref == cube.Spec.Packs[0].Ref {
            e = &entries[i]
        }
    }
    if e == nil || e.ValuesRef != vals || !strings.HasPrefix(e.ValuesPin, "file:") {
        t.Fatalf("lock entry missing values pin: %+v", e)
    }
}
```

(Adapt `testCube(t)` to whatever helper `TestDesiredStateMatchesUpAppliedSet` actually uses to build its cube+pack fixtures — read that test first and reuse its scaffolding verbatim; the assertion body above is the contract. If the fixture pack is chartless, point `Packs[0]` at the chart-bearing fixture the repo's render tests use, or add `chart.yaml` to the fixture; a chartless pack correctly fails CUBE-4016 here.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lock/ ./internal/diff/ -run 'TestLockEntryValuesFields|TestDesiredStateValuesRef' -v`
Expected: FAIL — `e.ValuesRef undefined` (compile)

- [ ] **Step 3: Implement**

`internal/lock/lock.go` — add to `Entry` after `RenderedHash`:

```go
    // ValuesRef/ValuesPin record the pack's remote values source and its
    // resolved pin (spec 2026-07-19 §6) — absent for inline-only packs, so
    // ref-less locks stay byte-identical to pre-RV2 output (omitempty).
    ValuesRef string `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"`
    ValuesPin string `yaml:"valuesPin,omitempty" json:"valuesPin,omitempty"`
```

`internal/up/up.go` pass-1 loop — replace the `RenderWith` call (currently `rendered, err := pk.RenderWith(pref.Values, pref.ExtraManifests, cube.Spec.Gateway)`) with:

```go
            // GT15 (U4) + RV2: RenderResolved = the values stone extended to
            // valuesRef, remote base fetch (CUBE-4021), RFC 7386 inline-over-
            // fetched merge, then RenderWith (CUBE-4016/4017 unchanged).
            rendered, valuesPin, err := pack.RenderResolved(ctx, pk, pref, cube.Spec.Gateway, dir)
            if err != nil {
                return err
            }
```

and extend the `entries = append(entries, lock.Entry{…})` literal:

```go
                ValuesRef: pref.ValuesRef,
                ValuesPin: valuesPin,
```

(`dir` is the pack cache dir already in scope for `pack.Fetch` in that loop; verify the actual variable name at the call site and use it. Note `ValuesRef` is set unconditionally — it is `""` for inline-only packs, which omitempty drops.)

`internal/diff/diff.go` `desiredState` — same two changes around lines 228-240: replace `p.RenderWith(pr.Values, pr.ExtraManifests, cube.Spec.Gateway)` with `pack.RenderResolved(ctx, p, pr, cube.Spec.Gateway, dir)` capturing `valuesPin`, and set `ValuesRef: pr.ValuesRef, ValuesPin: valuesPin` in its `lock.Entry` construction. Also update the D11 `customized` computation in `up.go` (~line 509): `customized := len(refs[i].Values) > 0 || refs[i].ValuesRef != "" || refs[i].ExtraManifests != ""` — a remotely-valued pack IS customized.

- [ ] **Step 3b: Bundle-mode rails guard (`CUBE-7007`) — amendment 2**

`up --bundle` (spec §4.1, Phase 3) promises "no fetch ever touches the network" by rewriting every PACK ref to a bundle-local dir — it knows nothing about `valuesRef`, so a bundled cube carrying one would silently reach for the network. Fail loudly before any cluster mutation (the `CUBE-7005` fail-fast precedent).

`internal/diag/codes.go`, 70xx block after `CodeBundleImageLoadFail`:

```go
    CodeBundleRemoteSource Code = "CUBE-7007" // `up --bundle` with a remote values/tuning/config source — remote refs are not vendored, offline rails would be violated
```

`internal/diag/registry.go`, 70xx section:

```go
    CodeBundleRemoteSource: {Summary: "`up --bundle` with a remote values/tuning/config source — remote refs are not vendored, offline rails would be violated"},
```

`internal/up/up.go` — a pure, unit-testable helper (this task's clause covers `valuesRef`; Task 10 adds the remote-`-f` origin clause, gated Task 7 adds `tuningRef`):

```go
// bundleRailsCheck enforces offline honesty for --bundle runs (CUBE-7007):
// the bundle vendors pack refs and images, NOT remote values/tuning/config
// sources — any of those alongside a bundle would either touch the network
// or fail mid-install, so refuse before any cluster mutation.
func bundleRailsCheck(cube *config.Cube) error {
    for _, p := range cube.Spec.Packs {
        if p.ValuesRef != "" {
            return diag.New(diag.CodeBundleRemoteSource,
                fmt.Sprintf("pack %q has valuesRef %q — remote values are not vendored into the bundle", p.Ref, p.ValuesRef),
                "inline the values (remove valuesRef) for air-gapped installs, or run without --bundle")
        }
    }
    return nil
}
```

called in `Run` right after the bundle is opened/verified and before any cluster mutation (next to the existing `CUBE-7005` provider check):

```go
    if opts.Bundle != "" {
        if err := bundleRailsCheck(cube); err != nil {
            return err
        }
    }
```

Test (append to the up package's existing bundle/offline tests, calling the helper directly — no cluster needed):

```go
func TestBundleRailsCheckRejectsValuesRef(t *testing.T) {
    cube := &config.Cube{}
    cube.Spec.Packs = []config.PackRef{{Ref: "packs/x", ValuesRef: "github.com/a/v//x@v1"}}
    err := bundleRailsCheck(cube)
    if err == nil || !diag.HasCode(err, diag.CodeBundleRemoteSource) {
        t.Fatalf("err = %v, want CUBE-7007", err)
    }
    cube.Spec.Packs[0].ValuesRef = ""
    if err := bundleRailsCheck(cube); err != nil {
        t.Fatalf("clean cube rejected: %v", err)
    }
}
```

(Use the repo's real diag inspection helper, as in Task 5. Include `internal/diag/codes.go` + `registry.go` in this task's commit pathspec.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/lock/ ./internal/diff/ ./internal/up/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lock/ internal/up/up.go internal/diff/
git commit -m "feat(up,diff): render via RenderResolved — valuesRef wired, valuesPin in cube.lock (RV2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/lock/ internal/up/up.go internal/diff/
```

---

### Task 7: `engine.tuningRef` — `factory.NewResolved` + `CUBE-3012` + lock fields

> **DECISION LANDED 2026-07-19: engine-as-pack RATIFIED (`017057a`) — the
> ACCEPTED outcome below applies. This task is SKIPPED in this plan; its
> replacement (`engine.valuesRef` riding the pack machinery) is planned
> AFTER the p7 engine-as-pack implementation lands. Do not claim T7.**
>
> **⛔ GATED — do not execute until the engine-as-pack decision (amendment 1).**
> `docs/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md` (PROPOSED)
> deletes `engine.tuning` in favor of `engine.ref` + open `engine.values`, which
> would make everything below dead on arrival. Outcomes:
>
> - **Engine-as-pack REJECTED** → execute this task exactly as written, and add
>   a `tuningRef` clause to `bundleRailsCheck` (Task 6 Step 3b):
>   `if cube.Spec.Engine.TuningRef != "" { return diag.New(diag.CodeBundleRemoteSource, …) }`.
> - **Engine-as-pack ACCEPTED** → replace this task: the engine renders through
>   the pack machinery, so remote engine values become `engine.valuesRef` riding
>   `pack.EffectiveValues`/`RenderResolved` (Task 5) — same GT15 chartless guard
>   (flux engine pack + values = CUBE-4016), same merge, same pin plumbing into
>   the engine's lock section. `CUBE-3012` is then not needed; re-plan the thin
>   remainder against the accepted engine-as-pack spec.
>
> Skipping this task also skips Task 11's `tuning(…)` attribution block (marked there).
> All other tasks are independent of this one — execution order 1-6, 8-12, then 7.

**Files:**
- Modify: `internal/engine/factory/factory.go`
- Modify: `internal/diag/codes.go` (3xxx block after `CodeEngineDepWait`), `internal/diag/registry.go`
- Modify: `internal/lock/lock.go` (`EngineLock`)
- Modify: `internal/up/up.go:192` (factory call) + `~405` (lock assembly), `internal/diff/diff.go:79`
- Test: `internal/engine/factory/factory_test.go` (extend or create)

**Interfaces:**
- Consumes: `refval.Resolve/Merge` (Task 2), `factory.New(spec)` (existing).
- Produces:
  - `func NewResolved(ctx context.Context, spec config.EngineSpec, cacheDir string) (engine.Engine, string, error)` — resolves `spec.TuningRef`, strict-decodes, merges inline `spec.Tuning` on top, returns the engine built from the EFFECTIVE spec plus the tuning pin (`""` when no ref).
  - `diag.CodeEngineTuningRefFetch Code = "CUBE-3012"`
  - `lock.EngineLock.TuningRef/TuningPin string` (omitempty).

- [ ] **Step 1: Add the diag code + registry entry**

`internal/diag/codes.go`, 3xxx block after `CodeEngineDepWait`:

```go
    // Remote tuning (spec 2026-07-19 §5.2, §8).
    CodeEngineTuningRefFetch Code = "CUBE-3012" // engine.tuningRef fetch failed, not a YAML mapping, or not the closed tuning shape (strict decode)
```

`internal/diag/registry.go`, 3xxx section:

```go
    CodeEngineTuningRefFetch: {Summary: "engine.tuningRef fetch failed, not a YAML mapping, or not the closed tuning shape (strict decode)"},
```

Run: `go test ./internal/diag/ -count=1` — Expected: PASS

- [ ] **Step 2: Write the failing test**

```go
// internal/engine/factory/factory_test.go (append; create with package factory if absent)
package factory

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/cube-idp/cube-idp/internal/config"
    "github.com/cube-idp/cube-idp/internal/diag"
)

func writeTuning(t *testing.T, content string) string {
    t.Helper()
    f := filepath.Join(t.TempDir(), "tuning.yaml")
    if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
    return f
}

func TestNewResolvedMergesInlineOverFetched(t *testing.T) {
    ref := writeTuning(t, "components:\n  source-controller:\n    replicas: 2\n  kustomize-controller:\n    replicas: 2\n")
    one := 1
    spec := config.EngineSpec{Type: "flux", TuningRef: ref,
        Tuning: &config.EngineTuning{Components: map[string]config.ComponentTuning{
            "source-controller": {Replicas: &one}, // inline wins
        }}}
    eng, pin, err := NewResolved(context.Background(), spec, t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    if eng == nil || pin == "" {
        t.Fatalf("eng=%v pin=%q", eng, pin)
    }
    objs, err := eng.InstallManifests()
    if err != nil {
        t.Fatal(err)
    }
    replicas := map[string]int64{}
    for _, o := range objs {
        if o.GetKind() == "Deployment" {
            if r, found, _ := unstructuredNestedInt(o.Object, "spec", "replicas"); found {
                replicas[o.GetName()] = r
            }
        }
    }
    if replicas["source-controller"] != 1 { // inline override
        t.Fatalf("source-controller replicas = %d, want 1 (inline over fetched)", replicas["source-controller"])
    }
    if replicas["kustomize-controller"] != 2 { // fetched base
        t.Fatalf("kustomize-controller replicas = %d, want 2 (fetched)", replicas["kustomize-controller"])
    }
}

func TestNewResolvedStrictDecodeRejectsUnknownFields(t *testing.T) {
    ref := writeTuning(t, "components:\n  source-controller: {replicas: 2}\nnotAKnob: true\n")
    _, _, err := NewResolved(context.Background(), config.EngineSpec{Type: "flux", TuningRef: ref}, t.TempDir())
    if err == nil || !diag.HasCode(err, diag.CodeEngineTuningRefFetch) {
        t.Fatalf("err = %v, want CUBE-3012", err)
    }
}

func TestNewResolvedNoRefIsPlainNew(t *testing.T) {
    eng, pin, err := NewResolved(context.Background(), config.EngineSpec{Type: "flux"}, t.TempDir())
    if err != nil || pin != "" || eng == nil {
        t.Fatalf("eng=%v pin=%q err=%v", eng, pin, err)
    }
}
```

(As in Task 5: use the repo's actual diag code-inspection helper, and for reading replicas use `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured.NestedInt64` directly rather than the `unstructuredNestedInt` sketch.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/engine/factory/ -v`
Expected: FAIL — `undefined: NewResolved`

- [ ] **Step 4: Implement `NewResolved`**

Append to `internal/engine/factory/factory.go`:

```go
// NewResolved is New plus the tuningRef ladder (spec 2026-07-19 §5.2):
// fetch the base tuning document (pack ref grammar, one YAML mapping),
// STRICT-decode it into the closed GT1 knob set (an unknown field is
// CUBE-3012, never a silent drop), RFC 7386 merge-patch the inline tuning
// on top, and build the engine from that effective spec. Numeric typing
// stays JSON-native (float64/int64) end to end — unstructured SSA forbids
// plain int, so no NormalizeIntegral here (the deliberate asymmetry with
// pack values). Returns the tuning pin for cube.lock; "" without a ref.
func NewResolved(ctx context.Context, spec config.EngineSpec, cacheDir string) (engine.Engine, string, error) {
    if spec.TuningRef == "" {
        eng, err := New(spec)
        return eng, "", err
    }
    baseMap, pin, err := refval.Resolve(ctx, spec.TuningRef, cacheDir)
    if err != nil {
        return nil, "", diag.Wrap(err, diag.CodeEngineTuningRefFetch,
            fmt.Sprintf("cannot fetch tuningRef %q", spec.TuningRef),
            "the ref must resolve to one readable YAML mapping in the engine.tuning shape")
    }
    // Strict-decode the fetched base FIRST so a wrong-shaped document names
    // itself, not the post-merge blend.
    if err := strictTuning(baseMap, spec.TuningRef); err != nil {
        return nil, "", err
    }
    inlineMap := map[string]any{}
    if spec.Tuning != nil {
        j, err := json.Marshal(spec.Tuning)
        if err != nil {
            return nil, "", err
        }
        if err := json.Unmarshal(j, &inlineMap); err != nil {
            return nil, "", err
        }
    }
    merged, err := refval.Merge(baseMap, inlineMap)
    if err != nil {
        return nil, "", diag.Wrap(err, diag.CodeEngineTuningRefFetch,
            fmt.Sprintf("cannot merge inline tuning over tuningRef %q", spec.TuningRef),
            "check that both documents are plain YAML mappings")
    }
    var effective config.EngineTuning
    mj, err := json.Marshal(merged)
    if err != nil {
        return nil, "", err
    }
    if err := sigyaml.UnmarshalStrict(mj, &effective); err != nil {
        return nil, "", diag.Wrap(err, diag.CodeEngineTuningRefFetch,
            fmt.Sprintf("merged tuning from %q is not the closed tuning shape", spec.TuningRef),
            "only components.<name>.replicas and components.<name>.resources are tunable")
    }
    spec.Tuning = &effective // spec is a value copy — caller's cube untouched
    eng, err := New(spec)
    return eng, pin, err
}

// strictTuning validates a JSON-typed map against the closed EngineTuning
// shape with unknown fields rejected.
func strictTuning(m map[string]any, ref string) error {
    j, err := json.Marshal(m)
    if err != nil {
        return err
    }
    var t config.EngineTuning
    if err := sigyaml.UnmarshalStrict(j, &t); err != nil {
        return diag.Wrap(err, diag.CodeEngineTuningRefFetch,
            fmt.Sprintf("tuningRef %q is not the closed tuning shape", ref),
            "only components.<name>.replicas and components.<name>.resources are tunable")
    }
    return nil
}
```

New imports for factory.go: `context`, `encoding/json`, `sigyaml "sigs.k8s.io/yaml"`, `github.com/cube-idp/cube-idp/internal/refval`.

`internal/lock/lock.go` — `EngineLock` gains:

```go
    // TuningRef/TuningPin record the engine's remote tuning source and pin
    // (spec 2026-07-19 §6); absent for inline-only tuning (omitempty).
    TuningRef string `yaml:"tuningRef,omitempty" json:"tuningRef,omitempty"`
    TuningPin string `yaml:"tuningPin,omitempty" json:"tuningPin,omitempty"`
```

Wire the callers (pack cache dir + ctx are in scope at both):

- `internal/up/up.go:192`: `eng, err := enginefactory.New(cube.Spec.Engine)` → 

```go
    eng, tuningPin, err := enginefactory.NewResolved(ctx, cube.Spec.Engine, dir)
```

  (confirm the pack cache dir variable in scope at line 192 — if the cache dir is established later in `Run`, hoist the `pack.DefaultCacheDir()` call above the engine construction; it is pure path computation.) Then at the lock assembly (~line 403): `Engine: lock.EngineLock{Type: cube.Spec.Engine.Type, TuningRef: cube.Spec.Engine.TuningRef, TuningPin: tuningPin}`.
- `internal/diff/diff.go:79`: same substitution, discarding the pin into a variable used by its `desiredState` entries only if diff records engine lock data — it does not, so `eng, _, err := enginefactory.NewResolved(ctx, cube.Spec.Engine, cacheDir)` (hoist `pack.DefaultCacheDir()` if not already in scope).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/engine/... ./internal/lock/ ./internal/up/ ./internal/diff/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/engine/factory/ internal/diag/codes.go internal/diag/registry.go internal/lock/lock.go internal/up/up.go internal/diff/diff.go
git commit -m "feat(engine): tuningRef — factory.NewResolved strict fetch+merge, CUBE-3012, tuningPin in cube.lock (RV3)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/engine/factory/ internal/diag/codes.go internal/diag/registry.go internal/lock/lock.go internal/up/up.go internal/diff/diff.go
```

---

### Task 8: `config.LoadBytes` split + `Cube` origin + `SaveValidated` guard (`CUBE-0012`)

**Files:**
- Modify: `internal/config/load.go`, `internal/config/types.go`
- Modify: `internal/diag/codes.go` (0xxx block), `internal/diag/registry.go`
- Test: `internal/config/load_test.go` (extend)

**Interfaces:**
- Produces:
  - `func LoadBytes(raw []byte, src string) (*Cube, error)` — the full existing pipeline (YAML → probes → CUE → normalize → crossValidate → k8s-version default); `src` labels errors (a path or a ref).
  - `type Origin struct { Ref, Pin string; Remote bool }`, `func (c *Cube) Origin() Origin`, `func (c *Cube) MarkRemoteOrigin(ref, pin string)` — origin survives on the struct, never serialized (unexported field).
  - `SaveValidated` fails `CUBE-0012` on remote-origin cubes.
  - `diag.CodeConfigRemoteReadOnly Code = "CUBE-0012"`

- [ ] **Step 1: Add the diag code + registry entry**

`internal/diag/codes.go`, 0xxx block after `CodeProviderConfigRemoved`:

```go
    // Remote -f (spec 2026-07-19 §7).
    CodeConfigRemoteReadOnly Code = "CUBE-0012" // a config-mutating command ran against a remote -f ref (remote configs are read-only)
    CodeConfigRemoteFetch    Code = "CUBE-0013" // remote -f ref fetch failed or did not yield one YAML document
```

(0013 lands here too — Task 9 uses it.) Registry entries:

```go
    CodeConfigRemoteReadOnly: {Summary: "a config-mutating command ran against a remote -f ref (remote configs are read-only)"},
    CodeConfigRemoteFetch:    {Summary: "remote -f ref fetch failed or did not yield one YAML document"},
```

Run: `go test ./internal/diag/ -count=1` — Expected: PASS

- [ ] **Step 2: Write the failing test**

```go
// internal/config/load_test.go (append)
func TestLoadBytesEqualsLoad(t *testing.T) {
    doc := []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: Cube\nmetadata: {name: demo}\nspec:\n  cluster: {provider: kind}\n  engine: {type: flux}\n  gateway: {}\n")
    f := filepath.Join(t.TempDir(), "cube.yaml")
    os.WriteFile(f, doc, 0o644)
    fromFile, err := Load(f)
    if err != nil {
        t.Fatal(err)
    }
    fromBytes, err := LoadBytes(doc, "oci://example/cfg:1")
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(fromFile.Spec, fromBytes.Spec) {
        t.Fatal("LoadBytes result diverges from Load")
    }
}

func TestSaveValidatedRefusesRemoteOrigin(t *testing.T) {
    doc := []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: Cube\nmetadata: {name: demo}\nspec:\n  cluster: {provider: kind}\n  engine: {type: flux}\n  gateway: {}\n")
    c, err := LoadBytes(doc, "oci://example/cfg:1")
    if err != nil {
        t.Fatal(err)
    }
    c.MarkRemoteOrigin("oci://example/cfg:1", "oci:sha256:abc")
    if got := c.Origin(); !got.Remote || got.Ref != "oci://example/cfg:1" {
        t.Fatalf("origin = %+v", got)
    }
    err = SaveValidated(filepath.Join(t.TempDir(), "cube.yaml"), c)
    if err == nil || !diag.HasCode(err, diag.CodeConfigRemoteReadOnly) {
        t.Fatalf("err = %v, want CUBE-0012", err)
    }
}
```

(Again: use the repo's real diag inspection helper.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestLoadBytes|TestSaveValidatedRefuses' -v`
Expected: FAIL — `undefined: LoadBytes` / `c.MarkRemoteOrigin undefined`

- [ ] **Step 4: Implement**

`internal/config/load.go` — mechanical split. `Load` becomes:

```go
func Load(path string) (*Cube, error) {
    raw, err := os.ReadFile(path)
    if err != nil {
        return nil, diag.Wrap(err, diag.CodeConfigRead, fmt.Sprintf("cannot read %s", path),
            "run `cube-idp init` to generate a starter cube.yaml")
    }
    return LoadBytes(raw, path)
}

// LoadBytes runs the full validation pipeline (YAML decode, spoke +
// migration probes, CUE unify/validate/decode with defaults, pack-values
// normalization, cross-field checks, k8s-version default) on an
// already-read document. src labels errors — a file path for Load, the
// remote ref for cfgload's remote -f (spec 2026-07-19 §7.1).
func LoadBytes(raw []byte, src string) (*Cube, error) {
    // …every line of today's Load from `var doc map[string]any` to
    // `return &c, nil`, with each fmt.Sprintf("…%s…", path) → src …
}
```

`internal/config/types.go` — on `Cube`:

```go
// Origin describes where a Cube document was loaded from (spec 2026-07-19
// §7.2). Remote-origin cubes are read-only: SaveValidated refuses them.
type Origin struct {
    Ref    string
    Pin    string
    Remote bool
}
```

and inside the `Cube` struct an unexported field (yaml/json ignore unexported — origin never serializes):

```go
    origin Origin
```

with methods:

```go
// Origin reports where this Cube was loaded from. Zero value = local file.
func (c *Cube) Origin() Origin { return c.origin }

// MarkRemoteOrigin flags this Cube as loaded from a remote ref (cfgload's
// remote -f path). SaveValidated refuses to write remote-origin cubes.
func (c *Cube) MarkRemoteOrigin(ref, pin string) {
    c.origin = Origin{Ref: ref, Pin: pin, Remote: true}
}
```

`SaveValidated` — first lines:

```go
func SaveValidated(file string, cube *Cube) error {
    if cube.Origin().Remote {
        return diag.New(diag.CodeConfigRemoteReadOnly,
            fmt.Sprintf("config was loaded from remote ref %q — remote configs are read-only", cube.Origin().Ref),
            "fetch the file locally (git clone / curl / oras pull), edit it, and pass the local path to -f")
    }
    // …existing body unchanged…
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ ./internal/diag/ -count=1`
Expected: PASS (including all pre-existing Load tests — the split must be behavior-neutral)

- [ ] **Step 6: Commit**

```bash
git add internal/config/load.go internal/config/types.go internal/config/load_test.go internal/diag/codes.go internal/diag/registry.go
git commit -m "feat(config): LoadBytes split + Cube origin + SaveValidated remote guard CUBE-0012 (RV4)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/config/load.go internal/config/types.go internal/config/load_test.go internal/diag/codes.go internal/diag/registry.go
```

---

### Task 9: `internal/cfgload` — remote `-f` dispatch (`CUBE-0013`)

**Files:**
- Modify: `internal/pack/getter.go` (export `IsRemoteRef`)
- Create: `internal/cfgload/cfgload.go`
- Test: `internal/cfgload/cfgload_test.go` (create)

**Interfaces:**
- Consumes: `pack.FetchFile` (Task 1), `config.LoadBytes` + `MarkRemoteOrigin` (Task 8).
- Produces:
  - `func pack.IsRemoteRef(ref string) bool` — `oci://` OR bare-git OR explicit getter form.
  - `func cfgload.Load(ctx context.Context, pathOrRef string) (*config.Cube, error)` — stat-wins dispatch (spec §7.1): local file → `config.Load` byte-identical; missing + remote-shaped → fetch + `LoadBytes` + `MarkRemoteOrigin`; missing + not remote-shaped → `config.Load` (its `CUBE-0001`).

- [ ] **Step 1: Write the failing test**

```go
// internal/cfgload/cfgload_test.go
package cfgload

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/cube-idp/cube-idp/internal/diag"
)

const doc = "apiVersion: cube-idp.dev/v1alpha1\nkind: Cube\nmetadata: {name: demo}\nspec:\n  cluster: {provider: kind}\n  engine: {type: flux}\n  gateway: {}\n"

func TestLoadLocalFileWins(t *testing.T) {
    // A name that PARSES as a bare-git ref but exists on disk must load
    // locally (stat wins — the configs.d/cube.yaml ambiguity, spec §7.1).
    dir := t.TempDir()
    sub := filepath.Join(dir, "configs.d")
    os.MkdirAll(sub, 0o755)
    f := filepath.Join(sub, "cube.yaml")
    os.WriteFile(f, []byte(doc), 0o644)
    c, err := Load(context.Background(), f)
    if err != nil {
        t.Fatal(err)
    }
    if c.Origin().Remote {
        t.Fatal("local file mis-classified as remote")
    }
}

func TestLoadMissingLocalNonRefIsConfigRead(t *testing.T) {
    _, err := Load(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"))
    if err == nil || !diag.HasCode(err, diag.CodeConfigRead) {
        t.Fatalf("err = %v, want CUBE-0001", err)
    }
}

func TestLoadRemoteRefFetchFailureIsCUBE0013(t *testing.T) {
    // Unpinned bare-git remote ref: rejected inside the fetch machinery;
    // cfgload wraps as CUBE-0013 (cause chain keeps CUBE-4007).
    _, err := Load(context.Background(), "github.com/acme/cubes//prod")
    if err == nil || !diag.HasCode(err, diag.CodeConfigRemoteFetch) {
        t.Fatalf("err = %v, want CUBE-0013", err)
    }
}

func TestLoadRemoteHTTPSetsOrigin(t *testing.T) {
    // Serve a cube.yaml over http (getter grammar) — full remote path:
    // fetch, LoadBytes, origin marked with a pin.
    srv := newYAMLServer(t, doc) // httptest.NewServer serving /cube.yaml
    c, err := Load(context.Background(), srv.URL+"/cube.yaml")
    if err != nil {
        t.Fatal(err)
    }
    o := c.Origin()
    if !o.Remote || o.Pin == "" || o.Ref != srv.URL+"/cube.yaml" {
        t.Fatalf("origin = %+v", o)
    }
}
```

(`newYAMLServer`: an `httptest.NewServer` whose handler writes `doc` for any GET — 6 lines; if go-getter's http getter requires extras (it fetches plain files directly), mirror whatever `internal/pack`'s existing getter tests do for http fixtures. If no such precedent exists and the http leg proves brittle in a unit context, keep the first three tests and move the http assertion into Task 12's integration coverage — origin-marking is still proven via the exported pieces.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cfgload/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement**

`internal/pack/getter.go` — append:

```go
// IsRemoteRef reports whether ref is remote-shaped under the pack ref
// grammar: oci://, the bare git form, or an explicit getter form. Local
// paths and plain filenames are not remote. cfgload uses this for the -f
// dispatch (stat wins first — this is only consulted for missing paths).
func IsRemoteRef(ref string) bool {
    return strings.HasPrefix(ref, "oci://") || isGitRef(ref) || isGetterRef(ref)
}
```

`internal/cfgload/cfgload.go`:

```go
// Package cfgload dispatches the -f flag between a local cube.yaml and a
// remote ref (spec 2026-07-19 §7.1). It exists as its own package — not
// inside config.Load as the spec first sketched — because config must not
// import pack (pack imports config); the observable contract is identical:
// one dispatch point, every command inherits remote -f, zero flag changes.
//
// Dispatch: an existing local path always wins (this also disambiguates
// names like configs.d/cube.yaml that would otherwise parse as bare-git
// refs). A missing path that is remote-shaped (pack.IsRemoteRef) is
// fetched through the pack ref grammar — read-only: config.SaveValidated
// refuses remote-origin cubes (CUBE-0012). Anything else falls through to
// config.Load for the canonical CUBE-0001.
package cfgload

import (
    "context"
    "fmt"
    "os"

    "github.com/cube-idp/cube-idp/internal/config"
    "github.com/cube-idp/cube-idp/internal/diag"
    "github.com/cube-idp/cube-idp/internal/pack"
)

func Load(ctx context.Context, pathOrRef string) (*config.Cube, error) {
    if _, err := os.Stat(pathOrRef); err == nil {
        return config.Load(pathOrRef)
    }
    if !pack.IsRemoteRef(pathOrRef) {
        return config.Load(pathOrRef) // canonical CUBE-0001 for a missing local file
    }
    cacheDir, err := pack.DefaultCacheDir()
    if err != nil {
        return nil, err
    }
    raw, pin, err := pack.FetchFile(ctx, pathOrRef, cacheDir)
    if err != nil {
        return nil, diag.Wrap(err, diag.CodeConfigRemoteFetch,
            fmt.Sprintf("cannot fetch remote config %q", pathOrRef),
            "the -f ref must resolve to one readable cube.yaml; check the ref, your network, and credentials")
    }
    cube, err := config.LoadBytes(raw, pathOrRef)
    if err != nil {
        return nil, err
    }
    cube.MarkRemoteOrigin(pathOrRef, pin)
    return cube, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cfgload/ ./internal/pack/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cfgload/ internal/pack/getter.go
git commit -m "feat(cfgload): remote -f dispatch over the pack ref grammar, CUBE-0013 (RV4)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/cfgload/ internal/pack/getter.go
```

---

### Task 10: switch every `-f` consumer to `cfgload.Load`; remote lock path + info line

**Files:**
- Modify: `internal/up/up.go` (~100, ~405), `internal/diff/diff.go` (~47, ~111), `internal/upgrade/plan.go` (~31, ~35)
- Modify: `cmd/config.go:26,77`, `cmd/pack.go:341`, `cmd/sync.go:48,88`, `cmd/down.go:44,158`, `cmd/cnoe.go:33`, `cmd/repo.go:76`, `cmd/trust.go:48`, `cmd/status.go:275`, `cmd/spoke.go:33,68,136`, `cmd/get.go:171`, `cmd/doctor.go:35`
- NOT `cmd/root.go:184` (loads the literal `"cube.yaml"` for shell completion — stays `config.Load`) and NOT `config.SaveValidated`'s internal temp-file `Load`.
- Test: existing suites + `TestCommandTreeGolden` (must pass unchanged)

**Interfaces:**
- Consumes: `cfgload.Load(ctx, pathOrRef)` (Task 9). Every cobra site has `cmd.Context()`; `up.Run`/`diff.Run`/`upgrade.Plan` have `ctx`.

- [ ] **Step 1: Mechanical substitution**

In each listed `cmd/` site: `config.Load(file)` → `cfgload.Load(cmd.Context(), file)` (add the `cfgload` import; drop `config` import only where now unused). In `internal/up/up.go:100`, `internal/diff/diff.go:47`, `internal/upgrade/plan.go:31`: `config.Load(cfgPath)` → `cfgload.Load(ctx, cfgPath)`.

- [ ] **Step 2: Remote lock path (spec §7.3)**

`internal/up/up.go` (~line 405) — replace `lock.PathFor(cfgPath)`:

```go
    lockPath := lock.PathFor(cfgPath)
    if cube.Origin().Remote {
        lockPath = "cube.lock" // remote -f: dir(ref) is meaningless; CWD (spec §7.3)
    }
    if err := lock.Write(lockPath, lf); err != nil {
```

Same branch at `internal/diff/diff.go:111` and `internal/upgrade/plan.go:35` for `lock.Read(lock.PathFor(cfgPath))`. Factor as a tiny helper to avoid triplication — in `internal/lock/lock.go`:

```go
// PathForOrigin picks the cube.lock path: next to cube.yaml for local
// configs, ./cube.lock in the working directory for remote -f refs
// (spec 2026-07-19 §7.3).
func PathForOrigin(cfgPath string, remote bool) string {
    if remote {
        return "cube.lock"
    }
    return PathFor(cfgPath)
}
```

and call `lock.PathForOrigin(cfgPath, cube.Origin().Remote)` at all three sites.

- [ ] **Step 2b: Extend `bundleRailsCheck` with the remote-origin clause (amendment 2)**

Now that remote `-f` exists, a bundled run must also refuse a remote config source. In `internal/up/up.go`'s `bundleRailsCheck` (Task 6 Step 3b), add as the first check:

```go
    if cube.Origin().Remote {
        return diag.New(diag.CodeBundleRemoteSource,
            fmt.Sprintf("config was loaded from remote ref %q — remote configs are not vendored into the bundle", cube.Origin().Ref),
            "fetch the cube.yaml locally and pass the local path to -f for air-gapped installs")
    }
```

Test (append next to `TestBundleRailsCheckRejectsValuesRef`):

```go
func TestBundleRailsCheckRejectsRemoteOrigin(t *testing.T) {
    cube := &config.Cube{}
    cube.MarkRemoteOrigin("oci://example/cfg:1", "oci:sha256:abc")
    err := bundleRailsCheck(cube)
    if err == nil || !diag.HasCode(err, diag.CodeBundleRemoteSource) {
        t.Fatalf("err = %v, want CUBE-7007", err)
    }
}
```

- [ ] **Step 3: Remote info line (spec §7.3, deviation 3)**

`internal/up/up.go` right after the existing `con.Step("config", …)` line (~104):

```go
    if o := cube.Origin(); o.Remote {
        con.Step("config", "using remote config %s (%s)", o.Ref, o.Pin)
    }
```

`internal/diff/diff.go` — after its load, print through its `out io.Writer` via the same `ui` helper the file already uses for sections: `ui.NewFor(out).Section(fmt.Sprintf("using remote config %s (%s)", o.Ref, o.Pin))` guarded by `o.Remote` (match the file's existing ui call style).

- [ ] **Step 4: Verify — build, full test, CLI freeze**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS everywhere; specifically `go test ./cmd/ -run TestCommandTreeGolden -v` PASSES WITHOUT `-update` (no flag surface changed).

- [ ] **Step 5: Commit**

```bash
git add cmd/ internal/up/up.go internal/diff/diff.go internal/upgrade/plan.go internal/lock/lock.go
git commit -m "feat(cmd): every -f accepts remote refs via cfgload — read-only, CWD lock path (RV4)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- cmd/ internal/up/up.go internal/diff/diff.go internal/upgrade/plan.go internal/lock/lock.go
```

---

### Task 11: `upgrade --plan` attribution + `ClusterLock` providerConfig pin

**Files:**
- Modify: `internal/lock/lock.go` (`File` + new `ClusterLock`)
- Modify: `internal/up/up.go` (lock assembly ~403)
- Modify: `internal/upgrade/plan.go`
- Test: `internal/upgrade/plan_test.go` (extend — follow its existing fixture style)

**Interfaces:**
- Consumes: `pack.ResolveRemote(ctx, ref, cacheDir) (string, error)` (existing), `refval.Resolve` (Task 2), lock fields (Tasks 6/7).
- Produces: `lock.File.Cluster *lock.ClusterLock` (omitempty); `upgrade` rows for `values(<pack-ref>)`, `tuning(engine)`, `providerConfig(cluster)` — distinct from pack rows.

- [ ] **Step 1: Write the failing test**

```go
// internal/upgrade/plan_test.go (append)
// Values-source drift must surface as its own row, never fold into the
// pack's chart row (spec 2026-07-19 §6).
func TestClassifyRefRow(t *testing.T) {
    r := classify("dir:h1:AAA", "dir:h1:BBB")
    if r.Change != "update available" {
        t.Fatalf("change = %q", r.Change)
    }
    r = classify("", "dir:h1:AAA")
    if r.Change != "new (not in cube.lock)" {
        t.Fatalf("change = %q", r.Change)
    }
    r = classify("dir:h1:AAA", "dir:h1:AAA")
    if r.Change != "up to date" {
        t.Fatalf("change = %q", r.Change)
    }
}
```

plus extend the file's existing end-to-end plan test fixture (whatever builds a cube+lock and runs `Plan`) with: a pack that has `ValuesRef` pointing at a local values file and a lock entry whose `ValuesPin` differs — assert the rendered table contains a row named `values(<that-ref>)` with `update available`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/upgrade/ -run TestClassifyRefRow -v`
Expected: FAIL — `classify` takes `(*lock.Entry, string)` today

- [ ] **Step 3: Implement**

`internal/lock/lock.go`:

```go
// ClusterLock records the cluster's remote provider-config source and pin
// (spec 2026-07-19 §6); the whole section is absent for inline-only
// clusters (omitempty pointer).
type ClusterLock struct {
    ProviderConfigRef string `yaml:"providerConfigRef,omitempty" json:"providerConfigRef,omitempty"`
    ProviderConfigPin string `yaml:"providerConfigPin,omitempty" json:"providerConfigPin,omitempty"`
}
```

and in `File` after `Engine`:

```go
    Cluster *ClusterLock `yaml:"cluster,omitempty" json:"cluster,omitempty"`
```

`internal/up/up.go` lock assembly (~403): populate it by resolving the (cached — the cluster ensure already fetched it) ref once more via `refval.Resolve`, avoiding deep plumbing through the provider interface (planning decision):

```go
    var clusterLock *lock.ClusterLock
    if ref := cube.Spec.Cluster.ProviderConfigRef; ref != "" {
        if _, pin, err := refval.Resolve(ctx, ref, dir); err == nil {
            clusterLock = &lock.ClusterLock{ProviderConfigRef: ref, ProviderConfigPin: pin}
        } // best-effort: the cluster is already up from this exact ref; a
        // transient re-resolve failure must not fail the whole up.
    }
    lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
        Engine: lock.EngineLock{Type: cube.Spec.Engine.Type, TuningRef: cube.Spec.Engine.TuningRef, TuningPin: tuningPin},
        Cluster: clusterLock, Packs: entries}
```

`internal/upgrade/plan.go`:

1. `classify` becomes pin-string based:

```go
func classify(current, latest string) Row {
    switch {
    case current == "":
        return Row{Latest: latest, Change: "new (not in cube.lock)"}
    case current == latest:
        return Row{Current: current, Latest: latest, Change: "up to date"}
    default:
        return Row{Current: current, Latest: latest, Change: "update available"}
    }
}
```

   The pack loop adapts: `locked := lockEntryByRef(lf, pr.Ref)`, `current := ""; if locked != nil { current = locked.Resolved }`, `row := classify(current, latest)`.

2. After the pack loop, append attribution rows (spec §6 — reported DISTINCTLY):

```go
    // Values sources (spec 2026-07-19 §6): each valuesRef row is its own
    // line item so "values source changed" never masquerades as a chart
    // change. ResolveRemote computes the would-be pin without pulling
    // (http/s3 probe excepted, its documented exception).
    for _, pr := range refs {
        if pr.ValuesRef == "" {
            continue
        }
        latest, err := pack.ResolveRemote(ctx, pr.ValuesRef, cacheDir)
        if err != nil {
            return false, err
        }
        locked := lockEntryByRef(lf, pr.Ref)
        current := ""
        if locked != nil {
            current = locked.ValuesPin
        }
        row := classify(current, latest)
        row.Name = fmt.Sprintf("values(%s)", pr.ValuesRef)
        if row.Change != "up to date" {
            changed = true
        }
        rows = append(rows, row)
    }
    // GATED with Task 7 (amendment 1): include this tuning block ONLY if
    // Task 7 was executed; skip it entirely if Task 7 is held/replaced.
    if tr := cube.Spec.Engine.TuningRef; tr != "" {
        latest, err := pack.ResolveRemote(ctx, tr, cacheDir)
        if err != nil {
            return false, err
        }
        row := classify(lf.Engine.TuningPin, latest)
        row.Name = fmt.Sprintf("tuning(%s)", tr)
        if row.Change != "up to date" {
            changed = true
        }
        rows = append(rows, row)
    }
    if pcr := cube.Spec.Cluster.ProviderConfigRef; pcr != "" {
        latest, err := pack.ResolveRemote(ctx, pcr, cacheDir)
        if err != nil {
            return false, err
        }
        current := ""
        if lf.Cluster != nil {
            current = lf.Cluster.ProviderConfigPin
        }
        row := classify(current, latest)
        row.Name = fmt.Sprintf("providerConfig(%s)", pcr)
        if row.Change != "up to date" {
            changed = true
        }
        rows = append(rows, row)
    }
```

Caveat: `ResolveRemote` pins a local FILE ref via its dir/probe path and may not produce `file:<sha256>` — check its local-path branch; if it only handles dirs, extend it with the same `file:` hashing FetchFile uses (Task 1) so plan/lock pins compare like-for-like. Add a unit test for that branch if extended.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/upgrade/ ./internal/lock/ ./internal/up/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lock/lock.go internal/up/up.go internal/upgrade/
git commit -m "feat(upgrade): plan attributes values/tuning/providerConfig source drift; cluster pin in cube.lock (RV5)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- internal/lock/lock.go internal/up/up.go internal/upgrade/
```

---

### Task 12: docs, e2e legs, full verification

**Files:**
- Modify: `README.md` (features/config reference section), `docs/pack-contract-v1.md` only if it documents `packs[].values` (check; valuesRef is a cube.yaml concern, likely README-only), `docs/machine-readable-output.md` (only if cube.lock schema is documented there — check)
- Modify: `tests/e2e/e2e_test.go` (extend the existing flow)
- Test: full suite

**Interfaces:** consumes everything above; produces no new APIs.

- [ ] **Step 1: README**

Add a "Remote values and config" subsection next to the existing `providerConfigRef`/values docs, covering: the TWO ref surfaces shipped by this plan — `packs[].valuesRef` and remote `-f` (NOT `engine.tuningRef`, dropped by Amendment 4) — with one YAML example each (adapt the spec §3 example, omitting its tuning row), the ref grammar table (spec §3.1), merge semantics (inline wins; `null` deletes; arrays replace), pin recording (`cube.lock` `valuesPin`/`cluster.providerConfigPin`), remote `-f` read-only rule + CWD `cube.lock`, and the four new CUBE codes (`CUBE-4021`, `CUBE-7007`, `CUBE-0014`, `CUBE-0015`). Keep the voice/format of the DEP4 README section added in commit `95a7b09`.

- [ ] **Step 2: e2e legs** (extend `tests/e2e/e2e_test.go`, gated by the existing `CUBE_IDP_E2E=1`; follow the file's helper conventions — it already shells the built binary and patches the generated cube.yaml)

TWO additions to the existing flow, at the point after the first successful `up` (the third, `tuningRef`, is DROPPED by Amendment 4):

1. **valuesRef leg:** write a values YAML for a pack the flow already installs (override one benign knob, e.g. a label or replica count the pack's chart templates), add `valuesRef: <local path>` to that pack in the cube.yaml, re-run `up`, then assert (a) `cube.lock` entry for that pack carries `valuesRef` + a `valuesPin` starting `file:`, and (b) `kubectl get` on the affected object shows the overridden value.
2. **remote `-f` leg:** `up -f <ref>` where the ref is the cube.yaml served remotely. Use the in-cluster gitea only if the harness already exposes a clonable URL helper; otherwise serve the file from a local `httptest`-style static server in the test process (an `http://127.0.0.1:<port>/cube.yaml` getter ref — same grammar leg). Assert `cube.lock` lands in the test's CWD and a mutating command (`cube-idp pack install … -f <same-ref>` or `spoke add`) exits non-zero mentioning `CUBE-0014` (the read-only guard's post-p7 number — Amendment 5; `CUBE-0012` now belongs to p7's `CodeEngineTuningRemoved`).

Each leg re-uses the harness's existing `runCLI`/kubectl helpers; no new flags, no new env vars.

- [ ] **Step 3: Full verification**

Run:
```bash
go build ./... && go vet ./... && go test ./... -count=1
go test ./cmd/ -run TestCommandTreeGolden -v   # must pass with NO golden update
```
Expected: all PASS. Then, if a docker-capable machine is at hand (owner decision — live e2e was deferred in p6 too):
```bash
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestE2E -v -timeout 30m
```

- [ ] **Step 4: Commit**

```bash
git add README.md tests/e2e/ docs/
git commit -m "docs+e2e: remote values/tuning/config — README section, e2e legs (RV5)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- README.md tests/e2e/ docs/
```

---

## Agent Execution Protocol

- One agent, one task, then stop. Strict order **T1→T6, then T8→T12** (T7
  is GATED_SKIP — engine-as-pack RATIFIED `017057a`; its replacement,
  `engine.valuesRef`, is planned post-p7 and is NOT part of this plan).
  Task 11 SKIPS its `tuning(…)` attribution block (marked in-task).
- Single repo: `$ROOT` only (no `$PACKS` in this plan).
- WORKTREE/BRANCH: all work happens on the EXISTING branch
  `2026-07-19-valuesref-remote-config` (the branch holding this plan, the
  spec, and this ledger) via a dedicated worktree, created once and reused:
  `git -C $ROOT worktree add $ROOT/.claude/worktrees/rv-valuesref 2026-07-19-valuesref-remote-config`
  (if the branch is checked out in the main tree, move that checkout to
  another branch first, or work in the main tree directly if it already sits
  on this branch — one checkout per branch is a git constraint). Code AND
  ledger commits land on this branch. Never commit to main.
- OUTWARD ACTS: none until T12. T12 ends with `git push -u origin
  2026-07-19-valuesref-remote-config` and `gh pr create --base main`
  (owner pre-authorized in the dispatch prompt). No tag pushes anywhere.
- CLAIM before any code: set the task's STATUS below to
  `IN_PROGRESS(<session>, <UTC ts>)`, commit
  `docs: rv plan — claim T<N>` with explicit pathspec. CLOSE after the
  task's gate passes: tick the task's checkboxes, fill EVERY Outcome field
  (evidence = pasted command output, not paraphrase), STATUS → DONE /
  DONE_WITH_CONCERNS / BLOCKED, commit `docs: rv plan — T<N> complete`.
- Gate for every task: `go build ./... && go vet ./... && go test ./...
  -count=1` in the worktree — real runs, never LSP diagnostics — PLUS
  `go test ./cmd/ -run TestCommandTreeGolden` passing WITHOUT `-update`.
- **DIAG-CODE RENUMBER RULE:** the p7 engine-as-pack plan also allocates
  `CUBE-0012/0013` (its T4) — whichever plan lands second renumbers. The
  CONSTANT NAMES in this plan (`CodeConfigRemoteReadOnly`,
  `CodeConfigRemoteFetch`, `CodePackValuesRefFetch`,
  `CodeBundleRemoteSource`) and their registry entries are normative; the
  NUMBERS are not — at claim time (T5/T6/T8), take the next free number in
  the domain block, record the actual number in FINDINGS + HANDOFF, and use
  it consistently in tests/docs for all later tasks.
- p7 COORDINATION: branch `p7/engine-as-pack` was created FROM this branch;
  both plans touch `up.go`/`config`/`lock`. Before claiming any task,
  cross-check `git log --oneline -15` for p7 merges that moved shared
  files; treat drift via the plan's own escape hatch (verify against the
  real code, minimal correction, FINDINGS entry).

## Ledger

### T1 — FetchFile returns pin [Task 1]
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

COMMITS:
- `a8c1cb6` docs: rv plan — claim T1
- `6d67f61` feat(pack): FetchFile returns the reproducibility pin (RV1)
- `<this>` docs: rv plan — T1 complete

FINDINGS:
1. Plan Step 3's code sketch matched the real `internal/pack/fetchfile.go`
   verbatim (same branches, same helper names `pullOCI`/`fetchGitTree`/
   `isGetterRef`/`sanitizeRef`/`fetchGetter`/`dirPin`/`singleYAML`) — applied
   as written, no correction needed. `dirPin` (`internal/pack/source.go:93`)
   already returns the `dir:`-prefixed form, so the getter/local-dir branches
   return it unmodified; `fetchGitTree` already returns the `git+<sha>` pin.
2. Step 5's commit pathspec list was extended by ONE file:
   `internal/pack/fetchfile_test.go`. Step 3 explicitly instructs to grep and
   fix test call sites (6 of them in that file); they must ride the same
   commit or the tree does not build. No other file touched.
3. Untracked new test file required `git add` before the pathspec commit
   (the plan's `git add` line covers this — the commit-with-pathspec form
   alone errors `did not match any file(s) known to git` for a new file).
4. Machine gotcha (affects every later task): this checkout has NO git
   identity configured (`~/.gitconfig` has no `[user]`, `.git/config` has
   none). Commits must be made as
   `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl" commit …`
   (matching every prior commit's author on this branch). Nothing was written
   to any git config file.
5. No p7 (engine-as-pack) merges on this branch — `git log --oneline -15`
   head was `394b39b` as the dispatcher stated; no shared-file drift.
6. Diag-code renumber rule: NOT applicable to T1 (allocates no codes).

BLOCKERS: none

HANDOFF:
- `pack.FetchFile(ctx, ref, cacheDir) ([]byte, string, error)` is live. Pin
  forms actually produced: `oci:<digest>` (oci://), `git+<sha>` (bare git),
  `dir:<h1:…>` (getter refs + local directories, via `dirPin`),
  `file:<sha256-hex>` (direct local FILE — the spec-deviation-ledger item 2
  form, implemented as `"file:" + hex(sha256(bytes))`).
- Pin is returned alongside a non-nil `singleYAML` error on the oci/git/getter
  branches (callers check err first); error branches return `""`.
- `internal/cluster/compose/compose.go:29` is a compile-fix only
  (`raw, _, err := pack.FetchFile(...)`) — the real `refval` migration is T3.
- The ONLY production call site remains compose.go:29; test call sites live in
  `internal/pack/fetchfile_test.go` (all updated).
- Evidence — `go build ./... && go vet ./... && go test ./... -count=1`
  (all green, 0 failures; tail):
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	11.703s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	5.756s
  ok  	github.com/cube-idp/cube-idp/internal/pack	15.410s
  ok  	github.com/cube-idp/cube-idp/internal/up	11.633s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	11.759s
  ```
- Evidence — new tests (Step 2 failed first with
  `assignment mismatch: 3 variables but FetchFile returns 2 values`, exactly
  the plan's Expected):
  ```
  === RUN   TestFetchFilePinLocalFile
  --- PASS: TestFetchFilePinLocalFile (0.00s)
  === RUN   TestFetchFilePinLocalDir
  --- PASS: TestFetchFilePinLocalDir (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/pack	1.119s
  ```
- Evidence — F1 CLI freeze (no `-update`, `cmd/` tree clean afterwards):
  ```
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.257s
  ```

### T2 — internal/refval resolver [Task 2]
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

COMMITS:
- `ee19943` docs: rv plan — claim T2
- `7edbcb6` feat(refval): shared one-YAML ref resolver — Resolve/Merge/NormalizeIntegral (RV1)
- `<this>` docs: rv plan — T2 complete

FINDINGS:
1. Amendment 4 applied to the package doc comment: the consumer list is
   `cluster.providerConfigRef (compose), packs[].valuesRef, and remote -f`
   (no `engine.tuningRef`) and the wrap-code list reads
   `(CUBE-1005 / CUBE-4021 / CUBE-0015)`. The plan body at Task 2 Step 3 had
   already been rewritten to the amended text, so the file was written
   verbatim from the plan — no further correction needed.
2. No anchor drift for this task: Task 2 creates two NEW files only
   (`internal/refval/refval.go`, `internal/refval/refval_test.go`) and
   touches nothing p7 moved. `internal/refval/` did not exist beforehand
   (`ls internal/refval` → `No such file or directory`).
3. T1's live signature verified before implementing:
   `internal/pack/fetchfile.go:24` is
   `func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, string, error)`
   — matches the plan's Consumes contract exactly.
4. `go.mod` gains nothing: both new imports are already DIRECT requirements —
   `github.com/evanphx/json-patch/v5 v5.9.11` (go.mod:52, direct block) and
   `sigs.k8s.io/yaml v1.6.0` (go.mod:31). `go.mod`/`go.sum` are unmodified by
   this task (`git status --short` showed only `?? internal/refval/`).
5. `NormalizeIntegral`'s doc comment retains the "NOT for engine tuning"
   sentence exactly as the plan specifies. It is a statement about the
   function's applicability, not a `tuningRef` implementation, so Amendment 4
   does not touch it. `refval.Merge` is currently a verbatim copy of
   `compose.Merge`, not yet a delegation — the compose migration is T3, as
   planned; both exist side by side for one commit.
6. Diag-code renumber rule: NOT applicable to T2 (allocates no codes; refval
   deliberately passes pack-layer diag codes through unwrapped).
7. Machine gotcha (T1 finding 4) reconfirmed: commits required
   `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl"`. Nothing
   was written to any git config file.

BLOCKERS: none

HANDOFF:
- `internal/refval` is live with the exact Task 2 "Interfaces" contract:
  - `func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error)`
    — `ref == ""` → `(empty non-nil map, "", nil)`; non-mapping YAML → error;
    empty file (JSON `null`) → empty non-nil map; pack-layer diag codes pass
    through UNWRAPPED (each consumer adds its own domain code).
  - `func Merge(base, patch map[string]any) (map[string]any, error)` — RFC 7386
    via `jsonpatch.MergePatch`, inputs untouched (verified by the test), never
    returns a nil map.
  - `func NormalizeIntegral(v any) any` — float64 with integral value in
    [MinInt32, MaxInt32] → `int`; recurses `map[string]any` and `[]any`
    IN PLACE (it mutates and returns the same container — callers that must
    preserve the original must copy first).
- Pin forms reaching callers are T1's verbatim: `oci:<digest>`, `git+<sha>`,
  `dir:<h1:…>`, `file:<sha256-hex>`.
- Numbers are float64 after `Resolve`/`Merge` (sigs.k8s.io/yaml JSON typing);
  normalization is a SEPARATE explicit step, Helm path only (T5).
- T3 can now rewrite `compose.Resolve`/`Merge`/`Compose` as `refval` wrappers;
  `internal/cluster/compose/compose.go` still holds its own
  `encoding/json`/`jsonpatch`/`sigyaml`/`pack` imports and its own
  `CUBE-1005` (`diag.CodeProviderConfigRefFetch`) wrapping, which T3 must
  preserve while delegating.
- Evidence — Step 2 failed exactly as the plan's Expected ("package does not
  exist / undefined symbols"):
  ```
  # github.com/cube-idp/cube-idp/internal/refval [.../internal/refval.test]
  internal/refval/refval_test.go:21:17: undefined: Resolve
  internal/refval/refval_test.go:51:14: undefined: Merge
  internal/refval/refval_test.go:66:9: undefined: NormalizeIntegral
  FAIL	github.com/cube-idp/cube-idp/internal/refval [build failed]
  ```
- Evidence — Step 4, `go test ./internal/refval/ -v -count=1` (5 tests, PASS):
  ```
  --- PASS: TestResolveEmptyRef (0.00s)
  --- PASS: TestResolveLocalFile (0.00s)
  --- PASS: TestResolveRejectsNonMapping (0.00s)
  --- PASS: TestMergeNullDeletesAndArraysReplace (0.00s)
  --- PASS: TestNormalizeIntegral (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/refval	1.390s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./... -count=1`:
  `BUILD OK`, `VET OK`, 32 `ok` packages, ZERO FAIL lines. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	17.969s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	5.523s
  ok  	github.com/cube-idp/cube-idp/internal/pack	14.459s
  ok  	github.com/cube-idp/cube-idp/internal/refval	11.594s
  ok  	github.com/cube-idp/cube-idp/internal/up	8.901s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	8.747s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	10.070s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden` with
  NO `-update` (tree clean afterwards: only `?? internal/refval/`):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.237s
  ```

### T3 — compose migration + providerConfig pin [Task 3]
STATUS: IN_PROGRESS(4a9e20e0-d82f-4974-b1c1-99d2adacd233, 2026-07-19T19:33:44Z)
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T4 — config surface valuesRef ONLY (tuningRef DROPPED, Amendment 4) [Task 4]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T5 — pack.EffectiveValues + RenderResolved + CUBE-4021 [Task 5]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS (actual diag number if renumbered) · BLOCKERS · HANDOFF:

### T6 — up/diff wiring + lock valuesPin + bundle guard CUBE-7007 [Task 6 incl. Step 3b]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS (actual diag number if renumbered) · BLOCKERS · HANDOFF:

### T7 — engine.tuningRef [Task 7 — DO NOT CLAIM]
STATUS: GATED_SKIP (engine-as-pack RATIFIED 017057a; replacement engine.valuesRef planned post-p7)
Outcome: n/a

### T8 — config.LoadBytes + origin + SaveValidated guard [Task 8]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF (numbers T9/T10/T12 must use). NUMBERS ARE FIXED BY AMENDMENT 5: `CUBE-0014` = `CodeConfigRemoteReadOnly`, `CUBE-0015` = `CodeConfigRemoteFetch` — do not re-derive:

### T9 — cfgload remote -f dispatch [Task 9]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T10 — call-site migration + lock CWD path + origin bundle clause [Task 10 incl. Step 2b]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF (clitree golden verified untouched):

### T11 — upgrade --plan attribution + ClusterLock (tuning block SKIPPED) [Task 11]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T12 — docs + e2e legs + full gate + PUSH + PR [Task 12, outward acts authorized]
STATUS: UNCLAIMED
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF (PR URL, e2e verdicts or deferral note):

## Self-Review Notes (already applied)

- **Spec coverage:** §3 config surface → Task 4; §3.1 grammar + §4/§4.1/§4.2 resolver → Tasks 1-3; §5.1 values ladder → Tasks 5-6; §5.2 tuning ladder → Task 7; §5.3 resolution site → Tasks 6-7 (up/diff only, `config.Load` stays offline); §6 pins/lock/plan → Tasks 6, 7, 11; §7 remote `-f` → Tasks 8-10; §8 codes → Tasks 5, 7, 8; §9 testing → per-task + Task 12; §10 lanes → task ordering.
- **Amendments applied (2026-07-19 capability assessment):** Task 7 gated on the engine-as-pack decision (banner in-task; Task 11's tuning block marked); bundle-mode rails guard `CUBE-7007` added (Task 6 Step 3b valuesRef clause, Task 10 Step 2b remote-origin clause, tuningRef clause travels with gated Task 7); gateway-valuesRef / spoke-providerConfig-pins / extraManifestsRef recorded as out of scope.
- **Known judgment calls an implementer may hit:** (1) exact fixture helpers in `diff_test.go`/`plan_test.go`/e2e — reuse what exists, the assertions above are the contract; (2) the diag error-inspection helper name — use the repo's, don't invent; (3) `ResolveRemote` local-file pin parity (Task 11 caveat); (4) if `engine/factory` importing `refval` ever cycles (it should not — `pack` does not import `engine`), fall back to resolving tuning in `up`/`diff` before `factory.New` and keep `New` untouched.
