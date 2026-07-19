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
- **Amendment 6 — remote refs are git/oci-shaped; a raw `http(s)://…/file.yaml`
  ref does NOT work.** Discovered empirically by T9 (probed, then the probe
  deleted): `pack.fetchGetter` hardcodes go-getter's `ClientModeDir`, so
  `HttpGetter` demands an `X-Terraform-Get` redirect and a plain YAML body
  fails. This is pre-existing pack behaviour, NOT a regression, and fixing it
  is OUT OF SCOPE for this plan. Consequences, both NORMATIVE:
  (a) Task 12's remote-`-f` e2e leg MUST NOT use an
  `http://127.0.0.1:<port>/cube.yaml` getter ref. Use the network-free
  in-process OCI precedent T9 validated in its own tests — the
  go-containerregistry in-process registry + `oci.PushPackDir` pattern from
  `internal/pack/catalog_test.go` — and an `oci://` ref.
  (b) Task 12's docs subsection MUST describe remote `-f` (and every other
  ref surface) as git/oci-shaped, and must NOT show a raw `https://` object
  URL as a working `-f` example.

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

- [x] **Step 1: Write the failing test** (append to the existing compose test file)

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

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/compose/ -run TestResolveReturnsPin -v`
Expected: FAIL — compile error (2-value return)

- [x] **Step 3: Rewrite `Resolve`/`Compose` as `refval` wrappers**

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

- [x] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cluster/... -count=1`
Expected: PASS

- [x] **Step 5: Commit**

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

- [x] **Step 1: Write the failing test**

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

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadValuesRef -v`
Expected: FAIL — `c.Spec.Packs[0].ValuesRef undefined` (compile), then after types-only fix, CUE rejection `valuesRef: field not allowed`

- [x] **Step 3: Add the fields**

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

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -count=1`
Expected: PASS

- [x] **Step 5: Commit**

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

- [x] **Step 1: Add the diag code (registry test would fail otherwise the moment the constant exists — do both together)**

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

- [x] **Step 2: Write the failing test**

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

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/pack/ -run 'TestEffectiveValues|TestRenderResolved' -v`
Expected: FAIL — `undefined: EffectiveValues`, `undefined: RenderResolved`

- [x] **Step 4: Implement `internal/pack/values.go`**

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

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/pack/ ./internal/diag/ -count=1`
Expected: PASS

- [x] **Step 6: Commit**

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

- [x] **Step 1: Write the failing tests**

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

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lock/ ./internal/diff/ -run 'TestLockEntryValuesFields|TestDesiredStateValuesRef' -v`
Expected: FAIL — `e.ValuesRef undefined` (compile)

- [x] **Step 3: Implement**

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

- [x] **Step 3b: Bundle-mode rails guard (`CUBE-7007`) — amendment 2**

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

- [x] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/lock/ ./internal/diff/ ./internal/up/ -count=1`
Expected: PASS

- [x] **Step 5: Commit**

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

- [x] **Step 1: Add the diag code + registry entry**

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

- [x] **Step 2: Write the failing test**

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

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestLoadBytes|TestSaveValidatedRefuses' -v`
Expected: FAIL — `undefined: LoadBytes` / `c.MarkRemoteOrigin undefined`

- [x] **Step 4: Implement**

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

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ ./internal/diag/ -count=1`
Expected: PASS (including all pre-existing Load tests — the split must be behavior-neutral)

- [x] **Step 6: Commit**

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

- [x] **Step 1: Write the failing test**

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

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cfgload/ -v`
Expected: FAIL — package does not exist

- [x] **Step 3: Implement**

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

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cfgload/ ./internal/pack/ -count=1`
Expected: PASS

- [x] **Step 5: Commit**

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

Add a "Remote values and config" subsection next to the existing `providerConfigRef`/values docs, covering: the TWO ref surfaces shipped by this plan — `packs[].valuesRef` and remote `-f` (NOT `engine.tuningRef`, dropped by Amendment 4) — with one YAML example each (adapt the spec §3 example, omitting its tuning row), the ref grammar table (spec §3.1), merge semantics (inline wins; `null` deletes; arrays replace), pin recording (`cube.lock` `valuesPin`/`cluster.providerConfigPin`), remote `-f` read-only rule + CWD `cube.lock`, and the four new CUBE codes (`CUBE-4021`, `CUBE-7007`, `CUBE-0014`, `CUBE-0015`). Per Amendment 6, describe every ref surface as git/oci-shaped — do NOT show a raw `https://…/cube.yaml` object URL as a working `-f` example, because `pack.fetchGetter`'s `ClientModeDir` makes it fail. Keep the voice/format of the DEP4 README section added in commit `95a7b09`.

