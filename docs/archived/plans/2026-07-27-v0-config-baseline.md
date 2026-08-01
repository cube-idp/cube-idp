# cube-idp v0 Config Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tear down the old implementation on this branch and build the v0 greenfield baseline: a CRD-ready `Config` type (`cube-idp.dev/v1alpha1`), error machinery, a strict loading pipeline, and a CLI exposing `config validate` and `config show`.

**Architecture:** Standard Go layout — `cmd/` entrypoint, public `api/config/v1alpha1` types (real apimachinery, controller-gen deepcopy), domain packages under `internal/` (`config` loader, `cubeerr` error machinery, `cli` cobra wiring only). Errors reach users solely as `CUBE-<TAG>-NNN` coded errors rendered by the CLI boundary. Spec: `docs/design/2026-07-27-back-to-basics-structure.md`.

**Tech Stack:** Go 1.26, `k8s.io/apimachinery v0.36.2`, `sigs.k8s.io/yaml`, `github.com/spf13/cobra v1.10.2`, `controller-gen` (build-time tool), golangci-lint.

## Global Constraints

- Module path stays `github.com/cube-idp/cube-idp`.
- Runtime dependencies are EXACTLY: `k8s.io/apimachinery`, `sigs.k8s.io/yaml`, `github.com/spf13/cobra`. No new module without a plan change.
- API group/version string: `cube-idp.dev/v1alpha1`; kind: `Config`.
- Error code format: `CUBE-<TAG>-NNN` (config domain owns `CUBE-CFG-*`). Machinery (`internal/cubeerr`) never contains a code catalog.
- Load pipeline order is always: decode (strict) → default → validate.
- Domains never print; only `internal/cli` writes to stdout/stderr. Errors to stderr, data to stdout.
- Functions <50 lines, files <300 lines (generated `zz_generated.deepcopy.go` exempt).
- Every commit message ends with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Verification is real commands: `go build ./... && go vet ./... && go test ./... -count=1`. Never editor diagnostics.
- Work happens on branch `RafPe/rework-to-basics` only. Never push; never touch `main`.

## File Structure (end state)

```
cmd/cube-idp/main.go                     # entrypoint: signal ctx → cli.Execute → os.Exit
api/config/v1alpha1/doc.go               # group marker + generate directive
api/config/v1alpha1/groupversion_info.go # GroupVersion, SchemeBuilder, AddToScheme
api/config/v1alpha1/config_types.go      # Config, ConfigSpec
api/config/v1alpha1/defaults.go          # (c *Config) Default()
api/config/v1alpha1/validation.go        # (c *Config) Validate() field.ErrorList
api/config/v1alpha1/zz_generated.deepcopy.go  # generated, committed
internal/cubeerr/cubeerr.go              # Code, Coded, Wrap, ExitCode
internal/config/errors.go                # CUBE-CFG-* codes + constructors
internal/config/load.go                  # Load(fs.FS, path), LoadFile(path)
internal/cli/root.go                     # cobra root, -f/--config flag
internal/cli/config.go                   # config validate | config show
internal/cli/exit.go                     # Execute(ctx) int, error rendering
examples/cube.yaml                       # canonical sample
Makefile, .golangci.yml
```

---

### Task 1: Teardown and fresh module skeleton

**Files:**
- Delete: `cmd/`, `internal/`, `tests/`, `hack/`, `main.go`, `Makefile`, `.goreleaser.yaml`, `go.mod`, `go.sum`
- Create: `go.mod` (fresh), `examples/cube.yaml`
- Keep untouched: `docs/`, `.github/`, `CLAUDE.md`, `AGENTS.md`, `README.md`, `CHANGELOG.md`, `.gitignore`

**Interfaces:**
- Consumes: nothing.
- Produces: an empty module `github.com/cube-idp/cube-idp` (go 1.26.2) that later tasks add packages to; `examples/cube.yaml` used by CLI tests and docs.

- [ ] **Step 1: Delete the old implementation (explicit pathspecs, never `git add -A`)**