- [ ] **Step 2: e2e legs** (extend `tests/e2e/e2e_test.go`, gated by the existing `CUBE_IDP_E2E=1`; follow the file's helper conventions — it already shells the built binary and patches the generated cube.yaml)

TWO additions to the existing flow, at the point after the first successful `up` (the third, `tuningRef`, is DROPPED by Amendment 4):

1. **valuesRef leg:** write a values YAML for a pack the flow already installs (override one benign knob, e.g. a label or replica count the pack's chart templates), add `valuesRef: <local path>` to that pack in the cube.yaml, re-run `up`, then assert (a) `cube.lock` entry for that pack carries `valuesRef` + a `valuesPin` starting `file:`, and (b) `kubectl get` on the affected object shows the overridden value.
2. **remote `-f` leg:** `up -f <ref>` where the ref is the cube.yaml served remotely. Use the in-cluster gitea only if the harness already exposes a clonable URL helper; otherwise use the NETWORK-FREE in-process OCI precedent T9 validated (the go-containerregistry in-process registry + `oci.PushPackDir` pattern from `internal/pack/catalog_test.go`, then an `oci://` ref). **Do NOT use an `http://127.0.0.1:<port>/cube.yaml` getter ref — Amendment 6: T9 proved `pack.fetchGetter` hardcodes `ClientModeDir`, so a plain YAML body over HTTP always fails.** Assert `cube.lock` lands in the test's CWD and a mutating command (`cube-idp pack install … -f <same-ref>` or `spoke add`) exits non-zero mentioning `CUBE-0014` (the read-only guard's post-p7 number — Amendment 5; `CUBE-0012` now belongs to p7's `CodeEngineTuningRemoved`).

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
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

COMMITS:
- `214ac42` docs: rv plan — claim T3
- `7a4cb13` refactor(compose): delegate to refval, surface providerConfig pin (RV1)
- `<this>` docs: rv plan — T3 complete

FINDINGS:
1. Plan Step 3's code sketch applied VERBATIM — `internal/cluster/compose/compose.go`
   is now the plan's three functions plus the (unchanged) package doc comment,
   with imports reduced to `context`, `fmt`, `diag`, `refval`. The dropped
   imports were exactly the ones the plan names (`encoding/json`, `jsonpatch`,
   `sigyaml`, `pack`).
2. CUBE-1005 preservation verified by the pre-existing tests, unchanged:
   `TestResolveFetchErrorWraps1005` and `TestResolveNonMappingDoc` both still
   assert `diag.CodeProviderConfigRefFetch` and both PASS. `refval.Resolve`
   passes pack-layer codes through unwrapped; compose's single `diag.Wrap` is
   the outermost error, so `errors.As(err, &de).Code == CUBE-1005` still holds
   for BOTH the fetch-failure path (was a pack `diag.Error` cause) and the
   non-mapping path (now a plain `fmt.Errorf` cause from refval instead of the
   old in-compose `json.Unmarshal` wrap). Observable code UNCHANGED; only the
   wrapped cause's text differs (no test or golden asserts it).
3. Only ONE user-facing string changed, exactly as the plan's Step 3 sketch
   dictates: the CUBE-1005 fix hint went from "the ref must resolve to one
   readable YAML file; …" to "the ref must resolve to one readable YAML
   mapping document; …". Grepped `readable YAML file` across the repo — no
   test, golden, or doc asserts it (`grep -rn "readable YAML file" .` → only
   the old compose.go line, now gone).
4. Line anchors held despite the p7 merge: `internal/cluster/kindp/merge.go:69`
   and `internal/cluster/k3dp/merge.go:66` were the EXACT `compose.Compose`
   call lines the plan names — p7 did not touch `internal/cluster/`. Both
   updated to `merged, _, err := compose.Compose(…)`. No anchor correction
   needed for this task.
5. The plan's Step 3 grep (`grep -rn "compose.Compose\|compose.Resolve"
   --include="*.go" internal/`) found NO qualified call sites in test files —
   the only other hits were two comments in `internal/cluster/kindp/merge_test.go`
   (lines 155, 168) and one in `internal/refval/refval.go:51`, none of which
   are call sites. The call sites needing the 3-value fix were the FIVE
   in-package (unqualified) ones in `internal/cluster/compose/compose_test.go`:
   `TestResolveEmptyRef`, `TestResolveLocalFile`, `TestResolveFetchErrorWraps1005`,
   `TestResolveNonMappingDoc`, `TestComposeRefPlusForProvider` — all changed to
   discard the pin (`m, _, err := …`), assertions otherwise untouched. This is
   the plan's "Fix compose/kindp/k3dp test call sites the same way" clause.
6. `refval.Merge`'s doc comment already read "Lifted verbatim from
   compose.Merge, which now delegates here" (written by T2 in anticipation);
   that sentence is TRUE as of this commit. No refval file was touched by T3.
7. Diag-code renumber rule: NOT applicable to T3 (allocates no codes; reuses
   the existing `CUBE-1005` / `diag.CodeProviderConfigRefFetch`).
8. Amendment 4 (tuningRef dropped): nothing in T3 references engine tuning —
   no action needed. Amendment 5's "re-verify anchors" instruction was
   honoured (finding 4).
9. Machine gotcha (T1 finding 4 / T2 finding 7) reconfirmed a THIRD time:
   commits required `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl"`.
   Nothing was written to any git config file. `go.mod`/`go.sum` unmodified
   (the task removes imports, adds only the in-repo `internal/refval`).

BLOCKERS: none

HANDOFF:
- `internal/cluster/compose` is now a THIN wrapper over `internal/refval`. Live
  signatures (all three exported symbols kept their names):
  - `func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error)`
    — delegates to `refval.Resolve`, wraps ANY error as `CUBE-1005`
    (`diag.CodeProviderConfigRefFetch`). Empty ref → `(empty non-nil map, "", nil)`
    (contract preserved, asserted by two tests).
  - `func Merge(base, patch map[string]any) (map[string]any, error)` — one-line
    delegation to `refval.Merge`. RFC 7386 behaviour is now single-sourced;
    `TestMergeVectors`' five vectors (deep-merge, list replace, null delete,
    empty patch, empty base) all still PASS against the refval implementation.
  - `func Compose(ctx context.Context, ref string, forProvider map[string]any, cacheDir string) (map[string]any, string, error)`
    — second return is the providerConfig pin, `""` when ref is empty. The pin
    is returned EVEN when `forProvider` is empty (early-return branch), so
    callers always get it.
- Pin forms reaching `Compose` callers are T1's verbatim: `oci:<digest>`,
  `git+<sha>`, `dir:<h1:…>`, `file:<sha256-hex>`.
- The pin is currently DISCARDED at both production call sites
  (`internal/cluster/kindp/merge.go:69`, `internal/cluster/k3dp/merge.go:66`,
  both `merged, _, err :=`). T11 (`ClusterLock.ProviderConfigRef/Pin`) is the
  task that plumbs it into `cube.lock` — it needs a path from
  `kindp/k3dp.Merge` (or a direct `compose.Compose` call in `up`) to the lock
  writer; nothing in T3 pre-builds that path.
- Evidence — Step 2 failed exactly as the plan's Expected ("compile error
  (2-value return)"):
  ```
  # github.com/cube-idp/cube-idp/internal/cluster/compose [.../compose.test]
  internal/cluster/compose/compose_test.go:92:17: assignment mismatch: 3 variables but Resolve returns 2 values
  internal/cluster/compose/compose_test.go:100:16: assignment mismatch: 3 variables but Resolve returns 2 values
  FAIL	github.com/cube-idp/cube-idp/internal/cluster/compose [build failed]
  ```
- Evidence — Step 4, `go build ./... && go test ./internal/cluster/... -count=1`:
  ```
  BUILD OK
  ok  	github.com/cube-idp/cube-idp/internal/cluster	1.494s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	2.347s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/k3dp	2.991s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/kindp	3.802s
  ```
- Evidence — every compose test, `go test ./internal/cluster/compose/ -count=1 -v`
  (7 top-level tests incl. the new one, ZERO failures):
  ```
  --- PASS: TestMergeVectors (0.00s)
  --- PASS: TestResolveEmptyRef (0.00s)
  --- PASS: TestResolveLocalFile (0.00s)
  --- PASS: TestResolveFetchErrorWraps1005 (0.00s)
  --- PASS: TestResolveNonMappingDoc (0.00s)
  --- PASS: TestResolveReturnsPin (0.00s)
  --- PASS: TestComposeRefPlusForProvider (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	0.983s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./... -count=1`:
  `BUILD OK`, `VET OK`, 32 `ok` packages, `grep -c "^FAIL"` → `0`. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	18.753s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	3.894s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/k3dp	6.671s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/kindp	2.217s
  ok  	github.com/cube-idp/cube-idp/internal/pack	15.376s
  ok  	github.com/cube-idp/cube-idp/internal/refval	12.983s
  ok  	github.com/cube-idp/cube-idp/internal/up	11.030s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	12.140s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden` with NO
  `-update` (`git status --short` empty afterwards — no golden rewritten):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.299s
  ```

### T4 — config surface valuesRef ONLY (tuningRef DROPPED, Amendment 4) [Task 4]
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

COMMITS:
- `f9a7441` docs: rv plan — claim T4
- `acd674c` feat(config): packs[].valuesRef field (RV2)
- `<this>` docs: rv plan — T4 complete

FINDINGS:
1. Amendment 4 honoured verbatim: `EngineSpec` was NOT touched, no
   `tuningRef?` was added to `schema.cue`, the test is `TestLoadValuesRef`
   (asserting `valuesRef` only), and the commit message is
   `feat(config): packs[].valuesRef field (RV2)`. Confirmed against the real
   post-p7 code that `engine.tuning` is gone: `grep -n "engine" internal/config/schema.cue`
   shows the engine block carries only `ref`/`values`/`selfManage` comments —
   no `tuning` key exists to anchor against.
2. NO anchor drift for this task despite the p7 merge. Both anchors verified
   against the real code before editing and both matched the plan exactly:
   - `internal/config/types.go:186` `type PackRef struct` with
     `Ref string \`yaml:"ref" json:"ref"\`` as its FIRST field — the plan's
     "inside `PackRef`, after `Ref`" anchor is valid, applied there.
   - `internal/config/schema.cue:41` read exactly
     `packs?: [...{ref: string & !="", values?: {...}, extraManifests?: string & !="", delivery?: "oci" | "repo", dependsOn?: [...string & !=""]}]`
     — the plan's sketch minus `valuesRef?`. `valuesRef?: string & !=""` was
     inserted after `ref`, producing the plan's Step 3 line verbatim.
     No correction to any plan text was required.
3. Test placement: `internal/config/load_test.go` needed NO new imports —
   `os`, `path/filepath`, `strings`, `testing` are already in its import
   block (lines 3-14), matching the file's existing temp-file conventions
   (`filepath.Join(t.TempDir(), "cube.yaml")` + `os.WriteFile` + `Load`).
   The test was appended at end of file (line 782+), the file's convention
   for the most recently added round-trip tests (the neighbouring
   `selfManage` omitempty test uses the identical
   `SaveValidated` → `os.ReadFile` → `strings.Contains` shape).
4. Step 2's Expected was observed in BOTH of its stated phases, in order:
   first the compile failure, then — after the types-only change — the CUE
   rejection. See the two evidence blocks below.
5. omitempty discipline (Global Constraints) verified by the round-trip half
   of the test, not merely by the struct tag: `SaveValidated` marshals with
   `sigs.k8s.io/yaml` and re-`Load`s the temp file through the full CUE
   pipeline, so an empty `ValuesRef` emitting `valuesRef: ""` would fail
   `schema.cue`'s `string & !=""` inside `SaveValidated` itself before the
   `strings.Contains` assertion ever ran. It passes — the key is absent.
6. Diag-code renumber rule: NOT applicable to T4 (allocates no codes).
   Amendment 5's fixed numbers (`CUBE-0014`/`CUBE-0015`, T8) are untouched.
7. `go.mod`/`go.sum` unmodified (no new module; the task adds one struct
   field, one CUE field, one test).
8. Machine gotcha (T1 finding 4 / T2 finding 7 / T3 finding 9) reconfirmed a
   FOURTH time: both commits required
   `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl"`. Nothing
   was written to any git config file.

BLOCKERS: none

HANDOFF:
- `config.PackRef.ValuesRef string` is live with tags
  `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"` — the exact Task 4
  "Interfaces" contract. It sits between `Ref` and `Values` in the struct.
- `schema.cue` now accepts `valuesRef?: string & !=""` on each `packs[]`
  entry. An explicit empty string is REJECTED by CUE (`CUBE-0002`), so
  consumers may treat `ValuesRef != ""` as "user set a ref" with no
  empty-string ambiguity.
- NOTHING reads the field yet — it is config surface only. T5
  (`pack.EffectiveValues`/`RenderResolved` + `CUBE-4021`) is the first
  consumer; T6 wires it into `up`/`diff`, records `valuesPin` in `cube.lock`,
  and adds the `CUBE-7007` bundle-rails clause for it. Until T6 lands, a user
  who sets `valuesRef` gets a CUE-valid, silently-ignored field — expected
  mid-plan state, closed by T5+T6.
- `EngineSpec` is untouched by this task (Amendment 4). Anyone looking for
  remote engine values wants the post-p7 `engine.valuesRef` plan, not this one.
- Evidence — Step 2 phase A, `go test ./internal/config/ -run TestLoadValuesRef -v`
  (compile failure, exactly the plan's Expected):
  ```
  # github.com/cube-idp/cube-idp/internal/config [.../internal/config.test]
  internal/config/load_test.go:804:28: c.Spec.Packs[0].ValuesRef undefined (type PackRef has no field or method ValuesRef)
  internal/config/load_test.go:808:18: c.Spec.Packs[0].ValuesRef undefined (type PackRef has no field or method ValuesRef)
  FAIL	github.com/cube-idp/cube-idp/internal/config [build failed]
  ```
- Evidence — Step 2 phase B, after the types-only fix, before the CUE edit
  (the plan's "then after types-only fix, CUE rejection `valuesRef: field not
  allowed`"):
  ```
  --- FAIL: TestLoadValuesRef (0.00s)
      load_test.go:802: CUBE-0002: /var/folders/.../cube.yaml failed validation: #Cube.spec.packs.0.valuesRef: field not allowed
  FAIL	github.com/cube-idp/cube-idp/internal/config	1.130s
  ```
- Evidence — Step 4, `go test ./internal/config/ -count=1` and the new test
  verbose:
  ```
  ok  	github.com/cube-idp/cube-idp/internal/config	0.830s
  === RUN   TestLoadValuesRef
  --- PASS: TestLoadValuesRef (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/config	0.407s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./... -count=1`:
  `BUILD OK`, `VET OK`, 32 `ok` packages, ZERO `FAIL` lines. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	13.352s
  ok  	github.com/cube-idp/cube-idp/internal/config	7.485s
  ok  	github.com/cube-idp/cube-idp/internal/lock	11.287s
  ok  	github.com/cube-idp/cube-idp/internal/pack	15.990s
  ok  	github.com/cube-idp/cube-idp/internal/refval	12.568s
  ok  	github.com/cube-idp/cube-idp/internal/up	11.247s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	10.853s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	12.541s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden -v` with
  NO `-update` (no flags changed by this task; `git status --porcelain`
  afterwards listed only the three intended source files, no golden rewritten):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.215s
  ```

### T5 — pack.EffectiveValues + RenderResolved + CUBE-4021 [Task 5]
STATUS: DONE
Outcome: COMMITS · FINDINGS (actual diag number if renumbered) · BLOCKERS · HANDOFF:

COMMITS:
- `eff7d80` docs: rv plan — claim T5
- `001a26d` feat(pack): EffectiveValues + RenderResolved — valuesRef fetch/merge, CUBE-4021 (RV2)
- `<this>` docs: rv plan — T5 complete

FINDINGS:

1. **ACTUAL DIAG NUMBER USED: `CUBE-4021` = `CodePackValuesRefFetch` — the
   planned number, NO renumber.** Re-verified against the real post-p7
   `internal/diag/codes.go` before allocating: the 4xxx block's highest
   allocated code was `CUBE-4020` (`CodePackDepGateway`, codes.go:114), so
   4021 was the next free number in the domain block. Both halves added per
   Global Constraints — the constant in `internal/diag/codes.go` (after
   `CodePackDepGateway`, with the plan's `// Remote values (spec 2026-07-19
   §5.1, §8).` comment) AND the `Desc` entry in `internal/diag/registry.go`
   (after the `CodePackDepGateway` line). `TestRegistryCoversEveryDeclaredCode`
   passes in both directions.

2. **PLAN-vs-REALITY CORRECTION (escape hatch, the one substantive deviation):
   Step 4's code sketch imports `internal/refval` from `package pack` — that
   is an IMPORT CYCLE and cannot compile.** `internal/refval` imports
   `internal/pack` (for `pack.FetchFile`, refval.go:19/31 — T2's shipped
   design), so `pack → refval` closes the loop. Verified empirically, not by
   inference: the plan's verbatim Step 4 file was written to disk first and
   `go build ./internal/pack/` produced

   ```
   package github.com/cube-idp/cube-idp/internal/pack
   	imports github.com/cube-idp/cube-idp/internal/refval from values.go
   	imports github.com/cube-idp/cube-idp/internal/pack from refval.go: import cycle not allowed
   ```

   and `go list -deps ./internal/refval | grep cube-idp` confirms the edge:
   `internal/diag`, `internal/apply`, `internal/config`, **`internal/pack`**,
   `internal/refval`.

   **Minimal correction applied** — the deviation is confined to Step 4's
   IMPORT LIST; every normative contract the plan states elsewhere is
   preserved unchanged: the file is still `internal/pack/values.go`, the test
   is still `internal/pack/values_test.go` (`package pack`), the symbols are
   still `pack.EffectiveValues` / `pack.RenderResolved` with the exact
   signatures in Task 5's "Interfaces", and Task 6's `Consumes:
   pack.RenderResolved` line needs NO change. Concretely, `values.go` drops
   the `refval` import and instead:
   - fetches through the **in-package `FetchFile`** — which is the shared
     machinery design G5 actually names ("the same grammar, cache, auth, and
     guard machinery"): `refval.Resolve` is itself only a decode wrapper
     around this very function, so nothing is bypassed;
   - merges through the **same `jsonpatch.MergePatch` primitive**
     `refval.Merge` and `compose.Merge` ride, so spec §5.1's "the identical
     algorithm and precedence direction as `forProvider` over
     `providerConfigRef`" holds by construction;
   - mirrors `refval.NormalizeIntegral` as the unexported `normalizeIntegral`
     (identical bounds `[MinInt32, MaxInt32]`, identical in-place recursion
     over `map[string]any`/`[]any`). This ~18-line mirror is the ONLY genuine
     duplication introduced.

   The two private helpers `resolveValuesDoc` and `mergeValuesPatch` are
   `refval.Resolve`/`refval.Merge`'s bodies with the in-package call. A
   package-file comment in `values.go` records the cycle and the reason so a
   later reader does not "fix" it back into a cycle.

   Precedent for choosing relocation-of-import over relocation-of-package:
   the plan's own Global Constraints "Import direction" bullet resolves the
   `config`/`pack` direction problem by moving code and keeping "the same
   observable contract"; here the observable contract is kept by moving only
   the import, which changes strictly fewer plan-normative facts (no new
   package, no File Structure change, no downstream interface rename).

3. **`diag.HasCode` does not exist** — the plan's Step 2 sketch anticipated
   this ("If `diag.HasCode` doesn't exist, check `internal/diag/diag.go` for
   the code-inspection helper the existing tests use … do NOT invent a new
   helper"). `internal/diag/diag.go` exports only `New`/`Wrap`/`Error`/
   `Unwrap`/`Render`; the repo-wide test convention is
   `var de *diag.Error; errors.As(err, &de) && de.Code == …` (20+ sites, e.g.
   `internal/pack/pack_test.go:37`, `source_test.go:105`). Both code
   assertions in `values_test.go` use that form. No helper was invented.

4. **Chartless fixture: used the repo's existing one instead of the sketch's
   temp-dir pack**, per Step 2's own instruction ("copy the minimal fixture
   pattern from an existing test in `internal/pack/` … rather than the sketch
   above if it differs"). `internal/pack/testdata/demo` IS the repo's
   chartless fixture (`pack.cue` + `manifests/cm.yaml`, no `chart.yaml`) and
   is exactly what `TestRenderWithValuesOnChartlessPackIsCube4016`
   (`render_test.go:159`) uses for the inline-values half of the same GT15
   stone — so the `valuesRef` half now sits beside the `values` half on the
   same fixture. No new testdata directory was created.

5. **GT15 (values stone) preserved, not weakened or relocated.**
   `RenderResolved` adds the `valuesRef` clause as a NEW guard in front of
   `RenderWith`, checked BEFORE any fetch (`pref.ValuesRef != "" &&
   !pk.HasChart()` → `CUBE-4016`); `RenderWith`'s existing
   `len(values) > 0 && !p.HasChart()` guard in `internal/pack/render.go:130`
   is untouched, so the inline-values path is byte-identical to pre-T5. Note
   the guards are complementary, not redundant: `RenderWith` alone would
   MISS a chartless pack whose only values come from a ref, because
   `EffectiveValues` would have to fetch first to produce a non-empty map —
   which is precisely why the plan puts this check before the fetch.

6. **Anchor drift: NONE for this task.** Both anchors verified against the
   real post-p7 code before editing and both matched: `codes.go`'s 4xxx block
   ends at `CodePackDepGateway` (line 114) as the plan's "after
   `CodePackDepGateway`" says, and `registry.go`'s 4xxx section ends at the
   matching `CodePackDepGateway` entry (line 115). `internal/pack/values.go`
   did not exist. p7 touched `internal/pack/helm.go` and added
   `enginepack.go`, neither of which this task reads or writes.

7. **Amendment 4 honoured:** nothing in this task references `engine.tuningRef`
   or `EngineSpec`; `CUBE-3012` was NOT allocated. Amendment 5's fixed
   `CUBE-0014`/`CUBE-0015` (T8) are untouched.

8. `go.mod`/`go.sum` unmodified — `github.com/evanphx/json-patch/v5` and
   `sigs.k8s.io/yaml` are already DIRECT requirements (T2 finding 4), and
   `sigs.k8s.io/yaml` was already imported inside `internal/pack` itself
   (`helm.go:36`). No new module in any form.

9. **Machine gotcha reconfirmed a FIFTH time** (T1 f4 / T2 f7 / T3 f9 /
   T4 f8): this checkout has NO git identity, so both commits were made as
   `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl" commit …`.
   Nothing was written to any git config file.

BLOCKERS: none

HANDOFF:

- **`pack.EffectiveValues(ctx context.Context, valuesRef string, inline map[string]any, cacheDir string) (map[string]any, string, error)`**
  is live, exactly the Task 5 "Interfaces" signature.
  - `valuesRef == ""` → returns `(inline, "", nil)` — the SAME map header the
    caller passed, untouched and un-normalized, no fetch, no allocation. A
    nil `inline` comes back nil.
  - Otherwise: fetch → decode → RFC 7386 merge (inline patched OVER fetched;
    `null` deletes, arrays replace wholesale) → `normalizeIntegral`. Returns
    the values pin in T1's forms: `oci:<digest>`, `git+<sha>`, `dir:<h1:…>`,
    `file:<sha256-hex>`.
  - **Numbers in the returned map are `int`, not `float64`** — the
    Helm-path-only normalization step is applied HERE, so T6 must not apply
    it again. Non-integral and out-of-int32-range numbers stay `float64`.
  - EVERY failure (fetch, non-mapping document, merge) is wrapped
    `CUBE-4021` / `diag.CodePackValuesRefFetch` as the OUTERMOST code, with
    the pack-layer cause (`CUBE-4006`/`CUBE-4001`/`CUBE-4007`/…) preserved
    underneath via `Unwrap`, so `errors.As` finds 4021 first.
- **`pack.RenderResolved(ctx context.Context, pk *Pack, pref config.PackRef, gw config.GatewaySpec, cacheDir string) (*Rendered, string, error)`**
  is live — the single render entry T6 wires into BOTH `up` and `diff`.
  - Order is: GT15 chartless guard (`CUBE-4016`, BEFORE any network) →
    `EffectiveValues` (`CUBE-4021`) → `pk.RenderWith(values,
    pref.ExtraManifests, gw)` (`CUBE-4002`/`4005`/`4016`/`4017` unchanged).
  - It is a **pure pass-through to `RenderWith` when `pref.ValuesRef == ""`**
    — same values map, same errors, same rendered objects. T6 can therefore
    route EVERY pack through it unconditionally, as the plan says, with no
    behaviour change for inline-only packs.
  - Second return is the values pin, `""` for inline-only packs — feeds
    `lock.Entry.ValuesPin` in T6 (and `ValuesRef: pref.ValuesRef` is set
    unconditionally; `omitempty` drops it when empty).
- **For T6 specifically:** the plan's Task 6 "Consumes: `pack.RenderResolved`"
  is CORRECT AS WRITTEN — finding 2's correction did not move the symbol out
  of `package pack`. `internal/up/up.go` and `internal/diff/diff.go` already
  import `internal/pack`, so no new import is needed in either file; the call
  is `pack.RenderResolved(ctx, pk, pref, cube.Spec.Gateway, <cacheDir var>)`.
  Do NOT add an `internal/refval` import to `up`/`diff` for values — it is
  not needed and `refval` is not on the values path any more.
- **For anyone touching `internal/pack` later:** `package pack` MUST NOT
  import `internal/refval` (cycle — finding 2). If a future task needs
  refval's exported helpers inside `pack`, the fix is to make `refval` a leaf
  (move the `FetchFile` call out of `refval.Resolve`), which is an
  owner-level change to T2's shipped contract, not a mid-task correction.
- `internal/refval`, `internal/cluster/compose`, `internal/config`,
  `internal/lock`, `internal/up`, `internal/diff` are all UNTOUCHED by T5.
  `valuesRef` is still silently ignored end-to-end until T6 wires it — T5
  builds the mechanism, T6 puts it on the path.

- Evidence — CUBE-4021 was free before allocation (`grep -n "CUBE-40"
  internal/diag/codes.go | tail -3`, pre-edit):
  ```
  112:	CodePackDepUnknown Code = "CUBE-4018" // dependsOn names a pack not in this cube
  113:	CodePackDepCycle   Code = "CUBE-4019" // pack dependency cycle (the message shows the path)
  114:	CodePackDepGateway Code = "CUBE-4020" // gateway pack cannot carry a dependsOn of its own
  ```
- Evidence — Step 1, `go test ./internal/diag/ -count=1` (registry coverage
  holds in both directions with the new code):
  ```
  ok  	github.com/cube-idp/cube-idp/internal/diag	0.809s
  ```
- Evidence — Step 3 failed exactly as the plan's Expected (`undefined:
  EffectiveValues`, `undefined: RenderResolved`):
  ```
  # github.com/cube-idp/cube-idp/internal/pack [github.com/cube-idp/cube-idp/internal/pack.test]
  internal/pack/values_test.go:17:19: undefined: EffectiveValues
  internal/pack/values_test.go:32:19: undefined: EffectiveValues
  internal/pack/values_test.go:46:15: undefined: EffectiveValues
  internal/pack/values_test.go:64:14: undefined: RenderResolved
  FAIL	github.com/cube-idp/cube-idp/internal/pack [build failed]
  ```
- Evidence — Step 4, the plan's verbatim sketch written first, `go build
  ./internal/pack/` (the import cycle of finding 2):
  ```
  package github.com/cube-idp/cube-idp/internal/pack
  	imports github.com/cube-idp/cube-idp/internal/refval from values.go
  	imports github.com/cube-idp/cube-idp/internal/pack from refval.go: import cycle not allowed
  ```
- Evidence — Step 5, the four new tests after the correction (`go test
  ./internal/pack/ -run 'TestEffectiveValues|TestRenderResolved' -v -count=1`):
  ```
  === RUN   TestEffectiveValuesNoRefPassesInlineThrough
  --- PASS: TestEffectiveValuesNoRefPassesInlineThrough (0.00s)
  === RUN   TestEffectiveValuesMergesInlineOverFetched
  --- PASS: TestEffectiveValuesMergesInlineOverFetched (0.00s)
  === RUN   TestEffectiveValuesWrapsFetchFailure
  --- PASS: TestEffectiveValuesWrapsFetchFailure (0.00s)
  === RUN   TestRenderResolvedChartlessValuesRef
  --- PASS: TestRenderResolvedChartlessValuesRef (0.00s)
  PASS
  ok  	github.com/cube-idp/cube-idp/internal/pack	1.527s
  ```
  (`TestEffectiveValuesMergesInlineOverFetched` is the ladder proof: fetched
  `replicas: 1, image.tag: v1, extra.a: 1` ⊕ inline `replicas: 3, extra: nil`
  → `map[string]any{"replicas": 3, "image": map[string]any{"tag": "v1"}}` —
  override wins, nested key survives, `null` deleted the subtree, and
  `replicas` is `int(3)` not `float64(3)`, so `reflect.DeepEqual` against the
  int-typed want map passes only if normalization ran.)
- Evidence — Step 5 package-level, `go test ./internal/pack/ ./internal/diag/ -count=1`:
  ```
  ok  	github.com/cube-idp/cube-idp/internal/pack	4.806s
  ok  	github.com/cube-idp/cube-idp/internal/diag	0.347s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./... -count=1`:
  `BUILD OK`, `VET OK`, 32 `ok` packages, `grep -c "^FAIL"` → `0`. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	16.020s
  ok  	github.com/cube-idp/cube-idp/internal/cluster/compose	4.515s
  ok  	github.com/cube-idp/cube-idp/internal/config	3.506s
  ok  	github.com/cube-idp/cube-idp/internal/diag	6.511s
  ok  	github.com/cube-idp/cube-idp/internal/diff	11.945s
  ok  	github.com/cube-idp/cube-idp/internal/lock	10.873s
  ok  	github.com/cube-idp/cube-idp/internal/pack	16.007s
  ok  	github.com/cube-idp/cube-idp/internal/refval	11.635s
  ok  	github.com/cube-idp/cube-idp/internal/up	10.856s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	10.557s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	11.901s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden -v
  -count=1` with NO `-update` (this task changes no flags; `git status
  --short` afterwards listed ONLY the four intended files —
  `M internal/diag/codes.go`, `M internal/diag/registry.go`,
  `?? internal/pack/values.go`, `?? internal/pack/values_test.go` — no golden
  rewritten, no stray staged file):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  PASS
  ok  	github.com/cube-idp/cube-idp/cmd	1.235s
  ```

### T6 — up/diff wiring + lock valuesPin + bundle guard CUBE-7007 [Task 6 incl. Step 3b]
STATUS: DONE
Outcome: COMMITS · FINDINGS (actual diag number if renumbered) · BLOCKERS · HANDOFF:

COMMITS:
- `00591b3` docs: rv plan — claim T6
- `cd2fd8f` feat(up,diff): render via RenderResolved — valuesRef wired, valuesPin in cube.lock (RV2)
- `<this>` docs: rv plan — T6 complete

FINDINGS:

1. **ACTUAL DIAG NUMBER USED: `CUBE-7007` = `CodeBundleRemoteSource`** — the
   planned number, NOT renumbered. Verified free pre-edit against the real
   post-p7 catalog (`grep -n "CUBE-70" internal/diag/codes.go`): the 70xx
   block ended at `CodeBundleImageLoadFail Code = "CUBE-7006"`. Both halves
   landed per the Global Constraint — constant in `internal/diag/codes.go`
   (70xx block, after `CodeBundleImageLoadFail`) AND `Desc` entry in
   `internal/diag/registry.go` (70xx section, same position);
   `go test ./internal/diag/ -count=1` → `ok` (registry coverage holds both
   directions).

2. **Anchor drift (p7 rewrite), `internal/up/up.go` — corrected, not blocked.**
   The plan's "pass-1 loop, ~lines 356-383" and "~line 509" are stale. Real
   post-p7 anchors located by structure and used:
   - `pk.RenderWith(pref.Values, pref.ExtraManifests, cube.Spec.Gateway)` at
     **line 378** (pass-1 fetch/render loop, inside the per-pack closure) →
     replaced with
     `pack.RenderResolved(ctx, pk, pref, cube.Spec.Gateway, dir)` capturing
     `valuesPin`. The cache-dir variable at that call site is `dir` exactly as
     the plan predicted.
   - `entries = append(entries, lock.Entry{…})` at **line 386** → gained
     `ValuesRef: pref.ValuesRef` + `ValuesPin: valuesPin`.
   - the D11 `customized` expression at **line 538** (plan said ~509) →
     `len(refs[i].Values) > 0 || refs[i].ValuesRef != "" || refs[i].ExtraManifests != ""`.
   No new import needed (`internal/pack`, `internal/diag`, `fmt` already
   imported), exactly as T5's HANDOFF predicted.

3. **Anchor drift, `internal/diff/diff.go` — corrected.** The plan's
   "`desiredState` pack loop, ~lines 228-240" is stale: post-p7 the loop sits
   at **lines 228-247** (p7 inserted the `FetchRenderEngine` block above it).
   `p.RenderWith(pr.Values, pr.ExtraManifests, cube.Spec.Gateway)` at line 237
   → `pack.RenderResolved(ctx, p, pr, cube.Spec.Gateway, dir)`; the entry
   append at line 245 gained `ValuesRef: pr.ValuesRef, ValuesPin: valuesPin`.
   `dir` is `pack.DefaultCacheDir()` from line 191, already in scope.

4. **Anchor drift, `internal/lock/lock.go` — corrected.** The plan's "Entry
   struct, ~line 30" is stale: post-p7 `EngineLock` (which p7 widened with
   `Ref/Name/Version/Resolved/RenderedHash/Images` + an `Entry()` projection)
   occupies lines 27-42 and `Entry` now starts at **line 45**. The two new
   fields went in after `RenderedHash` (line 49), before the `Images` comment
   block, exactly as the plan's "add to `Entry` after `RenderedHash`" says.
   Per Amendment 4, NO `TuningRef`/`TuningPin` was added to `EngineLock`.

5. **Test correction — diff lock entries carry no `Ref` (plan sketch bug).**
   The plan's `TestDesiredStateValuesRef` looks the entry up with
   `entries[i].Ref == cube.Spec.Packs[0].Ref`, but `desiredState` constructs
   `lock.Entry{Name: rendered.Name, RenderedHash: rh}` — `Ref` is deliberately
   never set there (diff's entries exist only for content-drift comparison by
   name). Minimal correction: the test looks the entry up **by rendered name**
   (`e.Name == "vr-pack"`). `Ref` was NOT added to diff's entry construction —
   that is a behavior change the plan does not specify.

6. **Test correction — fixture.** `TestDesiredStateMatchesUpAppliedSet`'s cube
   uses `../pack/testdata/demo-kustomize`, which is CHARTLESS: a `valuesRef` on
   it correctly fails `CUBE-4016`, so it cannot carry this test (the plan's own
   parenthetical anticipates this: "If the fixture pack is chartless, point
   `Packs[0]` at the chart-bearing fixture … or add `chart.yaml`"). The only
   chart-bearing fixtures are `demo-helm` (pulls a chart from ghcr.io —
   network) and `gw-sub-pack` (whose `chart:` path is relative to
   `internal/pack`, unusable from `internal/diff`). So a new network-free
   helper `writeValuesChartPack(t)` materializes a local helm chart + pack in
   `t.TempDir()` (absolute `chart:` path), mirroring the file's existing
   `writeEngineFixture(t)` convention. Everything else in the cube fixture
   (engine fixture, gateway `../pack/testdata/demo`, `fakeEngine{}`) is reused
   verbatim from `TestDesiredStateMatchesUpAppliedSet`.

7. **Test strengthening within the plan's stated contract.** The plan's test
   docstring promises "fetched base + inline override visible in the rendered
   objects, pin recorded in the lock entries", but `desiredState`'s `desired`
   set contains the ENGINE's delivery objects (fakeEngine's OCIRepository/
   Kustomization), never the pack's rendered manifests — so rendered content is
   not observable there. Instead the test runs `desiredState` twice on the same
   cube (with and without `valuesRef`) and asserts the pack's `RenderedHash`
   DIFFERS, which is the observable proof the fetched base reached the render;
   it also asserts the ref-less run leaves `ValuesRef`/`ValuesPin` empty. No
   new assertion outside the test the plan specifies.

8. **`bundleRailsCheck` call site — merged into the existing bundle block.**
   The plan's sketch adds a fresh `if opts.Bundle != "" { … }`; the real
   `Run` already has one (`internal/up/up.go:110-120`) that opens + verifies
   the bundle. The check was placed INSIDE it, immediately after the
   `con.Step("bundle", "bundle verified …")` line — i.e. exactly the plan's
   "right after the bundle is opened/verified and before any cluster mutation",
   and still ahead of the `CUBE-7005` provider check (line ~156), the CA, and
   `prov.Ensure`. A duplicate `if` would have been dead structure. The helper
   itself lives in `internal/up/up.go` (the plan's stated file), just above
   `resolveAndDeliverPacks`.

9. **Amendment 2 + Amendment 4 honoured — valuesRef clause ONLY.**
   `bundleRailsCheck` has exactly one clause (`packs[].valuesRef`). No
   `tuningRef` clause (Amendment 4 dropped `engine.tuningRef`;
   `config.EngineSpec` has no `TuningRef` field — verified: `grep -n
   "TuningRef" internal/config/types.go` → no match), and no remote-`-f`
   origin clause (that is T10's Step 2b). `CUBE-3012` remains unallocated.

10. **Observation for T10/T12 (no change made):** `resolveBundleRefs`
    (`internal/up/bundle.go:39`) rebuilds each `config.PackRef` field-by-field
    (`Ref/Values/ExtraManifests/Delivery`) and therefore DROPS `ValuesRef` in
    bundle mode. That is now unreachable — `bundleRailsCheck` rejects any
    `valuesRef` before `resolveBundleRefs` runs — so it is correct as-is and
    was deliberately left untouched (the plan specifies no change there).

11. **T5's HANDOFF held exactly.** `pack.RenderResolved` is a pure pass-through
    when `ValuesRef == ""` (every pack, gateway included, is routed through it
    unconditionally — no call-site branching); its second return feeds
    `lock.Entry.ValuesPin`; values arrive already int-normalized (no second
    normalization anywhere in `up`/`diff`); the GT15/`CUBE-4016` guard still
    fires before any fetch. `internal/refval` was NOT added to `up`/`diff`.

12. **`go.mod`/`go.sum` unmodified** — no new module in any form; every symbol
    used (`pack`, `lock`, `diag`, `fmt`) was already imported in the touched
    files.

13. **Machine gotcha reconfirmed a SIXTH time** (T1 f4 / T2 f7 / T3 f9 / T4 f8
    / T5 f9): this checkout has NO git identity, so both commits were made as
    `git -c user.name="Rafal P" -c user.email="rafal@pieniazek.nl" commit …`.
    Nothing was written to any git config file. Commits used explicit
    pathspecs; `git status --short` before the code commit listed ONLY the
    eight intended files.

14. **Commit pathspec widened by two files vs. the plan's Step 5 list.** Step 5
    lists `internal/lock/ internal/up/up.go internal/diff/`; the actual
    pathspec adds `internal/up/up_test.go` (Step 3b's own test — the package
    does not build without it) and `internal/diag/codes.go` +
    `internal/diag/registry.go`, which Step 3b explicitly instructs to include
    in this task's commit. No other file touched;
    `docs/superpowers/plans/…-agent-prompt.md` never staged.

BLOCKERS: none

HANDOFF:

- **`lock.Entry` now carries `ValuesRef` + `ValuesPin`** (both
  `yaml:",omitempty" json:",omitempty"`, placed after `RenderedHash`). Ref-less
  locks are byte-identical to pre-RV2 output. `EngineLock` is UNCHANGED
  (Amendment 4 — no `TuningRef`/`TuningPin`); T11's `ClusterLock` section is
  still unwritten.
- **`up.Run` pass-1 renders EVERY pack (gateway included) through
  `pack.RenderResolved(ctx, pk, pref, cube.Spec.Gateway, dir)`** and records
  `ValuesRef: pref.ValuesRef` (unconditionally; omitempty drops it) +
  `ValuesPin: valuesPin` in its `lock.Entry`. `internal/up/up.go` line numbers
  after this task: RenderResolved call ~line 382, entry literal ~line 390,
  `bundleRailsCheck` definition ~line 605, its call site ~line 120.
- **`diff.desiredState` mirrors it**: same `RenderResolved` call, and its
  `lock.Entry{Name, RenderedHash}` gained `ValuesRef`/`ValuesPin`. Its entries
  still carry NO `Ref` — anything downstream that wants to key diff entries by
  ref must use `Name` (T11 take note).
- **D11 `customized` now includes `ValuesRef != ""`** — a remotely-valued pack
  shows CUSTOMIZED in `pack ls`/the Pack record. No golden or column change
  (DEP4 output surface untouched; `TestCommandTreeGolden` passes without
  `-update`).
- **`bundleRailsCheck(cube *config.Cube) error`** is live in `internal/up`
  (unexported, pure, no cluster/bundle needed to call). It currently has ONE
  clause — `packs[].valuesRef` → `CUBE-7007` /
  `diag.CodeBundleRemoteSource`. **T10's Step 2b extends this same helper**
  with the remote-`-f` origin clause: add a clause to the existing function,
  do not create a second one; the call site (inside `Run`'s
  `if opts.Bundle != ""` block, right after `con.Step("bundle", "bundle
  verified …")`) already covers it. `CodeBundleRemoteSource`'s summary text
  deliberately reads "values/tuning/config source" so the later clause needs
  no registry edit.
- **`CUBE-7007` is now TAKEN.** The 70xx block's next free number is
  `CUBE-7008`. `CUBE-0014`/`CUBE-0015` (T8, Amendment 5) and `CUBE-4021` (T5)
  are unaffected; `CUBE-3012` remains unallocated (T7 GATED_SKIP).
- End-to-end status: `packs[].valuesRef` is now LIVE — config → fetch/merge →
  render → `cube.lock` — for both `up` and `diff`. What is still missing after
  T6: `upgrade --plan` attribution of values-pin drift (T11), the
  `ClusterLock` providerConfig pin (T11), remote `-f` (T8-T10), and docs/e2e
  (T12).

- Evidence — `CUBE-7007` free before allocation (`grep -n "CUBE-70"
  internal/diag/codes.go`, pre-edit tail):
  ```
  144:	CodeBundleNoImageLoader Code = "CUBE-7005" // `up --bundle` needs a provider that node-loads images (kind/k3d); `existing` cannot
  145:	CodeBundleImageLoadFail Code = "CUBE-7006" // bundled image load into cluster nodes failed (kind/k3d LoadImages, consume side)
  ```
- Evidence — Step 3b, `go test ./internal/diag/ -count=1` after adding the
  constant + registry entry:
  ```
  ok  	github.com/cube-idp/cube-idp/internal/diag	1.123s
  ```
- Evidence — Step 2, all three tests failed FIRST exactly as the plan's
  Expected (compile errors, `e.ValuesRef undefined` / `undefined:
  bundleRailsCheck`):
  ```
  # github.com/cube-idp/cube-idp/internal/lock [github.com/cube-idp/cube-idp/internal/lock.test]
  internal/lock/lock_test.go:144:13: f.Packs[0].ValuesRef undefined (type Entry has no field or method ValuesRef)
  internal/lock/lock_test.go:144:35: f.Packs[0].ValuesPin undefined (type Entry has no field or method ValuesPin)
  # github.com/cube-idp/cube-idp/internal/diff [github.com/cube-idp/cube-idp/internal/diff.test]
  internal/diff/diff_test.go:484:7: e.ValuesRef undefined (type *lock.Entry has no field or method ValuesRef)
  internal/diff/diff_test.go:487:10: base.ValuesRef undefined (type *lock.Entry has no field or method ValuesRef)
  # github.com/cube-idp/cube-idp/internal/up [github.com/cube-idp/cube-idp/internal/up.test]
  internal/up/up_test.go:1062:9: undefined: bundleRailsCheck
  internal/up/up_test.go:1064:45: undefined: diag.CodeBundleRemoteSource
  FAIL	github.com/cube-idp/cube-idp/internal/up [build failed]
  ```
- Evidence — Step 4, the three new tests after implementing (`go test
  ./internal/lock/ ./internal/diff/ ./internal/up/ -run
  'TestLockEntryValuesFields|TestDesiredStateValuesRef|TestBundleRailsCheck'
  -v -count=1`):
  ```
  === RUN   TestLockEntryValuesFieldsOmitEmpty
  --- PASS: TestLockEntryValuesFieldsOmitEmpty (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/lock	0.598s
  === RUN   TestDesiredStateValuesRef
  --- PASS: TestDesiredStateValuesRef (0.01s)
  ok  	github.com/cube-idp/cube-idp/internal/diff	1.284s
  === RUN   TestBundleRailsCheckRejectsValuesRef
  --- PASS: TestBundleRailsCheckRejectsValuesRef (0.00s)
  ok  	github.com/cube-idp/cube-idp/internal/up	1.940s
  ```
  (`TestDesiredStateValuesRef` is the wiring proof: the SAME cube rendered with
  and without `valuesRef` yields DIFFERENT `RenderedHash` — so the fetched
  base `replicas: 2` really reached the helm render over the chart default
  `1` — while the inline `message: inline` still wins on top, and the entry
  carries `ValuesRef: <path>` + `ValuesPin: file:<sha256>` where the ref-less
  run carries neither.)
- Evidence — Step 4 package-level (`go test ./internal/lock/ ./internal/diff/
  ./internal/up/ -count=1`), no pre-existing test disturbed:
  ```
  ok  	github.com/cube-idp/cube-idp/internal/lock	0.329s
  ok  	github.com/cube-idp/cube-idp/internal/diff	3.369s
  ok  	github.com/cube-idp/cube-idp/internal/up	2.107s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./...
  -count=1`: `BUILD OK`, `VET OK`, 32 `ok` packages, `grep -c "^FAIL"` → `0`.
  Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	12.941s
  ok  	github.com/cube-idp/cube-idp/internal/config	8.026s
  ok  	github.com/cube-idp/cube-idp/internal/diag	8.242s
  ok  	github.com/cube-idp/cube-idp/internal/diff	11.758s
  ok  	github.com/cube-idp/cube-idp/internal/lock	10.639s
  ok  	github.com/cube-idp/cube-idp/internal/pack	14.375s
  ok  	github.com/cube-idp/cube-idp/internal/refval	11.560s
  ok  	github.com/cube-idp/cube-idp/internal/up	10.677s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	10.285s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	11.974s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden -v
  -count=1` with NO `-update` (this task changes no flags), and `git status
  --short` immediately afterwards listing ONLY the eight intended files (no
  golden rewritten, no stray staged file):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.246s

   M internal/diag/codes.go
   M internal/diag/registry.go
   M internal/diff/diff.go
   M internal/diff/diff_test.go
   M internal/lock/lock.go
   M internal/lock/lock_test.go
   M internal/up/up.go
   M internal/up/up_test.go
  ```

### T7 — engine.tuningRef [Task 7 — DO NOT CLAIM]
STATUS: GATED_SKIP (engine-as-pack RATIFIED 017057a; replacement engine.valuesRef planned post-p7)
Outcome: n/a

### T8 — config.LoadBytes + origin + SaveValidated guard [Task 8]
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF (numbers T9/T10/T12 must use). NUMBERS ARE FIXED BY AMENDMENT 5: `CUBE-0014` = `CodeConfigRemoteReadOnly`, `CUBE-0015` = `CodeConfigRemoteFetch` — do not re-derive:

COMMITS:
- `489c67a` docs: rv plan — claim T8
- `3346cba` feat(config): LoadBytes split + Cube origin + SaveValidated remote guard CUBE-0014 (RV4)
- `<this>` docs: rv plan — T8 complete

FINDINGS:
1. **DIAG NUMBERS ACTUALLY USED (Amendment 5, applied verbatim — NOT
   re-derived):** `CodeConfigRemoteReadOnly Code = "CUBE-0014"` and
   `CodeConfigRemoteFetch Code = "CUBE-0015"`. Verified before allocating
   that the 0xxx block's highest taken code was `CUBE-0013`
   (`CodeEnginePackMismatch`, from p7):
   ```
   16:	CodeProviderConfigRemoved Code = "CUBE-0011"
   17:	CodeEngineTuningRemoved   Code = "CUBE-0012"
   18:	CodeEnginePackMismatch    Code = "CUBE-0013"
   ```
   The Task 8 body's Step-1 sketch still shows `CUBE-0012`/`CUBE-0013` —
   Amendment 5 overrides it; only the CONSTANT NAMES were taken from the
   body. The plan body was NOT rewritten (out of this task's file set);
   this FINDINGS entry is the record.
2. Anchor drift (expected, escape hatch used): the plan says to insert the
   codes "after `CodeProviderConfigRemoved`". p7 had already appended
   `CUBE-0012`/`CUBE-0013` there, so the two new constants were appended
   after `CodeEnginePackMismatch` at the END of the 0xxx const block
   (registry entries likewise after the `CodeEnginePackMismatch` line).
   Same block, minimal correction.
3. `diag.HasCode` does NOT exist in this repo (the plan's sketches warn
   about this). `internal/config/load_test.go` already carries the repo
   idiom as a local helper — `codeOf(t, err) diag.Code` built on
   `errors.As(err, &de)` (load_test.go:16-23) — and the new test uses it.
   No new helper invented.
4. `internal/config/load.go` drift from p7: `Load` now carries p7's
   `engine.tuning` migration guard (the `legacyTuning` probe →
   `diag.CodeEngineTuningRemoved`). It moved into `LoadBytes` UNCHANGED
   along with the rest of the pipeline — the split is purely `os.ReadFile`
   staying in `Load`, everything from `var doc map[string]any` onward
   moving to `LoadBytes`, with the three `fmt.Sprintf("…%s…", path)` error
   labels below the read (`is not valid YAML`, two × `failed validation`)
   re-pointed at `src`. p7's guard is untouched and still covered by the
   pre-existing `TestLoad…TuningRemoved` test (load_test.go:387).
5. Import direction respected: `internal/config` gained NO new imports at
   all — not `pack`, not `refval`. The remote fetch stays T9's `cfgload`
   job. `go build ./...` + `go vet ./...` green, no cycle.
6. omitempty/round-trip discipline for origin is met by CONSTRUCTION, not
   by a tag: `origin` is an UNEXPORTED field on `Cube`, which both
   `sigs.k8s.io/yaml` and `gopkg.in/yaml.v3` skip entirely — so it cannot
   emit a key at all (no `null`, no `""`) and cannot reach CUE
   re-validation inside `SaveValidated`. An extra test
   (`TestOriginNeverSerializes`) pins this: marshalling a
   `MarkRemoteOrigin`-flagged cube is byte-identical to marshalling the
   unflagged one.
7. Two assertions were ADDED beyond the plan's test sketch (strictly
   additive, same contract): (a) `LoadBytes` error labelling — a bad
   document loaded with `src = "oci://example/cfg:1"` must carry that REF
   in the message (proves `src` really replaced `path`); (b) the
   `SaveValidated` guard runs FIRST — neither `cube.yaml` nor
   `cube.yaml.tmp` exists after the refusal.
8. `SaveValidated`'s internal re-validation call is `Load(tmp)` on a
   freshly-marshalled temp file, so the reloaded cube has a ZERO origin —
   the guard cannot recurse or self-trip. (T10 must keep this call as
   `config.Load`, per the Task 10 file list.)

BLOCKERS: none

HANDOFF:
- **The two numbers T9/T10/T12 consume verbatim:**
  - `diag.CodeConfigRemoteReadOnly` = **`CUBE-0014`** — SaveValidated on a
    remote-origin cube.
  - `diag.CodeConfigRemoteFetch` = **`CUBE-0015`** — remote `-f` fetch /
    single-YAML failure. **Already declared AND registered by T8** (the
    plan's "0013 lands here too — Task 9 uses it"): T9 must only USE it,
    NOT re-add the constant or the registry entry.
- New exported API in `internal/config` (all live on this branch):
  - `func LoadBytes(raw []byte, src string) (*Cube, error)` — the full
    pipeline; `src` labels errors (path for `Load`, ref for `cfgload`).
    `Load(path)` is now exactly `os.ReadFile` + `LoadBytes(raw, path)`.
  - `type Origin struct { Ref, Pin string; Remote bool }`
  - `func (c *Cube) Origin() Origin` — zero value = local.
  - `func (c *Cube) MarkRemoteOrigin(ref, pin string)`
  - `SaveValidated` fails `CUBE-0014` on remote origin, before any write.
- T9's `cfgload.Load` shape is unchanged from the plan except the code
  number: `diag.Wrap(err, diag.CodeConfigRemoteFetch, …)` → CUBE-0015.
- T10: `cube.Origin().Remote` is the branch predicate for the CWD lock
  path and the `using remote config <ref> (<pin>)` info line; `Origin().Pin`
  carries the pin `pack.FetchFile` returned.
- Evidence — Step 1, `go test ./internal/diag/ -count=1` after adding both
  constants AND both registry entries (`TestRegistryCoversEveryDeclaredCode`
  enforces both directions):
  ```
  ok  	github.com/cube-idp/cube-idp/internal/diag	0.814s
  ```
- Evidence — Step 3, the new tests failed FIRST exactly as the plan's
  Expected (`undefined: LoadBytes`):
  ```
  # github.com/cube-idp/cube-idp/internal/config [github.com/cube-idp/cube-idp/internal/config.test]
  internal/config/load_test.go:832:20: undefined: LoadBytes
  internal/config/load_test.go:840:11: undefined: LoadBytes
  internal/config/load_test.go:850:12: undefined: LoadBytes
  internal/config/load_test.go:879:16: undefined: LoadBytes
  internal/config/load_test.go:883:17: undefined: LoadBytes
  FAIL	github.com/cube-idp/cube-idp/internal/config [build failed]
  ```
- Evidence — Step 5, the three new tests after implementing:
  ```
  === RUN   TestLoadBytesEqualsLoad
  --- PASS: TestLoadBytesEqualsLoad (0.00s)
  === RUN   TestSaveValidatedRefusesRemoteOrigin
  --- PASS: TestSaveValidatedRefusesRemoteOrigin (0.00s)
  === RUN   TestOriginNeverSerializes
  --- PASS: TestOriginNeverSerializes (0.00s)
  PASS
  ok  	github.com/cube-idp/cube-idp/internal/config	1.029s
  ```
- Evidence — Step 5 package-level, every pre-existing `Load` test still
  passing (the split is behavior-neutral):
  ```
  ok  	github.com/cube-idp/cube-idp/internal/config	0.702s
  ok  	github.com/cube-idp/cube-idp/internal/diag	0.355s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./...
  -count=1`: `BUILD OK`, `VET OK`, 32 `ok` packages,
  `go test ./... -count=1 | grep -c "^FAIL"` → `0`. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	12.270s
  ok  	github.com/cube-idp/cube-idp/internal/config	7.887s
  ok  	github.com/cube-idp/cube-idp/internal/diag	8.084s
  ok  	github.com/cube-idp/cube-idp/internal/diff	11.301s
  ok  	github.com/cube-idp/cube-idp/internal/lock	10.317s
  ok  	github.com/cube-idp/cube-idp/internal/pack	15.142s
  ok  	github.com/cube-idp/cube-idp/internal/refval	11.611s
  ok  	github.com/cube-idp/cube-idp/internal/up	10.731s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	11.205s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	9.725s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden -v
  -count=1` with NO `-update` (this task changes no flags), and
  `git status --short` immediately afterwards listing ONLY the five
  intended files (no golden rewritten, no stray staged file):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.213s

   M internal/config/load.go
   M internal/config/load_test.go
   M internal/config/types.go
   M internal/diag/codes.go
   M internal/diag/registry.go
  ```

### T9 — cfgload remote -f dispatch [Task 9]
STATUS: DONE
Outcome: COMMITS · FINDINGS · BLOCKERS · HANDOFF:

COMMITS:
- `4ba7fe0` docs: rv plan — claim T9
- `71a3a5d` feat(cfgload): remote -f dispatch over the pack ref grammar, CUBE-0015 (RV4)
- `<this>` docs: rv plan — T9 complete

FINDINGS:
1. **`pack.IsRemoteRef` was ADDED, not exported.** The plan's File Structure
   row and Task 9 Step 3 both say "export `IsRemoteRef`", implying an
   existing unexported `isRemoteRef`. No such function existed in any form:
   ```
   $ grep -rn "IsRemoteRef\|isRemoteRef" internal/ cmd/
   (no matches before this task)
   ```
   The plan's own Step-3 body was applied verbatim as a NEW function in
   `internal/pack/getter.go`, composed from the pre-existing unexported
   neighbours `isGitRef` / `isGetterRef` (both already in that file, lines
   16-35) — no logic reimplemented. Escape-hatch correction, not a redesign.
2. **Diag number: `CUBE-0015`, per Amendment 5 / T8 HANDOFF.** The Task 9
   heading and Step-1 sketch still say `CUBE-0013`; that number belongs to
   p7's `CodeEnginePackMismatch`. Only the CONSTANT is used
   (`diag.CodeConfigRemoteFetch`), which T8 already declared AND registered —
   T9 added NO constant and NO registry entry, exactly as T8's HANDOFF
   instructed. Verified live before writing code:
   ```
   internal/diag/codes.go:21:	CodeConfigRemoteReadOnly Code = "CUBE-0014"
   internal/diag/codes.go:22:	CodeConfigRemoteFetch    Code = "CUBE-0015"
   ```
   The test name was corrected to `TestLoadRemoteRefFetchFailureIsCUBE0015`
   and the package doc comment cites `CUBE-0014` for the SaveValidated
   guard.
3. **`diag.HasCode` does not exist** (the plan's test sketch uses it; T8
   FINDINGS 3 already recorded this). The repo idiom — a local
   `codeOf(t, err) diag.Code` helper over `errors.As` — was copied from
   `internal/config/load_test.go:16-23`. No helper invented, none added to
   the `diag` package.
4. **The plan's HTTP fixture leg does not work with this repo's getter
   plumbing — replaced with the OCI in-process-registry fixture** (the
   plan's Step-1 parenthetical explicitly authorises mirroring
   `internal/pack`'s existing fixture idiom). `pack.fetchGetter` hardcodes
   `getter.ClientModeDir`, so go-getter's `HttpGetter` demands an
   `X-Terraform-Get` redirect and a plain YAML body fails; a probe confirmed
   it (probe file deleted afterwards):
   ```
   HTTP: err=CUBE-4006: cannot fetch pack source "http://127.0.0.1:51637/cube.yaml":
         error downloading '…': no source URL was returned
   FILE: err=CUBE-4006: … invalid source string: file::/var/folders/…
   ```
   `internal/pack/catalog_test.go` already has the network-free precedent —
   `httptest` + go-containerregistry's in-process registry +
   `oci.PushPackDir` — so `TestLoadRemoteOCISetsOrigin` pushes the fixture
   `cube.yaml` as an OCI artifact and loads it through `oci://`. This is
   STRICTLY STRONGER than the plan's sketch: it exercises the whole remote
   path (`IsRemoteRef` → `DefaultCacheDir` → `FetchFile`/`pullOCI` →
   `singleYAML` → `LoadBytes` → `MarkRemoteOrigin`) end to end, and asserts
   the parsed document (`metadata.name == "demo"`) as well as the origin.
   `t.Setenv("HOME", t.TempDir())` keeps `DefaultCacheDir` off the
   developer's real `~/.cache` (catalog_test's `catalogEnv` precedent).
   No http-leg assertion was deferred to T12.
5. Two assertions ADDED beyond the plan's sketch, same contract: the
   CUBE-0015 test also asserts the CAUSE chain still carries
   `diag.CodePackRefUnpin` (CUBE-4007) — the plan's comment promises this
   but its sketch never checked it — and the OCI test asserts the decoded
   `metadata.name`.
6. **Import direction held.** `internal/cfgload` imports `config`, `diag`,
   `pack`; `internal/config` gained NOTHING (untouched by this task).
   `go build ./...` + `go vet ./...` green — no cycle. `go.mod` unchanged
   (go-containerregistry was already a test-only dependency via
   `internal/oci/ocitest`).
7. Scope fence respected: NO `cmd/*.go` call site migrated, no CWD lock
   path, no info line — those are T10. `internal/config/load.go`,
   `internal/lock/lock.go`, `internal/up/up.go`, `internal/diff/diff.go`
   were not opened for edit. `git status --short` before the code commit
   listed exactly `M internal/pack/getter.go` and `?? internal/cfgload/`.

BLOCKERS: none

HANDOFF:
- New API live on this branch:
  - `func pack.IsRemoteRef(ref string) bool` — `oci://` ‖ `isGitRef` ‖
    `isGetterRef`. `internal/pack/getter.go`, immediately above
    `NeedsGitCLI`.
  - `func cfgload.Load(ctx context.Context, pathOrRef string) (*config.Cube, error)`
    — `internal/cfgload/cfgload.go`. Dispatch, in order: (1) `os.Stat` OK →
    `config.Load(path)`, byte-identical local behaviour, zero origin;
    (2) missing + NOT remote-shaped → `config.Load(path)` so the canonical
    `CUBE-0001` is unchanged; (3) missing + remote-shaped →
    `pack.DefaultCacheDir` + `pack.FetchFile` (fetch error wrapped
    `CUBE-0015`, cause preserved) + `config.LoadBytes(raw, ref)` (its own
    codes pass through UNWRAPPED — a malformed remote cube still reports
    CUBE-0002/0003 etc. labelled with the REF) + `cube.MarkRemoteOrigin(ref, pin)`.
- **T10 contract:** substitute `config.Load(file)` → `cfgload.Load(cmd.Context(), file)`
  at the listed sites; `cube.Origin().Remote` is the predicate for the CWD
  lock path, the `using remote config <ref> (<pin>)` info line, and the
  Step-2b `bundleRailsCheck` clause. Keep `cmd/root.go:184` and
  `config.SaveValidated`'s internal temp-file `Load` on `config.Load`
  (T8 FINDINGS 8).
- `Origin().Pin` carries whatever `pack.FetchFile` returned for the form
  used: `oci:<digest>` / `git+<sha>` / `dir:<h1:…>` / `file:<sha256-hex>`.
- Note for T12 (docs/e2e): a remote `-f` over plain `http(s)://` pointing at
  a bare YAML file will FAIL in `fetchGetter` (ClientModeDir +
  X-Terraform-Get, FINDINGS 4) — this is pre-existing T1/pack behaviour, NOT
  introduced here and NOT in T9's scope to change. The e2e legs the spec
  §9 lists (gitea git ref, zot OCI artifact) are unaffected; document `-f`
  as git/oci-shaped rather than promising a raw https object.
- Evidence — Step 2, the tests failed FIRST exactly as the plan's Expected
  ("package does not exist"):
  ```
  # github.com/cube-idp/cube-idp/internal/cfgload [.../cfgload.test]
  internal/cfgload/cfgload_test.go:45:12: undefined: Load
  internal/cfgload/cfgload_test.go:55:12: undefined: Load
  internal/cfgload/cfgload_test.go:64:12: undefined: Load
  internal/cfgload/cfgload_test.go:99:12: undefined: Load
  FAIL	github.com/cube-idp/cube-idp/internal/cfgload [build failed]
  ```
- Evidence — Step 4, `go test ./internal/cfgload/ -v -count=1` after
  implementing, then `go test ./internal/cfgload/ ./internal/pack/ -count=1`:
  ```
  === RUN   TestLoadLocalFileWins
  --- PASS: TestLoadLocalFileWins (0.00s)
  === RUN   TestLoadMissingLocalNonRefIsConfigRead
  --- PASS: TestLoadMissingLocalNonRefIsConfigRead (0.00s)
  === RUN   TestLoadRemoteRefFetchFailureIsCUBE0015
  --- PASS: TestLoadRemoteRefFetchFailureIsCUBE0015 (0.00s)
  === RUN   TestLoadRemoteOCISetsOrigin
  --- PASS: TestLoadRemoteOCISetsOrigin (0.01s)
  ok  	github.com/cube-idp/cube-idp/internal/cfgload	1.400s

  ok  	github.com/cube-idp/cube-idp/internal/cfgload	0.949s
  ok  	github.com/cube-idp/cube-idp/internal/pack	4.710s
  ```
- Evidence — full gate `go build ./... && go vet ./... && go test ./...
  -count=1`: `BUILD OK`, `VET OK`, 33 `ok` packages,
  `go test ./... -count=1 | grep -c "^FAIL"` → `0`. Tail:
  ```
  ok  	github.com/cube-idp/cube-idp/cmd	15.003s
  ok  	github.com/cube-idp/cube-idp/internal/cfgload	5.891s
  ok  	github.com/cube-idp/cube-idp/internal/config	8.396s
  ok  	github.com/cube-idp/cube-idp/internal/diag	8.494s
  ok  	github.com/cube-idp/cube-idp/internal/lock	11.659s
  ok  	github.com/cube-idp/cube-idp/internal/pack	14.973s
  ok  	github.com/cube-idp/cube-idp/internal/refval	11.818s
  ok  	github.com/cube-idp/cube-idp/internal/up	10.719s
  ok  	github.com/cube-idp/cube-idp/internal/upgrade	10.463s
  ok  	github.com/cube-idp/cube-idp/tests/e2e	11.589s
  ```
- Evidence — F1 CLI freeze, `go test ./cmd/ -run TestCommandTreeGolden -v
  -count=1` with NO `-update` (this task changes no flags), and
  `git status --short` immediately afterwards listing ONLY the intended
  files (golden not rewritten, no stray staged file):
  ```
  === RUN   TestCommandTreeGolden
  --- PASS: TestCommandTreeGolden (0.00s)
  ok  	github.com/cube-idp/cube-idp/cmd	1.204s

   M internal/pack/getter.go
  ?? internal/cfgload/
  ```

### T10 — call-site migration + lock CWD path + origin bundle clause [Task 10 incl. Step 2b]
STATUS: IN_PROGRESS(4a9e20e0-d82f-4974-b1c1-99d2adacd233, 2026-07-19T00:00:00Z)
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