```bash
git rm -r -q cmd internal tests hack main.go Makefile .goreleaser.yaml go.mod go.sum
```

- [ ] **Step 2: Create the fresh go.mod**

Create `go.mod`:

```
module github.com/cube-idp/cube-idp

go 1.26.2
```

- [ ] **Step 3: Create the canonical example config**

Create `examples/cube.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
```

- [ ] **Step 4: Verify the empty module is sane**

Run: `go build ./... && go vet ./...`
Expected: exit 0 (a "matched no packages" warning is fine; no errors).

- [ ] **Step 5: Commit**

```bash
git add go.mod examples/cube.yaml
git commit -m "chore: v0 reset — remove old implementation, fresh module skeleton

Implements docs/design/2026-07-27-back-to-basics-structure.md §11.
Old code remains on main.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: `internal/cubeerr` — error machinery

**Files:**
- Create: `internal/cubeerr/cubeerr.go`
- Test: `internal/cubeerr/cubeerr_test.go`

**Interfaces:**
- Consumes: stdlib only (`errors`, `fmt`, `strings`).
- Produces (later tasks depend on these exact signatures):
  - `type Code string`
  - `type Coded struct { Code Code; Summary string; Remediation string }` with `Error() string`, `Unwrap() error`
  - `func Wrap(code Code, summary, remediation string, cause error) *Coded`
  - `func ExitCode(err error) int` — nil→0, `CUBE-CFG-*`→2, other Coded→1, uncoded→1
  - Constants `ExitSuccess = 0`, `ExitError = 1`, `ExitConfig = 2`

- [ ] **Step 1: Write the failing tests**

Create `internal/cubeerr/cubeerr_test.go`:

```go
package cubeerr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

func TestCodedError(t *testing.T) {
	cause := errors.New("boom")
	err := cubeerr.Wrap("CUBE-CFG-001", "bad apiVersion", "set apiVersion correctly", cause)

	if got, want := err.Error(), "CUBE-CFG-001: bad apiVersion"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Error("wrapped cause not reachable via errors.Is")
	}
}

func TestCodedThroughWrapping(t *testing.T) {
	coded := cubeerr.Wrap("CUBE-CFG-003", "invalid config", "fix fields", nil)
	wrapped := fmt.Errorf("loading: %w", coded)

	var got *cubeerr.Coded
	if !errors.As(wrapped, &got) {
		t.Fatal("Coded not found through fmt.Errorf wrapping")
	}
	if got.Code != "CUBE-CFG-003" {
		t.Errorf("Code = %q, want CUBE-CFG-003", got.Code)
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, cubeerr.ExitSuccess},
		{"config code maps to 2", cubeerr.Wrap("CUBE-CFG-002", "s", "r", nil), cubeerr.ExitConfig},
		{"wrapped config code maps to 2", fmt.Errorf("x: %w", cubeerr.Wrap("CUBE-CFG-001", "s", "r", nil)), cubeerr.ExitConfig},
		{"non-config code maps to 1", cubeerr.Wrap("CUBE-CLI-001", "s", "r", nil), cubeerr.ExitError},
		{"uncoded error maps to 1", errors.New("plain"), cubeerr.ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cubeerr.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cubeerr/ -count=1`
Expected: FAIL (package does not exist / undefined symbols).

- [ ] **Step 3: Write the implementation**

Create `internal/cubeerr/cubeerr.go`:

```go
// Package cubeerr is the error machinery for user-facing cube-idp errors.
// It defines the Coded error shape and exit-code mapping ONLY. Error code
// catalogs live in the domain packages that own them (e.g. internal/config
// owns CUBE-CFG-*) — this package must never grow a code table.
package cubeerr

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a diagnostic code of the form CUBE-<TAG>-NNN, where <TAG>
// names the owning component (CFG, CLI, CLU, ...).
type Code string

// Process exit codes.
const (
	ExitSuccess = 0
	ExitError   = 1
	ExitConfig  = 2
)

// Coded is the only error shape that reaches a user: a diagnostic code,
// a one-line summary, and a remediation hint, wrapping the technical cause.
type Coded struct {
	Code        Code
	Summary     string
	Remediation string
	err         error
}

func (e *Coded) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Summary) }

func (e *Coded) Unwrap() error { return e.err }

// Wrap builds a Coded error around an optional cause.
func Wrap(code Code, summary, remediation string, cause error) *Coded {
	return &Coded{Code: code, Summary: summary, Remediation: remediation, err: cause}
}

// ExitCode maps an error chain to the process exit code.
func ExitCode(err error) int {
	var c *Coded
	switch {
	case err == nil:
		return ExitSuccess
	case errors.As(err, &c):
		if tag(c.Code) == "CFG" {
			return ExitConfig
		}
		return ExitError
	default:
		return ExitError
	}
}

// tag extracts the component tag from CUBE-<TAG>-NNN; empty if malformed.
func tag(c Code) string {
	parts := strings.Split(string(c), "-")
	if len(parts) != 3 || parts[0] != "CUBE" {
		return ""
	}
	return parts[1]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cubeerr/ -count=1`
Expected: PASS (all tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cubeerr/cubeerr.go internal/cubeerr/cubeerr_test.go
git commit -m "feat(cubeerr): Coded error machinery and exit-code mapping

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `api/config/v1alpha1` — CRD-ready Config types

**Files:**
- Create: `api/config/v1alpha1/doc.go`, `api/config/v1alpha1/config_types.go`, `api/config/v1alpha1/groupversion_info.go`, `api/config/v1alpha1/defaults.go`, `api/config/v1alpha1/validation.go`, `api/config/v1alpha1/zz_generated.deepcopy.go` (generated)
- Modify: `go.mod` (adds apimachinery + controller-gen tool)
- Test: `api/config/v1alpha1/validation_test.go`

**Interfaces:**
- Consumes: `k8s.io/apimachinery` (`metav1`, `runtime`, `schema`, `util/validation/field`).
- Produces (later tasks depend on these exact names):
  - `type Config struct { metav1.TypeMeta; metav1.ObjectMeta; Spec ConfigSpec }`
  - `type ConfigSpec struct{}`
  - `var GroupVersion = schema.GroupVersion{Group: "cube-idp.dev", Version: "v1alpha1"}` (so `GroupVersion.String()` == `"cube-idp.dev/v1alpha1"`)
  - `var SchemeBuilder`, `var AddToScheme`
  - `func (c *Config) Default()`
  - `func (c *Config) Validate() field.ErrorList`

- [ ] **Step 1: Add dependencies**

```bash
go get k8s.io/apimachinery@v0.36.2
go get -tool sigs.k8s.io/controller-tools/cmd/controller-gen@latest
```

Expected: go.mod gains `k8s.io/apimachinery` (require) and a `tool` directive for controller-gen.

- [ ] **Step 2: Write the failing validation tests**

Create `api/config/v1alpha1/validation_test.go`:

```go
package v1alpha1_test

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

func validConfig() *v1alpha1.Config {
	return &v1alpha1.Config{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cube-idp.dev/v1alpha1", Kind: "Config"},
		ObjectMeta: metav1.ObjectMeta{Name: "dev"},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*v1alpha1.Config)
		wantField string // "" = expect valid
	}{
		{"valid minimal", func(c *v1alpha1.Config) {}, ""},
		{"missing name", func(c *v1alpha1.Config) { c.Name = "" }, "metadata.name"},
		{"uppercase name", func(c *v1alpha1.Config) { c.Name = "Dev" }, "metadata.name"},
		{"leading dash", func(c *v1alpha1.Config) { c.Name = "-dev" }, "metadata.name"},
		{"too long", func(c *v1alpha1.Config) { c.Name = strings.Repeat("a", 32) }, "metadata.name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			c.Default()
			errs := c.Validate()

			if tt.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("expected valid, got: %v", errs.ToAggregate())
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("expected validation errors, got none")
			}
			if !strings.Contains(errs.ToAggregate().Error(), tt.wantField) {
				t.Errorf("errors %v do not mention field %s", errs.ToAggregate(), tt.wantField)
			}
		})
	}
}

func TestDefaultIsIdempotent(t *testing.T) {
	c := validConfig()
	c.Default()
	before := c.DeepCopy()
	c.Default()
	if c.Name != before.Name {
		t.Error("Default() must be idempotent")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./api/... -count=1`
Expected: FAIL (package does not exist).

- [ ] **Step 4: Write the type definitions**

Create `api/config/v1alpha1/doc.go`:

```go
// Package v1alpha1 contains the cube-idp.dev/v1alpha1 API types.
//
// The Config kind is KRM-shaped: loaded from a local file today, written
// to Kubernetes API conventions so it can be served as a CRD later with
// no type changes. This package is a pure contract: types, defaults, and
// validation only — no I/O (the loader lives in internal/config).
//
// +kubebuilder:object:generate=true
// +groupName=cube-idp.dev
package v1alpha1

//go:generate go tool controller-gen object paths=.
```

Create `api/config/v1alpha1/config_types.go`:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// Config is the cube-idp configuration document.
// Only metadata.name is honored from ObjectMeta; server-populated fields
// (uid, resourceVersion, ...) are accepted and ignored when file-loaded.
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ConfigSpec `json:"spec,omitempty"`
}

// ConfigSpec is intentionally minimal in v0. Each component domain adds
// one typed sub-struct here (Cluster, Engine, Packs, ...) together with
// its defaults and validation — the loading machinery never changes.
type ConfigSpec struct{}
```

Create `api/config/v1alpha1/groupversion_info.go`:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the group and version of this API: cube-idp.dev/v1alpha1.
var GroupVersion = schema.GroupVersion{Group: "cube-idp.dev", Version: "v1alpha1"}

var (
	// SchemeBuilder collects functions that register this package's types.
	// Unused while configs are file-loaded; required for CRD promotion.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme registers this package's types into a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &Config{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
```

Create `api/config/v1alpha1/defaults.go`:

```go
package v1alpha1

// Default applies defaults in place. It is called by the loader after
// decoding and before Validate, and must be idempotent.
//
// v0 has no defaultable fields yet; component sub-structs bring their own
// defaulting here as they are added to ConfigSpec.
func (c *Config) Default() {}
```

Create `api/config/v1alpha1/validation.go`:

```go
package v1alpha1

import (
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// nameRE constrains the cube identity: DNS-label-like, max 31 chars.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// Validate checks the defaulted Config and returns every problem found.
// It never mutates; call Default first.
func (c *Config) Validate() field.ErrorList {
	var errs field.ErrorList
	namePath := field.NewPath("metadata", "name")

	switch {
	case c.Name == "":
		errs = append(errs, field.Required(namePath, "cube identity is required"))
	case !nameRE.MatchString(c.Name):
		errs = append(errs, field.Invalid(namePath, c.Name,
			"must match ^[a-z0-9][a-z0-9-]{0,30}$ (lowercase alphanumeric and dashes, max 31 chars)"))
	}
	return errs
}
```

- [ ] **Step 5: Generate deepcopy**

```bash
go tool controller-gen object paths=./api/config/v1alpha1
```

Expected: `api/config/v1alpha1/zz_generated.deepcopy.go` created, containing `DeepCopy`, `DeepCopyInto`, `DeepCopyObject` for `Config` and `ConfigSpec`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./api/... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add api/config/v1alpha1 go.mod go.sum
git commit -m "feat(api): CRD-ready Config types for cube-idp.dev/v1alpha1

Types, groupversion, no-op Default, name validation, generated deepcopy.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: `internal/config` — loading pipeline

**Files:**
- Create: `internal/config/errors.go`, `internal/config/load.go`
- Modify: `go.mod` (adds sigs.k8s.io/yaml)
- Test: `internal/config/load_test.go`

**Interfaces:**
- Consumes: Task 2 (`cubeerr.Code`, `cubeerr.Wrap`), Task 3 (`v1alpha1.Config`, `v1alpha1.GroupVersion`, `Default()`, `Validate()`).
- Produces (Task 5 depends on these exact signatures):
  - `func Load(fsys fs.FS, path string) (*v1alpha1.Config, error)`
  - `func LoadFile(path string) (*v1alpha1.Config, error)` — os convenience wrapper
  - Codes: `CodeUnsupportedAPIVersion cubeerr.Code = "CUBE-CFG-001"`, `CodeUnknownField = "CUBE-CFG-002"`, `CodeInvalidConfig = "CUBE-CFG-003"`

- [ ] **Step 1: Add dependency**

```bash
go get sigs.k8s.io/yaml@latest
```

- [ ] **Step 2: Write the failing tests**

Create `internal/config/load_test.go`:

```go
package config_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

const validYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
`

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantCode cubeerr.Code // "" = expect success
	}{
		{"valid minimal", validYAML, ""},
		{"server-side metadata accepted and ignored",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\n  uid: abc\n", ""},
		{"unknown top-level field",
			validYAML + "bogus: 1\n", config.CodeUnknownField},
		{"unknown spec field",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  bogus: 1\n", config.CodeUnknownField},
		{"wrong apiVersion",
			"apiVersion: nope.dev/v1\nkind: Config\nmetadata:\n  name: dev\n", config.CodeUnsupportedAPIVersion},
		{"wrong kind",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Cluster\nmetadata:\n  name: dev\n", config.CodeUnsupportedAPIVersion},
		{"empty file", "", config.CodeUnsupportedAPIVersion},
		{"missing name",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata: {}\n", config.CodeInvalidConfig},
		{"invalid name",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: BAD\n", config.CodeInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{"cube.yaml": {Data: []byte(tt.yaml)}}
			cfg, err := config.Load(fsys, "cube.yaml")

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.Name != "dev" {
					t.Errorf("Name = %q, want dev", cfg.Name)
				}
				return
			}

			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("error %v is not a *cubeerr.Coded", err)
			}
			if coded.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", coded.Code, tt.wantCode)
			}
			if coded.Remediation == "" {
				t.Error("coded error must carry a remediation")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load(fstest.MapFS{}, "cube.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/ -count=1`
Expected: FAIL (package does not exist).

- [ ] **Step 4: Write the domain error catalog**

Create `internal/config/errors.go`:

```go
package config

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The config domain owns the CUBE-CFG-* code range. Codes are declared
// here and nowhere else; the human-readable registry lives in
// docs/design/2026-07-27-back-to-basics-structure.md §5.2.
const (
	CodeUnsupportedAPIVersion cubeerr.Code = "CUBE-CFG-001"
	CodeUnknownField          cubeerr.Code = "CUBE-CFG-002"
	CodeInvalidConfig         cubeerr.Code = "CUBE-CFG-003"
)

func errUnsupportedAPIVersion(gvk string) error {
	return cubeerr.Wrap(CodeUnsupportedAPIVersion,
		fmt.Sprintf("unsupported apiVersion/kind %q", gvk),
		"set apiVersion: cube-idp.dev/v1alpha1 and kind: Config", nil)
}

func errUnknownField(cause error) error {
	return cubeerr.Wrap(CodeUnknownField,
		"config contains unknown fields",
		"remove the unknown fields listed above, or check for typos against `cube-idp config show`", cause)
}

func errInvalidConfig(cause error) error {
	return cubeerr.Wrap(CodeInvalidConfig,
		"invalid config",
		"fix the fields above and re-run `cube-idp config validate`", cause)
}
```

- [ ] **Step 5: Write the loader**

Create `internal/config/load.go`:

```go
// Package config loads and validates the cube-idp Config document.
// Pipeline order is fixed: strict decode → Default → Validate.
package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// Load reads, strictly decodes, defaults, and validates a Config from fsys.
// Fail fast: a non-nil *Config is always complete and valid.
func Load(fsys fs.FS, path string) (*v1alpha1.Config, error) {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return decode(raw)
}

// LoadFile is an os-filesystem convenience wrapper around Load.
func LoadFile(path string) (*v1alpha1.Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %s: %w", path, err)
	}
	return Load(os.DirFS(filepath.Dir(abs)), filepath.Base(abs))
}

func decode(raw []byte) (*v1alpha1.Config, error) {
	var tm struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := yaml.Unmarshal(raw, &tm); err != nil {
		return nil, fmt.Errorf("determine apiVersion/kind: %w", err)
	}

	gvk := tm.APIVersion + "/" + tm.Kind
	switch gvk {
	case v1alpha1.GroupVersion.String() + "/Config":
		var c v1alpha1.Config
		if err := yaml.UnmarshalStrict(raw, &c); err != nil {
			return nil, errUnknownField(err)
		}
		c.Default()
		if errs := c.Validate(); len(errs) > 0 {
			return nil, errInvalidConfig(errs.ToAggregate())
		}
		return &c, nil
	default:
		return nil, errUnsupportedAPIVersion(gvk)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/config/ -count=1`
Expected: PASS. If "unknown spec field" fails because `ConfigSpec` is empty and strict-decodes differently than expected, the test expectation stands — investigate the decode, not the test.

- [ ] **Step 7: Commit**

```bash
git add internal/config/errors.go internal/config/load.go internal/config/load_test.go go.mod go.sum
git commit -m "feat(config): strict load pipeline (decode → default → validate) with CUBE-CFG codes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: `internal/cli` + `cmd/cube-idp` — CLI surface

**Files:**
- Create: `internal/cli/root.go`, `internal/cli/config.go`, `internal/cli/exit.go`, `cmd/cube-idp/main.go`
- Modify: `go.mod` (adds cobra)
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: Task 2 (`cubeerr.ExitCode`, `*cubeerr.Coded`), Task 4 (`config.LoadFile`).
- Produces: `func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int` — the single entrypoint `main.go` calls; binary commands `cube-idp config validate|show [-f path]`.

- [ ] **Step 1: Add dependency**

```bash
go get github.com/spf13/cobra@v1.10.2
```

- [ ] **Step 2: Write the failing tests**

Create `internal/cli/cli_test.go`:

```go
package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cli"
)

const validYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cube.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = cli.Execute(context.Background(), args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestConfigValidateValid(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, stdout, _ := run(t, "config", "validate", "-f", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("stdout %q should confirm validity", stdout)
	}
}

func TestConfigValidateInvalid(t *testing.T) {
	path := writeTemp(t, strings.Replace(validYAML, "name: dev", "name: \"\"", 1))
	code, _, stderr := run(t, "config", "validate", "-f", path)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "CUBE-CFG-003") {
		t.Errorf("stderr %q should carry CUBE-CFG-003", stderr)
	}
	if !strings.Contains(stderr, "metadata.name") {
		t.Errorf("stderr %q should name the offending field", stderr)
	}
}

func TestConfigShowRoundTrip(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, stdout, _ := run(t, "config", "show", "-f", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"apiVersion: cube-idp.dev/v1alpha1", "kind: Config", "name: dev"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show output missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestMissingFile(t *testing.T) {
	code, _, stderr := run(t, "config", "validate", "-f", filepath.Join(t.TempDir(), "nope.yaml"))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for missing file")
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cli/ -count=1`
Expected: FAIL (package does not exist).

- [ ] **Step 4: Write the CLI**

Create `internal/cli/root.go`:

```go
// Package cli holds ALL cobra wiring and user-facing rendering for
// cube-idp. Commands only map flags and call domain packages — business
// logic never lives here.
package cli

import (
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp — internal developer platform, from a single config",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringP("config", "f", "cube.yaml", "path to the Config document")
	root.AddCommand(newConfigCmd())
	return root
}
```

Create `internal/cli/config.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate the Config document",
	}
	cmd.AddCommand(newConfigValidateCmd(), newConfigShowCmd())
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Load, default, and validate the Config; report every problem",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("config")
			cfg, err := config.LoadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config %q is valid\n", cfg.Name)
			return nil
		},
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the loaded, defaulted Config back as YAML",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("config")
			cfg, err := config.LoadFile(path)
			if err != nil {
				return err
			}
			out, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("render config: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
}
```

Create `internal/cli/exit.go`:

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// Execute runs the CLI and returns the process exit code. It is the ONLY
// place errors are rendered: coded errors print code, summary, cause
// detail, and remediation to stderr.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		printError(stderr, err)
		return cubeerr.ExitCode(err)
	}
	return cubeerr.ExitSuccess
}

func printError(w io.Writer, err error) {
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		fmt.Fprintf(w, "✗ error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "✗ %s: %s\n", coded.Code, coded.Summary)
	if cause := coded.Unwrap(); cause != nil {
		fmt.Fprintf(w, "    %v\n", cause)
	}
	if coded.Remediation != "" {
		fmt.Fprintf(w, "  → %s\n", coded.Remediation)
	}
}
```

Create `cmd/cube-idp/main.go`:

```go
// Command cube-idp is the cube-idp binary entrypoint.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cube-idp/cube-idp/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS (all packages).

- [ ] **Step 6: Smoke-test the real binary against the example**

```bash
go build -o /tmp/cube-idp-v0 ./cmd/cube-idp
/tmp/cube-idp-v0 config validate -f examples/cube.yaml && echo "exit=$?"
/tmp/cube-idp-v0 config show -f examples/cube.yaml
```

Expected: `config "dev" is valid` + `exit=0`; show prints the YAML round-trip including `apiVersion: cube-idp.dev/v1alpha1`.

- [ ] **Step 7: Commit**

```bash
git add internal/cli cmd/cube-idp go.mod go.sum
git commit -m "feat(cli): cube-idp binary with config validate/show

Cobra wiring only; errors rendered once at the CLI boundary.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Tooling gates — Makefile and golangci-lint

**Files:**
- Create: `Makefile`, `.golangci.yml`

**Interfaces:**
- Consumes: everything previous.
- Produces: `make build|test|generate|lint` — the green gate for all future work.

- [ ] **Step 1: Write the Makefile**

Create `Makefile`:

```makefile
GO ?= go

.PHONY: build test generate lint filelen

build:
	CGO_ENABLED=0 $(GO) build -o cube-idp ./cmd/cube-idp

test:
	$(GO) vet ./...
	$(GO) test ./... -count=1

generate:
	$(GO) tool controller-gen object paths=./api/config/v1alpha1

lint: filelen
	golangci-lint run ./...

# Files stay under 300 lines (generated code exempt) — design §7.
filelen:
	@bad=$$(find . -name '*.go' -not -name 'zz_generated*' -not -path './.git/*' \
		| xargs wc -l | awk '$$1 > 300 && $$2 != "total" {print $$2" ("$$1" lines)"}'); \
	if [ -n "$$bad" ]; then \
		echo "files exceed 300 lines:"; echo "$$bad"; exit 1; \
	fi
```

- [ ] **Step 2: Write the lint config**

Create `.golangci.yml`:

```yaml
version: "2"

linters:
  default: standard   # errcheck, govet, ineffassign, staticcheck, unused
  enable:
    - funlen
  settings:
    funlen:
      lines: 50
      ignore-comments: true
```

- [ ] **Step 3: Run the full gate**

Run: `make build && make test && make lint`
Expected: all green. If `golangci-lint` is not installed, install per its docs (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`) and re-run; if the v2 config schema is rejected by an older local binary, upgrade the binary rather than downgrading the config. If `funlen` flags a function, refactor the function — do not raise the limit.

- [ ] **Step 4: Verify generate is reproducible**

Run: `make generate && git diff --exit-code api/`
Expected: exit 0 (regeneration produces no diff).

- [ ] **Step 5: Commit**

```bash
git add Makefile .golangci.yml
git commit -m "chore: build/test/generate/lint gates with size limits

funlen(50) via golangci-lint; 300-line file gate in make lint.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

```bash
make build && make test && make lint
./cube-idp config validate -f examples/cube.yaml
./cube-idp config show -f examples/cube.yaml
```

Expected: all green; validate prints `config "dev" is valid`, exit 0; show round-trips the example. Binary `cube-idp` at repo root is a build artifact — do not commit it.
