# cube-idp Phase 1 (MVP) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the cube-idp MVP: a single Go binary where `cube-idp up` creates a kind (or targets an existing) cluster, installs Flux + a zot OCI registry, renders data-only packs (traefik, gitea, argocd) in-process, pushes them as OCI artifacts, and hands reconciliation to Flux — then exits.

**Architecture:** "Pusher, not operator" (spec §1): no CRDs, no daemon, no embedded git server. Kernel units per spec §4.1 — `ClusterProvider` (kind + existing, D10 two-layer customization), `Applier` (fluxcd/pkg/ssa: SSA + kstatus waits + inventory), `GitOpsEngine` interface with Flux implementation (D2), pack engine (CUE-validated values, helm/raw-manifest rendering in-process), typed `CUBE-xxxx` diagnostics.

**Tech Stack:** Go 1.24+, cobra, cuelang.org/go, sigs.k8s.io/kind (library), client-go, fluxcd/pkg/ssa, fluxcd/pkg/oci + oras, Helm SDK (wrapped), envtest for apply-layer tests.

**Spec:** `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` — treat it as the source of truth. Task-level MVP narrowings (allowed by spec §6 Phase 1):
- Pack render types in Phase 1: raw manifests + helm chart. Kustomize overlays land in Phase 2.
- Host→registry access is via port-forward to the zot Service (works identically for kind and existing providers); no NodePort.
- Helm wrapper targets `helm.sh/helm/v4` action API; if the v4 API surface differs at build time, pin `helm.sh/helm/v3` behind the same internal interface (spec §7 risk mitigation) — the wrapper is the only file allowed to import helm.

## Global Constraints (from spec — every task inherits these)

- Module path: `github.com/rafpe/cube-idp`. Repo root = this repo's root (alongside `docs/`).
- Single static binary; nothing runs on the developer machine after exit (spec §3 non-goals). No cube-idp CRDs.
- Config: `apiVersion: cube-idp.dev/v1alpha1`, `kind: Cube` (D5). Users author YAML; CUE is internal only.
- Defaults: provider `kind`, engine `flux`, gateway pack `traefik`, host `cube-idp.localtest.me`, port `8443`.
- SSA field manager: `cube-idp`; prune opt-out annotation `cube-idp.dev/prune: disabled` (§4.1 Applier).
- Every user-facing failure is a typed `CUBE-xxxx` error with remediation (§4.5); every wait has a hard deadline. Code ranges: 0xxx preflight/config, 1xxx cluster, 2xxx apply, 3xxx engine, 4xxx pack, 5xxx registry.
- All in-cluster components live in namespace `cube-idp-system` except engine (`flux-system`) and pack-defined namespaces.
- Labels: everything cube-idp applies carries `cube-idp.dev/cube: <name>`; CLI-surfaced secrets carry `cube-idp.dev/cli-secret: "true"` and `cube-idp.dev/pack-name: <pack>`.
- `engine.type: argocd` must parse as valid config but fail at engine construction with `CUBE-3002` "argocd engine ships in Phase 2" (D2: interface day one, Flux impl Phase 1).
- Commit style: conventional commits (`feat:`, `test:`, `chore:`), each task ends committed and `go build ./... && go test ./...` green.

## File Structure

```
main.go                          # calls cmd.Execute()
cmd/                             # cobra commands only — no business logic
  root.go  version.go  up.go  down.go  status.go  get.go  config.go  init.go
internal/diag/                   # CUBE-xxxx typed errors, Finding, Render
  diag.go  diag_test.go
internal/config/                 # cube.yaml load + CUE validation + defaults
  types.go  load.go  schema.cue  load_test.go  testdata/
internal/cluster/                # ClusterProvider iface + factory
  provider.go  existing.go  existing_test.go
internal/cluster/kindp/          # kind provider + D10 merge
  kind.go  merge.go  merge_test.go  testdata/
internal/apply/                  # Applier: SSA, waits, inventory, delete
  applier.go  inventory.go  applier_test.go  testenv_test.go
internal/registry/               # embedded zot manifests + install + port-forward
  zot.go  portforward.go  manifests/zot.yaml  zot_test.go
internal/pack/                   # pack model, sources, CUE values, rendering
  pack.go  source.go  render.go  helm.go  pack_test.go  testdata/
internal/engine/                 # GitOpsEngine iface + factory
  engine.go  engine_test.go
internal/engine/flux/            # Flux implementation
  flux.go  deliver.go  manifests/install.yaml  flux_test.go
internal/oci/                    # artifact push (fluxcd/pkg/oci)
  push.go
internal/up/                     # orchestration of `up` (kept out of cmd/)
  up.go
packs/traefik/  packs/gitea/  packs/argocd/   # starter packs (data only)
hack/gen-flux-manifests.sh
Makefile
.github/workflows/ci.yaml
tests/e2e/e2e_test.go
```

---

### Task 1: Go module, cobra shell, version command

**Files:**
- Create: `go.mod`, `main.go`, `cmd/root.go`, `cmd/version.go`, `Makefile`, `.gitignore`
- Test: `cmd/version_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `cmd.Execute() error`; `cmd.NewRootCmd() *cobra.Command` (all later command tasks attach to it); `var Version = "dev"` overridable via ldflags.

- [ ] **Step 1: Scaffold module**

```bash
cd <repo-root>
go mod init github.com/rafpe/cube-idp
go get github.com/spf13/cobra@latest
```

Create `.gitignore`:

```
/cube-idp
/dist/
*.test
/bin/
```

- [ ] **Step 2: Write the failing test**

`cmd/version_test.go`:

```go
package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "cube-idp version dev") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/ -run TestVersionCommand -v`
Expected: FAIL (package cmd does not exist / NewRootCmd undefined)

- [ ] **Step 4: Implement**

`cmd/root.go`:

```go
package cmd

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp stands up an internal developer platform on Kubernetes and gets out of the way",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	return root
}

func Execute() error { return NewRootCmd().Execute() }
```

`cmd/version.go`:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set via -ldflags "-X github.com/rafpe/cube-idp/cmd.Version=v0.1.0".
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cube-idp version",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintf(c.OutOrStdout(), "cube-idp version %s\n", Version)
			return nil
		},
	}
}
```

`main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/rafpe/cube-idp/cmd"
	"github.com/rafpe/cube-idp/internal/diag"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, diag.Render(err))
		os.Exit(1)
	}
}
```

Note: `diag.Render` arrives in Task 2. Until then make `main.go` compile by using `fmt.Fprintln(os.Stderr, err)` and switch to `diag.Render` in Task 2 Step 4.

`Makefile`:

```make
BIN := cube-idp

build:
	CGO_ENABLED=0 go build -o $(BIN) .

test:
	go test ./...

envtest-assets:
	go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path

.PHONY: build test envtest-assets
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/ -run TestVersionCommand -v && go build ./...`
Expected: PASS, clean build

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: cobra shell with version command"
```

---

### Task 2: internal/diag — typed CUBE errors and findings

**Files:**
- Create: `internal/diag/diag.go`
- Modify: `main.go` (switch to `diag.Render`)
- Test: `internal/diag/diag_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (used by every later task):

```go
type Code string                     // e.g. Code("CUBE-1101")
type Error struct{ Code Code; Summary string; Cause error; Remediation string }
func New(code Code, summary, remediation string) *Error
func Wrap(cause error, code Code, summary, remediation string) *Error
func (e *Error) Error() string       // "CUBE-1101: <summary>: <cause>"
func (e *Error) Unwrap() error
func Render(err error) string        // multi-line human block; plain errors pass through
type Severity string                 // "error" | "warning" | "info"
type Finding struct{ Code Code; Severity Severity; Message, Remediation string }
```

- [ ] **Step 1: Write the failing test**

`internal/diag/diag_test.go`:

```go
package diag

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorFormatsCodeAndCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := Wrap(cause, "CUBE-1101", "cluster unreachable", "check that the kubeconfig context points at a running cluster")
	if got := err.Error(); !strings.Contains(got, "CUBE-1101") || !strings.Contains(got, "connection refused") {
		t.Fatalf("unexpected: %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap chain broken")
	}
}

func TestRenderIncludesRemediation(t *testing.T) {
	err := New("CUBE-0002", "cube.yaml is invalid", "run `cube-idp config schema` to see the expected shape")
	out := Render(err)
	for _, want := range []string{"CUBE-0002", "cube.yaml is invalid", "fix:", "config schema"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderPlainError(t *testing.T) {
	if got := Render(errors.New("boom")); got != "Error: boom" {
		t.Fatalf("unexpected: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement**

`internal/diag/diag.go`:

```go
// Package diag defines cube-idp's typed error model. Every user-facing
// failure carries a CUBE-xxxx code and a copy-pasteable remediation
// (spec §4.5). Code ranges: 0xxx preflight/config, 1xxx cluster,
// 2xxx apply, 3xxx engine, 4xxx pack, 5xxx registry.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

type Code string

type Error struct {
	Code        Code
	Summary     string
	Cause       error
	Remediation string
}

func New(code Code, summary, remediation string) *Error {
	return &Error{Code: code, Summary: summary, Remediation: remediation}
}

func Wrap(cause error, code Code, summary, remediation string) *Error {
	return &Error{Code: code, Summary: summary, Cause: cause, Remediation: remediation}
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Summary, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Summary)
}

func (e *Error) Unwrap() error { return e.Cause }

// Render produces the terminal-facing block for any error. *Error values
// get the structured layout; anything else passes through unchanged.
func Render(err error) string {
	var de *Error
	if !errors.As(err, &de) {
		return "Error: " + err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "✗ %s  %s\n", de.Code, de.Summary)
	if de.Cause != nil {
		fmt.Fprintf(&b, "  cause: %v\n", de.Cause)
	}
	if de.Remediation != "" {
		fmt.Fprintf(&b, "  fix:   %s", de.Remediation)
	}
	return strings.TrimRight(b.String(), "\n")
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is a non-fatal diagnostic surfaced by providers/engines and the
// doctor command (Phase 2 wires the command; the type ships now because
// ClusterProvider.Diagnose returns it).
type Finding struct {
	Code        Code
	Severity    Severity
	Message     string
	Remediation string
}
```

- [ ] **Step 4: Switch main.go to diag.Render**

In `main.go`, ensure the error path is `fmt.Fprintln(os.Stderr, diag.Render(err))` (import `github.com/rafpe/cube-idp/internal/diag`).

- [ ] **Step 5: Run tests**

Run: `go test ./... && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: typed CUBE-xxxx error model and findings"
```

---

### Task 3: internal/config — cube.yaml loader with CUE validation and defaults

**Files:**
- Create: `internal/config/types.go`, `internal/config/load.go`, `internal/config/schema.cue`, `internal/config/testdata/{minimal.yaml,full.yaml,bad-provider.yaml,existing-with-ports.yaml}`
- Test: `internal/config/load_test.go`

**Interfaces:**
- Consumes: `diag.New/Wrap`.
- Produces (exact types later tasks depend on):

```go
type Cube struct{ APIVersion, Kind string; Metadata Metadata; Spec Spec }
type Metadata struct{ Name string }
type Spec struct{ Cluster ClusterSpec; Engine EngineSpec; Gateway GatewaySpec; Packs []PackRef }
type ClusterSpec struct {
	Provider          string        // "kind" | "existing"
	Context           string        // for existing
	KubernetesVersion string
	ExtraPorts        []PortMapping // D10 layer 1
	Registry          RegistrySpec  // D10 layer 1
	Mounts            []Mount       // D10 layer 1
	ProviderConfig    string        // D10 layer 2: file path or inline YAML
}
type PortMapping struct{ HostPort, NodePort int32 }
type RegistrySpec struct{ Mirrors map[string]string; Insecure []string }
type Mount struct{ HostPath, NodePath string }
type EngineSpec struct{ Type string }               // "flux" | "argocd"
type GatewaySpec struct{ Pack, Host string; Port int }
type PackRef struct{ Ref string; Values map[string]any }
func Load(path string) (*Cube, error)               // reads, validates, defaults
func Default(name string) *Cube                      // D9 default profile (init uses it)
```

- CUE validation errors → `CUBE-0002`; missing file → `CUBE-0001`; `existing` + node-creation fields (ExtraPorts/Mounts/ProviderConfig/KubernetesVersion) → `CUBE-1003` (D10, spec §4.1).

- [ ] **Step 1: Write the failing test**

`internal/config/load_test.go`:

```go
package config

import (
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *diag.Error, got %T: %v", err, err)
	}
	return de.Code
}

func TestLoadMinimalAppliesDefaults(t *testing.T) {
	c, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "kind" || c.Spec.Engine.Type != "flux" {
		t.Fatalf("defaults not applied: %+v", c.Spec)
	}
	if c.Spec.Gateway.Host != "cube-idp.localtest.me" || c.Spec.Gateway.Port != 8443 || c.Spec.Gateway.Pack != "traefik" {
		t.Fatalf("gateway defaults: %+v", c.Spec.Gateway)
	}
}

func TestLoadFullRoundTrips(t *testing.T) {
	c, err := Load("testdata/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spec.Cluster.ExtraPorts) != 1 || c.Spec.Cluster.ExtraPorts[0].HostPort != 32222 {
		t.Fatalf("extraPorts: %+v", c.Spec.Cluster.ExtraPorts)
	}
	if c.Spec.Cluster.Registry.Mirrors["docker.io"] != "https://mirror.corp.example" {
		t.Fatalf("mirrors: %+v", c.Spec.Cluster.Registry)
	}
	if len(c.Spec.Packs) != 2 || c.Spec.Packs[1].Values["replicas"] != 2 {
		t.Fatalf("packs: %+v", c.Spec.Packs)
	}
}

func TestLoadRejectsBadProvider(t *testing.T) {
	_, err := Load("testdata/bad-provider.yaml")
	if codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("want CUBE-0002, got %v", err)
	}
}

func TestLoadRejectsNodeFieldsOnExisting(t *testing.T) {
	_, err := Load("testdata/existing-with-ports.yaml")
	if codeOf(t, err) != "CUBE-1003" {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("testdata/nope.yaml")
	if codeOf(t, err) != "CUBE-0001" {
		t.Fatalf("want CUBE-0001, got %v", err)
	}
}

func TestDefaultProfileIncludesGitea(t *testing.T) { // D9
	c := Default("dev")
	found := false
	for _, p := range c.Spec.Packs {
		if p.Ref == "oci://ghcr.io/cube-idp/packs/gitea:0.1.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default profile must include gitea (D9): %+v", c.Spec.Packs)
	}
}
```

- [ ] **Step 2: Create testdata fixtures**

`internal/config/testdata/minimal.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
```

`internal/config/testdata/full.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: full
spec:
  cluster:
    provider: kind
    kubernetesVersion: v1.33.1
    extraPorts:
      - hostPort: 32222
        nodePort: 32222
    registry:
      mirrors:
        docker.io: https://mirror.corp.example
      insecure: [registry.corp.example:5000]
    mounts:
      - hostPath: /tmp/images
        nodePath: /var/lib/images
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.1.0
    - ref: ./platform/backstage
      values:
        replicas: 2
```

`internal/config/testdata/bad-provider.yaml`: copy `minimal.yaml` and add:

```yaml
spec:
  cluster:
    provider: minikube
```

`internal/config/testdata/existing-with-ports.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: remote
spec:
  cluster:
    provider: existing
    context: my-eks
    extraPorts:
      - hostPort: 32222
        nodePort: 32222
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 4: Implement types and CUE schema**

```bash
go get cuelang.org/go@latest gopkg.in/yaml.v3@latest
```

`internal/config/types.go`: exactly the types from the Interfaces block above, each field tagged `yaml:"camelCase"` and `json:"camelCase"` (e.g. `HostPort int32 \`yaml:"hostPort" json:"hostPort"\``). Plus:

```go
// Default returns the D9 default profile that `cube-idp init` writes:
// kind cluster, flux engine, traefik gateway, gitea + argocd packs.
func Default(name string) *Cube {
	return &Cube{
		APIVersion: "cube-idp.dev/v1alpha1",
		Kind:       "Cube",
		Metadata:   Metadata{Name: name},
		Spec: Spec{
			Cluster: ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"},
			Engine:  EngineSpec{Type: "flux"},
			Gateway: GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443},
			Packs: []PackRef{
				{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
				{Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.1.0"},
			},
		},
	}
}
```

`internal/config/schema.cue`:

```cue
package config

#Cube: {
	apiVersion: "cube-idp.dev/v1alpha1"
	kind:       "Cube"
	metadata: name: =~"^[a-z0-9][a-z0-9-]{0,30}$"
	spec: {
		cluster: {
			provider:           *"kind" | "existing"
			context?:           string
			kubernetesVersion:  *"v1.33.1" | string
			extraPorts?: [...{hostPort: int & >0 & <65536, nodePort: int & >0 & <65536}]
			registry?: {mirrors?: {[string]: string}, insecure?: [...string]}
			mounts?: [...{hostPath: string, nodePath: string}]
			providerConfig?: string
		}
		engine: type: *"flux" | "argocd"
		gateway: {
			pack: *"traefik" | string
			host: *"cube-idp.localtest.me" | string
			port: *8443 | (int & >0 & <65536)
		}
		packs?: [...{ref: string & !="", values?: {...}}]
	}
}
```

`internal/config/load.go`:

```go
package config

import (
	_ "embed"
	"fmt"
	"os"

	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"

	"github.com/rafpe/cube-idp/internal/diag"
)

//go:embed schema.cue
var schemaCUE string

func Load(path string) (*Cube, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-0001", fmt.Sprintf("cannot read %s", path),
			"run `cube-idp init` to generate a starter cube.yaml")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s is not valid YAML", path),
			"fix the YAML syntax; run `cube-idp config schema` for the expected shape")
	}

	ctx := cuecontext.New()
	schema := ctx.CompileString(schemaCUE).LookupPath(cuePath("#Cube"))
	val := schema.Unify(ctx.Encode(doc))
	if err := val.Validate(); err != nil {
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}

	var c Cube
	if err := val.Decode(&c); err != nil { // decodes with CUE defaults applied
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}
	if err := crossValidate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// crossValidate enforces rules CUE can't express cleanly across fields.
func crossValidate(c *Cube) error {
	cl := c.Spec.Cluster
	if cl.Provider == "existing" {
		if len(cl.ExtraPorts) > 0 || len(cl.Mounts) > 0 || cl.ProviderConfig != "" {
			return diag.New("CUBE-1003",
				"cluster.extraPorts/mounts/providerConfig imply node creation and are not valid with provider: existing",
				"remove those fields, or switch to provider: kind")
		}
	}
	return nil
}
```

`cuePath` helper: `func cuePath(s string) cue.Path { return cue.ParsePath(s) }` (import `cuelang.org/go/cue`). Note on defaults: decoding a unified value applies CUE's `*default` markers, which is how `minimal.yaml` comes back fully populated. If `Decode` in the installed CUE version does not apply defaults for optional structs (`gateway` absent entirely), add a `fillDefaults(&c)` Go fallback that sets the five documented defaults when zero-valued — the test asserts the outcome either way.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/config/ -v`
Expected: all 6 tests PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: cube.yaml loader with CUE validation, defaults, and D10 cross-checks"
```

---

### Task 4: ClusterProvider interface + existing provider

**Files:**
- Create: `internal/cluster/provider.go`, `internal/cluster/existing.go`
- Test: `internal/cluster/existing_test.go`

**Interfaces:**
- Consumes: `config.ClusterSpec`, `diag`.
- Produces:

```go
type Conn struct{ Kubeconfig []byte; Context string; REST *rest.Config }
type Provider interface {
	Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*Conn, error) // idempotent
	Delete(ctx context.Context, name string) error
	Exists(ctx context.Context, name string) (bool, error)
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
	Diagnose(ctx context.Context, name string) []diag.Finding
}
func New(spec config.ClusterSpec, gw config.GatewaySpec) (Provider, error) // factory; CUBE-1001 unknown provider
```

- [ ] **Step 1: Write the failing test**

`internal/cluster/existing_test.go`:

```go
package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

const kubeconfigTmpl = `
apiVersion: v1
kind: Config
clusters:
- name: dead
  cluster: {server: "https://127.0.0.1:1"}
contexts:
- name: dead-ctx
  context: {cluster: dead, user: u}
users:
- name: u
  user: {}
current-context: dead-ctx
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(kubeconfigTmpl), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFactoryRejectsUnknownProvider(t *testing.T) {
	_, err := New(config.ClusterSpec{Provider: "minikube"}, config.GatewaySpec{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1001" {
		t.Fatalf("want CUBE-1001, got %v", err)
	}
}

func TestExistingMissingContext(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, err := New(config.ClusterSpec{Provider: "existing", Context: "nope"}, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Ensure(context.Background(), "dev", config.ClusterSpec{Provider: "existing", Context: "nope"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1102" {
		t.Fatalf("want CUBE-1102 (context not found), got %v", err)
	}
}

func TestExistingUnreachable(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, _ := New(config.ClusterSpec{Provider: "existing", Context: "dead-ctx"}, config.GatewaySpec{})
	_, err := p.Ensure(context.Background(), "dev", config.ClusterSpec{Provider: "existing", Context: "dead-ctx"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1101" {
		t.Fatalf("want CUBE-1101 (unreachable), got %v", err)
	}
}

func TestExistingDeleteIsNoOp(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, _ := New(config.ClusterSpec{Provider: "existing", Context: "dead-ctx"}, config.GatewaySpec{})
	if err := p.Delete(context.Background(), "dev"); err != nil {
		t.Fatalf("delete must never destroy a cluster cube-idp did not create: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement**

```bash
go get k8s.io/client-go@latest k8s.io/apimachinery@latest
```

`internal/cluster/provider.go`:

```go
// Package cluster defines the ClusterProvider seam (spec §4.1).
// Implementations are compiled in — no plugin protocol (D8).
package cluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

type Conn struct {
	Kubeconfig []byte
	Context    string
	REST       *rest.Config
}

type Provider interface {
	Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*Conn, error)
	Delete(ctx context.Context, name string) error
	Exists(ctx context.Context, name string) (bool, error)
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
	Diagnose(ctx context.Context, name string) []diag.Finding
}

func New(spec config.ClusterSpec, gw config.GatewaySpec) (Provider, error) {
	switch spec.Provider {
	case "kind":
		return newKind(gw), nil
	case "existing":
		return &existing{}, nil
	default:
		return nil, diag.New("CUBE-1001",
			fmt.Sprintf("unknown cluster provider %q", spec.Provider),
			"use provider: kind or provider: existing")
	}
}
```

(`newKind` lands in Task 5; until then declare `func newKind(gw config.GatewaySpec) Provider { panic("kind provider: Task 5") }` in `provider.go` so this task compiles, and remove it in Task 5.)

`internal/cluster/existing.go`:

```go
package cluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

// existing targets a pre-existing cluster through a kubeconfig context.
// It never creates or deletes clusters; Delete is a documented no-op
// (down removes only cube-idp-managed resources, spec §4.3).
type existing struct{}

func (e *existing) load(kctx string) (clientcmd.ClientConfig, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules() // honors KUBECONFIG
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kctx}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides), nil
}

func (e *existing) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*Conn, error) {
	cc, _ := e.load(spec.Context)
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1102", "cannot load kubeconfig", "check $KUBECONFIG")
	}
	kctx := spec.Context
	if kctx == "" {
		kctx = raw.CurrentContext
	}
	if _, ok := raw.Contexts[kctx]; !ok {
		return nil, diag.New("CUBE-1102", fmt.Sprintf("kubeconfig context %q not found", kctx),
			"run `kubectl config get-contexts` and set spec.cluster.context to one of them")
	}
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1102", "cannot build client config", "check $KUBECONFIG")
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err == nil {
		_, err = dc.ServerVersion()
	}
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1101", fmt.Sprintf("cluster behind context %q is unreachable", kctx),
			"check that the cluster is running and the context credentials are valid")
	}
	kubeconfig, err := clientcmd.Write(raw)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1102", "cannot serialize kubeconfig", "check $KUBECONFIG")
	}
	return &Conn{Kubeconfig: kubeconfig, Context: kctx, REST: restCfg}, nil
}

func (e *existing) Delete(ctx context.Context, name string) error { return nil }

func (e *existing) Exists(ctx context.Context, name string) (bool, error) {
	cc, _ := e.load("")
	raw, err := cc.RawConfig()
	if err != nil {
		return false, nil
	}
	return len(raw.Contexts) > 0, nil
}

func (e *existing) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	cc, _ := e.load("")
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1102", "cannot load kubeconfig", "check $KUBECONFIG")
	}
	return clientcmd.Write(raw)
}

func (e *existing) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := e.Ensure(ctx, name, config.ClusterSpec{Provider: "existing"}); err != nil {
		return []diag.Finding{{Code: "CUBE-1101", Severity: diag.SeverityError,
			Message: err.Error(), Remediation: "verify kubeconfig and cluster health"}}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cluster/ -v`
Expected: all 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: ClusterProvider interface and existing-cluster provider"
```

---

### Task 5: kind provider with D10 two-layer merge + `config render-cluster`

**Files:**
- Create: `internal/cluster/kindp/kind.go`, `internal/cluster/kindp/merge.go`, `internal/cluster/kindp/testdata/{merged-typed.yaml,user-kind-config.yaml,merged-with-user.yaml}`, `cmd/config.go`
- Modify: `internal/cluster/provider.go` (replace the `newKind` panic stub with the real constructor)
- Test: `internal/cluster/kindp/merge_test.go`

**Interfaces:**
- Consumes: `config.ClusterSpec`, `config.GatewaySpec`, `diag`.
- Produces:

```go
package kindp
func New(gw config.GatewaySpec) *Kind                       // implements cluster.Provider
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec) ([]byte, error)
// RenderConfig is the D10 merge, pure and cluster-free — `cube-idp config
// render-cluster` and Ensure both call it. Merge rules (spec D10/§4.1):
//   base   = user providerConfig (file path or inline YAML) if set, else empty v1alpha4.Cluster
//   inject = gateway extraPortMapping (hostPort=gw.Port -> containerPort 443),
//            registry mirrors/insecure as containerdConfigPatches,
//            typed extraPorts + mounts on the control-plane node,
//            node image from kubernetesVersion (kindest/node:<version>)
//   conflict = user config already maps gw.Port to a different containerPort,
//              or sets a different node image than kubernetesVersion -> CUBE-1201
```

- In `internal/cluster/provider.go`, `newKind(gw)` returns `kindp.New(gw)` (delete the Task 4 panic stub).

- [ ] **Step 1: Write the failing merge tests**

`internal/cluster/kindp/merge_test.go`:

```go
package kindp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

var gw = config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderTypedFieldsOnly(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Registry: config.RegistrySpec{
			Mirrors:  map[string]string{"docker.io": "https://mirror.corp.example"},
			Insecure: []string{"registry.corp.example:5000"},
		},
		Mounts: []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
	}
	out, err := RenderConfig("dev", spec, gw)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-typed.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, golden(t, "merged-typed.yaml"))
	}
}

func TestRenderMergesUserProviderConfig(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    filepath.Join("testdata", "user-kind-config.yaml"),
	}
	out, err := RenderConfig("dev", spec, gw)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
	}
}

func TestRenderConflictOnGatewayPort(t *testing.T) {
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 9999
    hostPort: 8443
`
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1201" {
		t.Fatalf("want CUBE-1201 conflict, got %v", err)
	}
}
```

- [ ] **Step 2: Create golden fixtures**

`internal/cluster/kindp/testdata/user-kind-config.yaml`:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  labels:
    corp.example/zone: local
featureGates:
  UserNamespacesSupport: true
```

Golden files `merged-typed.yaml` / `merged-with-user.yaml`: on first run, generate them by temporarily writing `os.WriteFile(filepath.Join("testdata", name), out, 0o644)` in the test, eyeball the YAML (it must contain the gateway mapping `hostPort: 8443` / `containerPort: 443`, the containerd patch for the mirror, the mount, the extra port, node image `kindest/node:v1.33.1`, and — for the user-config case — the preserved label and feature gate), then remove the write and commit the fixtures. Golden review is a required human step, not a formality.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cluster/kindp/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 4: Implement merge.go**

```bash
go get sigs.k8s.io/kind@latest
```

`internal/cluster/kindp/merge.go`:

```go
package kindp

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

const gatewayContainerPort = 443

// RenderConfig performs the D10 two-layer merge and returns the final kind
// config YAML. It is pure: no docker, no cluster, fully unit-testable.
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec) ([]byte, error) {
	cfg, err := loadUserConfig(spec.ProviderConfig)
	if err != nil {
		return nil, err
	}
	cfg.Kind = "Cluster"
	cfg.APIVersion = "kind.x-k8s.io/v1alpha4"
	cfg.Name = name
	if len(cfg.Nodes) == 0 {
		cfg.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}
	}
	cp := &cfg.Nodes[0] // first node is the control-plane by convention

	image := "kindest/node:" + spec.KubernetesVersion
	if cp.Image != "" && cp.Image != image {
		return nil, diag.New("CUBE-1201",
			fmt.Sprintf("providerConfig sets node image %q but spec.cluster.kubernetesVersion implies %q", cp.Image, image),
			"remove the image from providerConfig or align kubernetesVersion; inspect with `cube-idp config render-cluster`")
	}
	cp.Image = image

	// Required injection: gateway port (spec D10 "injects only what it requires").
	for _, pm := range cp.ExtraPortMappings {
		if pm.HostPort == int32(gw.Port) && pm.ContainerPort != gatewayContainerPort {
			return nil, diag.New("CUBE-1201",
				fmt.Sprintf("providerConfig maps hostPort %d to containerPort %d, but cube-idp requires %d -> %d for the gateway",
					gw.Port, pm.ContainerPort, gw.Port, gatewayContainerPort),
				"remove that extraPortMapping or change spec.gateway.port; inspect with `cube-idp config render-cluster`")
		}
	}
	if !hasHostPort(cp.ExtraPortMappings, int32(gw.Port)) {
		cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
			ContainerPort: gatewayContainerPort, HostPort: int32(gw.Port), Protocol: v1alpha4.PortMappingProtocolTCP,
		})
	}

	// D10 layer-1 typed fields.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cp.ExtraPortMappings, p.HostPort) {
			return nil, diag.New("CUBE-1201",
				fmt.Sprintf("hostPort %d is mapped both in providerConfig and spec.cluster.extraPorts", p.HostPort),
				"keep exactly one of the two mappings")
		}
		cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
			ContainerPort: p.NodePort, HostPort: p.HostPort, Protocol: v1alpha4.PortMappingProtocolTCP,
		})
	}
	for _, m := range spec.Mounts {
		cp.ExtraMounts = append(cp.ExtraMounts, v1alpha4.Mount{HostPath: m.HostPath, ContainerPath: m.NodePath})
	}
	cfg.ContainerdConfigPatches = append(cfg.ContainerdConfigPatches, containerdPatches(spec.Registry)...)

	return yaml.Marshal(cfg)
}

func loadUserConfig(pc string) (*v1alpha4.Cluster, error) {
	var cfg v1alpha4.Cluster
	if pc == "" {
		return &cfg, nil
	}
	raw := []byte(pc)
	if !strings.Contains(pc, "\n") { // single line -> treat as file path
		b, err := os.ReadFile(pc)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-1202", fmt.Sprintf("cannot read providerConfig file %s", pc),
				"set spec.cluster.providerConfig to a readable kind config file or an inline YAML document")
		}
		raw = b
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diag.Wrap(err, "CUBE-1202", "providerConfig is not a valid kind Cluster document",
			"see https://kind.sigs.k8s.io/docs/user/configuration/")
	}
	return &cfg, nil
}

func hasHostPort(pms []v1alpha4.PortMapping, host int32) bool {
	for _, pm := range pms {
		if pm.HostPort == host {
			return true
		}
	}
	return false
}

func containerdPatches(r config.RegistrySpec) []string {
	var out []string
	for host, mirror := range r.Mirrors {
		out = append(out, fmt.Sprintf(
			"[plugins.\"io.containerd.grpc.v1.cri\".registry.mirrors.%q]\n  endpoint = [%q]", host, mirror))
	}
	for _, host := range r.Insecure {
		out = append(out, fmt.Sprintf(
			"[plugins.\"io.containerd.grpc.v1.cri\".registry.configs.%q.tls]\n  insecure_skip_verify = true", host))
	}
	return out
}
```

Note: map iteration order is random — sort mirror hostnames before emitting patches (`slices.Sorted(maps.Keys(r.Mirrors))`) so golden tests are deterministic.

- [ ] **Step 5: Implement kind.go (provider around the merge)**

`internal/cluster/kindp/kind.go`:

```go
package kindp

import (
	"context"
	"fmt"
	"slices"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

type Kind struct {
	gw       config.GatewaySpec
	provider *kindcluster.Provider
}

func New(gw config.GatewaySpec) *Kind {
	// DetectNodeProvider picks docker/podman/nerdctl (spec §4.1).
	np, _ := kindcluster.DetectNodeProvider()
	return &Kind{gw: gw, provider: kindcluster.NewProvider(np)}
}

func (k *Kind) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*cluster.Conn, error) {
	exists, err := k.Exists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		cfg, err := RenderConfig(name, spec, k.gw)
		if err != nil {
			return nil, err
		}
		err = k.provider.Create(name,
			kindcluster.CreateWithRawConfig(cfg),
			kindcluster.CreateWithWaitForReady(120*time.Second),
		)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-1203", "kind cluster creation failed",
				"check that the container runtime is running and has free resources; `cube-idp doctor` (Phase 2) will preflight this")
		}
	}
	kc, err := k.provider.KubeConfig(name, false)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1204", "cannot get kubeconfig from kind", "retry; if it persists, `cube-idp down` and `up` again")
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc))
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-1204", "kind kubeconfig is invalid", "delete the cluster with `cube-idp down` and retry")
	}
	return &cluster.Conn{Kubeconfig: []byte(kc), Context: "kind-" + name, REST: restCfg}, nil
}

func (k *Kind) Exists(ctx context.Context, name string) (bool, error) {
	names, err := k.provider.List()
	if err != nil {
		return false, diag.Wrap(err, "CUBE-1203", "cannot list kind clusters", "is the container runtime running?")
	}
	return slices.Contains(names, name), nil
}

func (k *Kind) Delete(ctx context.Context, name string) error {
	if err := k.provider.Delete(name, ""); err != nil {
		return diag.Wrap(err, "CUBE-1205", fmt.Sprintf("failed to delete kind cluster %q", name), "retry, or remove the container manually")
	}
	return nil
}

func (k *Kind) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	kc, err := k.provider.KubeConfig(name, false)
	return []byte(kc), err
}

func (k *Kind) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := k.provider.List(); err != nil {
		return []diag.Finding{{Code: "CUBE-1203", Severity: diag.SeverityError,
			Message: "container runtime unreachable: " + err.Error(),
			Remediation: "start Docker/Podman and retry"}}
	}
	return nil
}
```

Import-cycle note: `kindp` must not import `internal/cluster` if `cluster` imports `kindp`. Resolve by moving `Conn` into its own leaf package `internal/cluster/conn` OR (simpler) have the factory in `internal/cluster` do the import and `kindp` define its own return of `*cluster.Conn` — Go allows `cluster` → `kindp` → `cluster` only if broken. **Do the leaf-type move:** put `Conn` in `internal/kube` (`package kube`, fields as in Task 4), have both `cluster` and `kindp` import it, and update Task 4 signatures to `*kube.Conn`. The Provider interface stays in `internal/cluster`.

- [ ] **Step 6: Add `cube-idp config render-cluster`**

`cmd/config.go`:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/cluster/kindp"
	"github.com/rafpe/cube-idp/internal/config"
)

func newConfigCmd() *cobra.Command {
	var file string
	cfg := &cobra.Command{Use: "config", Short: "Inspect cube-idp configuration"}

	render := &cobra.Command{
		Use:   "render-cluster",
		Short: "Print the final merged provider config that `up` would create (D10)",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			if cube.Spec.Cluster.Provider != "kind" {
				return fmt.Errorf("render-cluster applies to provider: kind (got %q)", cube.Spec.Cluster.Provider)
			}
			out, err := kindp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), string(out))
			return nil
		},
	}
	cfg.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	cfg.AddCommand(render)
	return cfg
}
```

Register in `cmd/root.go`: `root.AddCommand(newConfigCmd())`.

- [ ] **Step 7: Run tests, generate + review goldens, run again**

Run: `go test ./internal/cluster/... -v && go build ./...`
Expected: PASS (after golden fixtures are reviewed and committed)

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: kind provider with D10 two-layer config merge and render-cluster command"
```

---

### Task 6: internal/apply — SSA Applier with inventory (envtest)

**Files:**
- Create: `internal/apply/applier.go`, `internal/apply/inventory.go`, `internal/apply/testenv_test.go`
- Test: `internal/apply/applier_test.go`

**Interfaces:**
- Consumes: `*rest.Config` (from `kube.Conn.REST`), `diag`.
- Produces:

```go
type Applier struct{ /* private */ }
func New(rest *rest.Config, cubeName string) (*Applier, error)
func (a *Applier) Apply(ctx context.Context, objs []*unstructured.Unstructured, wait bool, timeout time.Duration) error
	// labels every object cube-idp.dev/cube=<cubeName>, SSA-applies with
	// field manager "cube-idp", optionally waits via kstatus. Timeout -> CUBE-2001
	// wrapping the per-object status summary.
func (a *Applier) RecordInventory(ctx context.Context, objs []*unstructured.Unstructured) error
	// merges refs into ConfigMap cube-idp-inventory-<cube> in ns cube-idp-system
func (a *Applier) LoadInventory(ctx context.Context) ([]object.ObjMetadata, error)
func (a *Applier) DeleteAll(ctx context.Context, timeout time.Duration) error
	// deletes inventory objects in reverse apply order, skipping any object
	// annotated cube-idp.dev/prune=disabled; then deletes the inventory CM
func (a *Applier) Client() client.Client // reused by status/get-secrets commands
```

- [ ] **Step 1: envtest scaffolding**

```bash
go get github.com/fluxcd/pkg/ssa@latest sigs.k8s.io/controller-runtime@latest github.com/fluxcd/cli-utils@latest
make envtest-assets   # prints KUBEBUILDER_ASSETS path
```

`internal/apply/testenv_test.go`:

```go
package apply

import (
	"os"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testREST *rest.Config

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		// envtest binaries not installed; skip the whole package rather than fail.
		os.Exit(0)
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		panic(err)
	}
	testREST = cfg
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}
```

Add to `Makefile`:

```make
test-apply:
	KUBEBUILDER_ASSETS=$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path) \
	go test ./internal/apply/ -v
```

- [ ] **Step 2: Write the failing tests**

`internal/apply/applier_test.go`:

```go
package apply

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func cm(name, ns string, annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": ns},
		"data":     map[string]any{"k": "v"},
	}}
	if annotations != nil {
		u.SetAnnotations(annotations)
	}
	return u
}

func ns(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": name},
	}}
}

func TestApplyIsIdempotentAndLabels(t *testing.T) {
	a, err := New(testREST, "dev")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	objs := []*unstructured.Unstructured{ns("t1"), cm("a", "t1", nil)}
	for i := 0; i < 2; i++ { // second apply must be a clean no-op
		if err := a.Apply(ctx, objs, true, 30*time.Second); err != nil {
			t.Fatalf("apply #%d: %v", i+1, err)
		}
	}
	got := cm("a", "t1", nil)
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t1", Name: "a"}, got); err != nil {
		t.Fatal(err)
	}
	if got.GetLabels()["cube-idp.dev/cube"] != "dev" {
		t.Fatalf("cube label missing: %v", got.GetLabels())
	}
}

func TestInventoryRoundTripAndDeleteAll(t *testing.T) {
	a, _ := New(testREST, "dev2")
	ctx := context.Background()
	keep := cm("keep", "t2", map[string]string{"cube-idp.dev/prune": "disabled"})
	objs := []*unstructured.Unstructured{ns("t2"), cm("gone", "t2", nil), keep}
	if err := a.Apply(ctx, objs, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		t.Fatal(err)
	}
	inv, err := a.LoadInventory(ctx)
	if err != nil || len(inv) != 3 {
		t.Fatalf("inventory: %v %v", inv, err)
	}
	if err := a.DeleteAll(ctx, 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t2", Name: "gone"}, cm("gone", "t2", nil)); err == nil {
		t.Fatal("object 'gone' should have been pruned")
	}
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t2", Name: "keep"}, cm("keep", "t2", nil)); err != nil {
		t.Fatalf("annotated object must survive DeleteAll: %v", err)
	}
}
```

(Add `sigs.k8s.io/controller-runtime/pkg/client` import as `client`.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-apply`
Expected: FAIL (New undefined)

- [ ] **Step 4: Implement**

`internal/apply/applier.go`:

```go
// Package apply wraps fluxcd/pkg/ssa: server-side apply with field manager
// "cube-idp", kstatus waits with hard deadlines, and a ConfigMap inventory
// that powers down/prune (spec §4.1 Applier).
package apply

import (
	"context"
	"time"

	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/diag"
)

const (
	FieldManager    = "cube-idp"
	CubeLabel       = "cube-idp.dev/cube"
	PruneAnnotation = "cube-idp.dev/prune" // value "disabled" opts out
	SystemNamespace = "cube-idp-system"
)

type Applier struct {
	rm   *ssa.ResourceManager
	c    client.Client
	cube string
}

func New(cfg *rest.Config, cubeName string) (*Applier, error) {
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-2002", "cannot build cluster client", "check kubeconfig and connectivity")
	}
	poller := polling.NewStatusPoller(c, c.RESTMapper(), polling.Options{})
	rm := ssa.NewResourceManager(c, poller, ssa.Owner{Field: FieldManager, Group: "cube-idp.dev"})
	return &Applier{rm: rm, c: c, cube: cubeName}, nil
}

func (a *Applier) Client() client.Client { return a.c }

func (a *Applier) Apply(ctx context.Context, objs []*unstructured.Unstructured, wait bool, timeout time.Duration) error {
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[CubeLabel] = a.cube
		o.SetLabels(labels)
	}
	if _, err := a.rm.ApplyAllStaged(ctx, objs, ssa.DefaultApplyOptions()); err != nil {
		return diag.Wrap(err, "CUBE-2003", "server-side apply failed", "inspect the object in the error and re-run `cube-idp up`")
	}
	if !wait {
		return nil
	}
	if err := a.rm.WaitForSet(ssa.ObjectsToRef(objs), ssa.WaitOptions{Interval: 2 * time.Second, Timeout: timeout}); err != nil {
		return diag.Wrap(err, "CUBE-2001", "timed out waiting for resources to become ready",
			"re-run `cube-idp up` (idempotent); if it persists, inspect the resources named above with kubectl")
	}
	return nil
}
```

API-drift note: exact names in `fluxcd/pkg/ssa` and `fluxcd/cli-utils` may differ by version (`ApplyAllStaged`/`ApplyAll`, `ssa.ObjectsToRef`/`ssa.ToUnstructuredRefs`, poller constructor signature). Follow the versions Flux CLI itself uses (`go list -m -json github.com/fluxcd/flux2@latest` → copy its `pkg/ssa` usage from `flux install` source) and adjust mechanically; the tests define the behavior.

`internal/apply/inventory.go`:

```go
package apply

import (
	"context"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/cli-utils/pkg/object"

	"github.com/rafpe/cube-idp/internal/diag"
)

func (a *Applier) inventoryName() string { return "cube-idp-inventory-" + a.cube }

func (a *Applier) RecordInventory(ctx context.Context, objs []*unstructured.Unstructured) error {
	refs := object.UnstructuredSetToObjMetadataSet(objs)
	existing, _ := a.LoadInventory(ctx)
	merged := object.ObjMetadataSet(existing).Union(refs)
	payload, _ := json.Marshal(merged.Strings()) // triple-delimited GKNN strings
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: a.inventoryName(), Namespace: SystemNamespace,
			Labels: map[string]string{CubeLabel: a.cube}},
		Data: map[string]string{"inventory": string(payload)},
	}
	nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: SystemNamespace}}
	_ = a.c.Create(ctx, nsObj) // ignore AlreadyExists
	if err := a.c.Update(ctx, cm); apierrors.IsNotFound(err) {
		return a.c.Create(ctx, cm)
	} else if err != nil {
		return diag.Wrap(err, "CUBE-2004", "cannot write inventory", "check RBAC on namespace cube-idp-system")
	}
	return nil
}

func (a *Applier) LoadInventory(ctx context.Context) ([]object.ObjMetadata, error) {
	var cm corev1.ConfigMap
	err := a.c.Get(ctx, client.ObjectKey{Namespace: SystemNamespace, Name: a.inventoryName()}, &cm)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-2004", "cannot read inventory", "check RBAC on namespace cube-idp-system")
	}
	var strs []string
	if err := json.Unmarshal([]byte(cm.Data["inventory"]), &strs); err != nil {
		return nil, diag.Wrap(err, "CUBE-2004", "inventory is corrupt", "delete the ConfigMap and re-run `cube-idp up` to rebuild it")
	}
	return object.FromStringList(strs) // adjust to actual cli-utils helper name
}

func (a *Applier) DeleteAll(ctx context.Context, timeout time.Duration) error {
	refs, err := a.LoadInventory(ctx)
	if err != nil {
		return err
	}
	var deletable []object.ObjMetadata
	for _, ref := range refs {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(ref.GroupKind.WithVersion("")) // resolved via RESTMapper by client
		// fetch to honor the prune opt-out annotation
		obj, getErr := a.getByRef(ctx, ref)
		if getErr != nil {
			continue // already gone
		}
		if obj.GetAnnotations()[PruneAnnotation] == "disabled" {
			continue
		}
		deletable = append(deletable, ref)
	}
	// reverse order: dependents (applied later) go first
	for i := len(deletable) - 1; i >= 0; i-- {
		if obj, err := a.getByRef(ctx, deletable[i]); err == nil {
			_ = a.c.Delete(ctx, obj)
		}
	}
	// finally remove the inventory itself
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: a.inventoryName(), Namespace: SystemNamespace}}
	_ = a.c.Delete(ctx, cm)
	return nil
}
```

`getByRef` helper: build `*unstructured.Unstructured`, resolve version via `a.c.RESTMapper().RESTMapping(ref.GroupKind)`, `SetGroupVersionKind`, then `a.c.Get`. ~15 lines.

- [ ] **Step 5: Run tests**

Run: `make test-apply`
Expected: both tests PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: SSA applier with kstatus waits, inventory, and annotated prune opt-out"
```

---

### Task 7: internal/registry — embedded zot + port-forward

**Files:**
- Create: `internal/registry/manifests/zot.yaml`, `internal/registry/zot.go`, `internal/registry/portforward.go`
- Test: `internal/registry/zot_test.go`

**Interfaces:**
- Consumes: `apply.Applier`, `kube.Conn`.
- Produces:

```go
const InClusterURL = "zot.cube-idp-system.svc.cluster.local:5000" // engines reference this
func Manifests() ([]*unstructured.Unstructured, error)            // parsed embedded YAML
func Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error
func PortForward(ctx context.Context, rest *rest.Config) (localAddr string, stop func(), err error)
	// forwards a free local port to svc/zot:5000; used by the pack pusher
```

- [ ] **Step 1: Write the failing test**

`internal/registry/zot_test.go`:

```go
package registry

import "testing"

func TestManifestsParseAndTarget(t *testing.T) {
	objs, err := Manifests()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, o := range objs {
		kinds[o.GetKind()] = true
		if o.GetKind() != "Namespace" && o.GetNamespace() != "cube-idp-system" {
			t.Fatalf("%s/%s must live in cube-idp-system", o.GetKind(), o.GetName())
		}
	}
	for _, want := range []string{"Namespace", "Deployment", "Service"} {
		if !kinds[want] {
			t.Fatalf("missing %s in zot manifests", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**

`internal/registry/manifests/zot.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: cube-idp-system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zot
  namespace: cube-idp-system
spec:
  replicas: 1
  selector:
    matchLabels: {app: zot}
  template:
    metadata:
      labels: {app: zot}
    spec:
      containers:
        - name: zot
          image: ghcr.io/project-zot/zot-linux-amd64:v2.1.2
          ports: [{containerPort: 5000}]
          volumeMounts: [{name: data, mountPath: /var/lib/registry}]
          readinessProbe:
            httpGet: {path: /v2/, port: 5000}
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: zot
  namespace: cube-idp-system
spec:
  selector: {app: zot}
  ports: [{port: 5000, targetPort: 5000}]
```

(Arch note: on arm64 kind nodes use `ghcr.io/project-zot/zot-linux-arm64`; if a multi-arch tag `ghcr.io/project-zot/zot:v2.1.2` resolves at implementation time, prefer it. emptyDir is fine for MVP: artifacts are re-pushed by every `up`.)

`internal/registry/zot.go`:

```go
// Package registry embeds and installs the in-cluster zot OCI registry —
// the delivery bus between cube-idp's client-side rendering and the GitOps
// engine (spec §4, "OCI push").
package registry

import (
	"context"
	_ "embed"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

const InClusterURL = "zot.cube-idp-system.svc.cluster.local:5000"

//go:embed manifests/zot.yaml
var zotYAML []byte

func Manifests() ([]*unstructured.Unstructured, error) {
	return apply.ParseMultiDoc(zotYAML) // shared helper, see below
}

func Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := Manifests()
	if err != nil {
		return diag.Wrap(err, "CUBE-5001", "embedded zot manifests are invalid", "this is a cube-idp bug — please report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}
```

Add the shared multi-doc parser to `internal/apply/applier.go` (it belongs with the apply types):

```go
// ParseMultiDoc splits multi-document YAML into unstructured objects,
// skipping empty documents.
func ParseMultiDoc(data []byte) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := yaml.Unmarshal(doc, &m); err != nil {
			return nil, err
		}
		if len(m) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: m})
	}
}
```

(Imports: `bufio`, `bytes`, `io`, `sigs.k8s.io/yaml`, `utilyaml "k8s.io/apimachinery/pkg/util/yaml"`.)

`internal/registry/portforward.go` — standard client-go port-forward (used by Task 9's pusher and testable only e2e):

```go
package registry

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/rafpe/cube-idp/internal/diag"
)

// PortForward tunnels a free local port to the zot pod and returns
// "127.0.0.1:<port>". stop() must be deferred by the caller.
func PortForward(ctx context.Context, cfg *rest.Config) (string, func(), error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	pods, err := cs.CoreV1().Pods("cube-idp-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app=zot", FieldSelector: "status.phase=" + string(corev1.PodRunning)})
	if err != nil || len(pods.Items) == 0 {
		return "", nil, diag.Wrap(err, "CUBE-5002", "no running zot pod to port-forward to",
			"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
	}
	req := cs.CoreV1().RESTClient().Post().Resource("pods").
		Namespace("cube-idp-system").Name(pods.Items[0].Name).SubResource("portforward")
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	stopCh, readyCh := make(chan struct{}), make(chan struct{})
	fw, err := portforward.New(dialer, []string{"0:5000"}, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", nil, err
	}
	go func() { _ = fw.ForwardPorts() }()
	select {
	case <-readyCh:
	case <-ctx.Done():
		close(stopCh)
		return "", nil, ctx.Err()
	}
	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return "", nil, diag.Wrap(err, "CUBE-5002", "port-forward to zot failed", "retry `cube-idp up`")
	}
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local), func() { close(stopCh) }, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/registry/ ./internal/apply/ -v` (apply package skips without envtest assets; registry test must PASS)

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: embedded zot registry install and port-forward tunnel"
```

---

### Task 8: internal/pack — model, sources, values validation, rendering

**Files:**
- Create: `internal/pack/pack.go`, `internal/pack/source.go`, `internal/pack/render.go`, `internal/pack/helm.go`, `internal/pack/testdata/demo/{pack.cue,manifests/cm.yaml}`, `internal/pack/testdata/demo-helm/{pack.cue,chart.yaml}`
- Test: `internal/pack/pack_test.go`

**Interfaces:**
- Consumes: `diag`, `apply.ParseMultiDoc`.
- Produces:

```go
type Pack struct{ Name, Version, Dir string }        // fetched + validated metadata
type Rendered struct {
	Name    string
	Version string
	Objects []*unstructured.Unstructured             // final manifests to deliver
}
func Fetch(ctx context.Context, ref string, cacheDir string) (*Pack, error)
	// ref forms (spec §4.4 MVP): local dir path | oci://host/repo:tag
	// git refs land Phase 2. Unknown scheme -> CUBE-4001.
func (p *Pack) Render(values map[string]any) (*Rendered, error)
	// 1. validate values against pack.cue #Values -> CUBE-4002 on mismatch
	// 2. if manifests/ exists: parse raw YAML docs
	// 3. if chart.yaml exists: helm-render (helm.go) and append
```

Pack format (documented in the code and README): a directory with `pack.cue` (required: `name`, `version`; optional `#Values` schema) plus `manifests/*.yaml` and/or `chart.yaml`:

```yaml
# chart.yaml — helm chart reference rendered client-side (spec §4: engines
# receive rendered manifests; helm-controller is NOT installed)
chart: traefik
repo: https://traefik.github.io/charts   # or oci://registry/chart
version: "34.1.0"
releaseName: traefik
namespace: traefik
```

- [ ] **Step 1: Create fixtures**

`internal/pack/testdata/demo/pack.cue`:

```cue
name:    "demo"
version: "0.1.0"
#Values: {
	replicas: int & >0 | *1
	message:  string | *"hello"
}
```

`internal/pack/testdata/demo/manifests/cm.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: default
data:
  message: placeholder
```

`internal/pack/testdata/demo-helm/pack.cue`: `name: "demo-helm"`, `version: "0.1.0"`. `chart.yaml`: use a tiny well-known chart pinned by version (e.g. `podinfo` from `oci://ghcr.io/stefanprodan/charts/podinfo`, version `6.7.1`) — the helm-render test hits the network; guard it with `testing.Short()`.

- [ ] **Step 2: Write the failing tests**

`internal/pack/pack_test.go`:

```go
package pack

import (
	"context"
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestFetchLocalDirAndMetadata(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Version != "0.1.0" {
		t.Fatalf("metadata: %+v", p)
	}
}

func TestFetchUnknownScheme(t *testing.T) {
	_, err := Fetch(context.Background(), "svn://old/school", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4001" {
		t.Fatalf("want CUBE-4001, got %v", err)
	}
}

func TestRenderValidatesValues(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	_, err := p.Render(map[string]any{"replicas": -3})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4002" {
		t.Fatalf("want CUBE-4002, got %v", err)
	}
}

func TestRenderManifests(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	r, err := p.Render(map[string]any{"replicas": 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Objects) != 1 || r.Objects[0].GetKind() != "ConfigMap" {
		t.Fatalf("objects: %+v", r.Objects)
	}
}

func TestRenderHelmChart(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	p, err := Fetch(context.Background(), "testdata/demo-helm", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range r.Objects {
		if o.GetKind() == "Deployment" {
			found = true
		}
	}
	if !found {
		t.Fatal("helm render produced no Deployment")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/pack/ -short -v`
Expected: FAIL

- [ ] **Step 4: Implement pack.go + source.go + render.go**

```bash
go get oras.land/oras-go/v2@latest
go get helm.sh/helm/v4@latest   # fall back to helm.sh/helm/v3 if unresolvable (plan header note)
```

`internal/pack/pack.go`:

```go
// Package pack implements cube-idp's extensibility tier 1 (spec §4.4):
// data-only directories with pack.cue metadata, fetched from local dirs or
// OCI, values-validated with CUE, rendered in-process.
package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
)

type Pack struct {
	Name    string
	Version string
	Dir     string
}

type Rendered struct {
	Name    string
	Version string
	Objects []*unstructured.Unstructured
}

func loadMeta(dir string) (*Pack, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pack.cue"))
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4003", fmt.Sprintf("pack at %s has no pack.cue", dir),
			"every pack needs a pack.cue with at least name and version")
	}
	v := cuecontext.New().CompileBytes(raw)
	if v.Err() != nil {
		return nil, diag.Wrap(v.Err(), "CUBE-4003", "pack.cue does not compile", "fix the CUE syntax")
	}
	p := &Pack{Dir: dir}
	if err := v.LookupPath(cue.ParsePath("name")).Decode(&p.Name); err != nil || p.Name == "" {
		return nil, diag.New("CUBE-4003", "pack.cue is missing 'name'", "add: name: \"<pack-name>\"")
	}
	if err := v.LookupPath(cue.ParsePath("version")).Decode(&p.Version); err != nil || p.Version == "" {
		return nil, diag.New("CUBE-4003", "pack.cue is missing 'version'", "add: version: \"0.1.0\"")
	}
	return p, nil
}

// validateValues unifies user values with #Values (if declared) and returns
// the concrete, defaulted value map.
func (p *Pack) validateValues(values map[string]any) (map[string]any, error) {
	raw, _ := os.ReadFile(filepath.Join(p.Dir, "pack.cue"))
	root := cuecontext.New().CompileBytes(raw)
	schema := root.LookupPath(cue.ParsePath("#Values"))
	if !schema.Exists() {
		return values, nil
	}
	unified := schema.Unify(root.Context().Encode(values))
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return nil, diag.Wrap(err, "CUBE-4002",
			fmt.Sprintf("values for pack %q do not match its #Values schema", p.Name),
			"compare your values with the pack's pack.cue #Values definition")
	}
	var out map[string]any
	if err := unified.Decode(&out); err != nil {
		return nil, diag.Wrap(err, "CUBE-4002", "cannot decode validated values", "simplify the values to plain YAML types")
	}
	return out, nil
}
```

`internal/pack/source.go`:

```go
package pack

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rafpe/cube-idp/internal/diag"
)

func Fetch(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		dir, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
		if err != nil {
			return nil, err
		}
		return loadMeta(dir)
	case strings.Contains(ref, "://"):
		return nil, diag.New("CUBE-4001", fmt.Sprintf("unsupported pack ref scheme in %q", ref),
			"use a local directory path or oci://host/repo:tag (git refs arrive in Phase 2)")
	default: // local directory
		abs, err := filepath.Abs(ref)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-4001", "bad pack path", "use a valid directory path")
		}
		return loadMeta(abs)
	}
}
```

`pullOCI` (same file): use `oras.land/oras-go/v2` — `remote.NewRepository(ref)`, `oras.Copy` into an `oci.NewFromFS`/file store under `cacheDir/<repo>@<digest>`, return the extracted dir. If the artifact is a Flux-style gzipped tarball layer, untar it. ~40 lines; mirror the pull example in the oras-go README. Registry auth: anonymous only in Phase 1; `Insecure` transport when the host is `127.0.0.1` (needed for the zot tunnel).

`internal/pack/render.go`:

```go
package pack

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

func (p *Pack) Render(values map[string]any) (*Rendered, error) {
	vals, err := p.validateValues(values)
	if err != nil {
		return nil, err
	}
	r := &Rendered{Name: p.Name, Version: p.Version}

	manifestsDir := filepath.Join(p.Dir, "manifests")
	if entries, err := os.ReadDir(manifestsDir); err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || (filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(manifestsDir, e.Name()))
			if err != nil {
				return nil, diag.Wrap(err, "CUBE-4004", "cannot read pack manifest "+e.Name(), "check file permissions")
			}
			objs, err := apply.ParseMultiDoc(raw)
			if err != nil {
				return nil, diag.Wrap(err, "CUBE-4004", p.Name+"/"+e.Name()+" is not valid YAML", "fix the manifest")
			}
			r.Objects = append(r.Objects, objs...)
		}
	}

	if _, err := os.Stat(filepath.Join(p.Dir, "chart.yaml")); err == nil {
		objs, err := renderHelm(p.Dir, vals)
		if err != nil {
			return nil, err
		}
		r.Objects = append(r.Objects, objs...)
	}

	if len(r.Objects) == 0 {
		return nil, diag.New("CUBE-4004", "pack "+p.Name+" rendered zero objects",
			"a pack needs manifests/ and/or chart.yaml")
	}
	return r, nil
}
```

`internal/pack/helm.go` — the ONLY file importing helm (plan-header risk rule):

```go
package pack

// renderHelm reads chart.yaml next to pack.cue, pulls the pinned chart, and
// template-renders it client-side with the pack values merged in. Returns
// unstructured objects. Uses the Helm SDK action package with DryRun +
// ClientOnly (no cluster access, no install):
//   settings := cli.New()
//   cfg := new(action.Configuration)                 // zero config: client-only
//   install := action.NewInstall(cfg)
//   install.DryRun, install.ClientOnly, install.Replace = true, true, true
//   install.ReleaseName, install.Namespace = ref.ReleaseName, ref.Namespace
//   install.ChartPathOptions.RepoURL, .Version = ref.Repo, ref.Version
//   chartPath, _ := install.ChartPathOptions.LocateChart(ref.Chart, settings)
//   chart, _ := loader.Load(chartPath)
//   rel, err := install.Run(chart, values["helm"] or values)
//   -> apply.ParseMultiDoc([]byte(rel.Manifest))
// Failure -> diag.Wrap(err, "CUBE-4005", "helm render failed for pack ...",
//   "check chart repo/version in chart.yaml; try `helm template` manually").
// Namespace handling: prepend a Namespace object for ref.Namespace when the
// chart does not emit one; set install.CreateNamespace = false (we manage it).
```

Write the real ~70-line implementation following that recipe; the comment is the contract. `chart.yaml` decodes into:

```go
type chartRef struct {
	Chart       string `yaml:"chart"`
	Repo        string `yaml:"repo"`
	Version     string `yaml:"version"`
	ReleaseName string `yaml:"releaseName"`
	Namespace   string `yaml:"namespace"`
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/pack/ -short -v` (then once without `-short` to exercise the podinfo helm render)
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: pack engine — fetch, CUE values validation, manifest and helm rendering"
```

---

### Task 9: GitOpsEngine interface + Flux implementation + OCI push

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/flux/flux.go`, `internal/engine/flux/deliver.go`, `internal/engine/flux/manifests/install.yaml` (generated), `internal/oci/push.go`, `hack/gen-flux-manifests.sh`
- Test: `internal/engine/engine_test.go`, `internal/engine/flux/flux_test.go`

**Interfaces:**
- Consumes: `apply.Applier`, `registry.InClusterURL`, `pack.Rendered`, `diag`.
- Produces:

```go
package engine
type ArtifactRef struct{ Repo, Tag string } // e.g. {"packs/gitea", "0.1.0"}
type ComponentHealth struct{ Name string; Ready bool; Message string }
type Engine interface {
	Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error
	Deliver(ctx context.Context, r *pack.Rendered, src ArtifactRef) ([]*unstructured.Unstructured, error)
	// Deliver RETURNS engine-native objects; the caller applies them via the
	// Applier (keeps Deliver pure/testable and one apply path).
	Health(ctx context.Context, a *apply.Applier) ([]ComponentHealth, error)
	Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error
}
func New(typ string) (Engine, error) // "flux" -> flux.New(); "argocd" -> CUBE-3002; else CUBE-3001

package oci
func PushRendered(ctx context.Context, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)
	// writes r.Objects as one all.yaml into a tmp dir, pushes it as a
	// Flux-compatible OCI artifact to <registryAddr>/packs/<name>:<version>
	// using github.com/fluxcd/pkg/oci client (plain HTTP for 127.0.0.1)
```

- [ ] **Step 1: Generate Flux install manifests**

`hack/gen-flux-manifests.sh`:

```bash
#!/usr/bin/env bash
# Regenerates the embedded Flux install manifests (spec: pre-rendered at
# build time so the runtime needs no external binaries, works offline).
# Requires the flux CLI: https://fluxcd.io/flux/installation/
set -euo pipefail
cd "$(dirname "$0")/.."
flux install --export \
  --components=source-controller,kustomize-controller \
  > internal/engine/flux/manifests/install.yaml
echo "wrote internal/engine/flux/manifests/install.yaml"
```

Run it (`brew install fluxcd/tap/flux` if needed): only source-controller (OCIRepository) and kustomize-controller (Kustomization apply) are required — helm rendering is client-side (Task 8), so helm-controller stays out.

- [ ] **Step 2: Write the failing tests**

`internal/engine/engine_test.go`:

```go
package engine

import (
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestFactoryFlux(t *testing.T) {
	if _, err := New("flux"); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryArgoCDPhase2(t *testing.T) {
	_, err := New("argocd")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-3002" {
		t.Fatalf("want CUBE-3002 (argocd ships in Phase 2, D2), got %v", err)
	}
}

func TestFactoryUnknown(t *testing.T) {
	_, err := New("jenkins")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-3001" {
		t.Fatalf("want CUBE-3001, got %v", err)
	}
}
```

`internal/engine/flux/flux_test.go`:

```go
package flux

import (
	"context"
	"testing"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

func TestDeliverShapesFluxObjects(t *testing.T) {
	f := New()
	r := &pack.Rendered{Name: "gitea", Version: "0.1.0"}
	objs, err := f.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/gitea", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want OCIRepository + Kustomization, got %d objects", len(objs))
	}
	repo, kust := objs[0], objs[1]
	if repo.GetKind() != "OCIRepository" || kust.GetKind() != "Kustomization" {
		t.Fatalf("kinds: %s, %s", repo.GetKind(), kust.GetKind())
	}
	url, _, _ := unstructuredNestedString(repo, "spec", "url")
	if url != "oci://zot.cube-idp-system.svc.cluster.local:5000/packs/gitea" {
		t.Fatalf("url: %s", url)
	}
	insecure, _, _ := unstructuredNestedBool(repo, "spec", "insecure")
	if !insecure {
		t.Fatal("in-cluster zot is plain HTTP; spec.insecure must be true")
	}
	prune, _, _ := unstructuredNestedBool(kust, "spec", "prune")
	if !prune {
		t.Fatal("Kustomization.spec.prune must be true")
	}
	src, _, _ := unstructuredNestedString(kust, "spec", "sourceRef", "kind")
	if src != "OCIRepository" {
		t.Fatalf("sourceRef.kind: %s", src)
	}
}

func TestInstallManifestsEmbedAndParse(t *testing.T) {
	objs, err := InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) < 10 {
		t.Fatalf("flux install.yaml seems empty: %d objects — run hack/gen-flux-manifests.sh", len(objs))
	}
}
```

(`unstructuredNestedString/Bool` = small local wrappers over `unstructured.NestedString/NestedBool` on `.Object` — 5 lines each in the test file.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/engine/... -v`
Expected: FAIL

- [ ] **Step 4: Implement**

`internal/engine/engine.go`:

```go
// Package engine defines the GitOpsEngine seam (spec §4.1, D2). Flux ships
// in Phase 1; Argo CD in Phase 2; both are compiled in — no plugins (D8).
// Engine types never leak above this interface: packs describe intent,
// engines translate.
package engine

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/pack"
)

type ArtifactRef struct{ Repo, Tag string }

type ComponentHealth struct {
	Name    string
	Ready   bool
	Message string
}

type Engine interface {
	Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error
	Deliver(ctx context.Context, r *pack.Rendered, src ArtifactRef) ([]*unstructured.Unstructured, error)
	Health(ctx context.Context, a *apply.Applier) ([]ComponentHealth, error)
	Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error
}

// New is the factory. It lives here (not in cmd/) so the up orchestrator and
// future commands share one construction path.
func New(typ string) (Engine, error) {
	switch typ {
	case "flux":
		return newFlux(), nil
	case "argocd":
		return nil, diag.New("CUBE-3002", "the argocd engine ships in Phase 2 (D2)",
			"use engine.type: flux for now; argocd is available as a UI pack today")
	default:
		return nil, diag.New("CUBE-3001", fmt.Sprintf("unknown engine type %q", typ),
			"use engine.type: flux or argocd")
	}
}
```

Import-cycle note: `engine.New` returning the flux implementation requires `engine` → `flux` while `flux` imports `engine` for the types. Break it the standard Go way: declare `var newFlux func() Engine` in `engine.go` and register it from `flux` via a blank-import + `init()` — OR (preferred, simpler) move the factory into a tiny `internal/engine/factory/factory.go` package that imports both. Choose the factory-package option; the tests above then live in `factory` and import both packages.

`internal/engine/flux/flux.go`:

```go
// Package flux implements the GitOpsEngine over Flux's source-controller and
// kustomize-controller. Delivery shape: one OCIRepository + one Kustomization
// per pack, pointing at the in-cluster zot registry (spec §4.1, §4.3).
package flux

import (
	"context"
	_ "embed"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
)

//go:embed manifests/install.yaml
var installYAML []byte

type Flux struct{}

func New() *Flux { return &Flux{} }

func InstallManifests() ([]*unstructured.Unstructured, error) {
	return apply.ParseMultiDoc(installYAML)
}

func (f *Flux) Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := InstallManifests()
	if err != nil {
		return diag.Wrap(err, "CUBE-3003", "embedded flux manifests are invalid",
			"this is a cube-idp bug — regenerate with hack/gen-flux-manifests.sh and report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}

func (f *Flux) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	// Engine removal is inventory-driven like everything else; `down`
	// deletes the whole inventory, so nothing engine-specific is needed
	// in Phase 1 beyond being present in the inventory.
	return nil
}
```

`internal/engine/flux/deliver.go`:

```go
package flux

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

const fluxNS = "flux-system"

func (f *Flux) Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-" + r.Name
	repo := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "OCIRepository",
		"metadata":   map[string]any{"name": name, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "1m",
			"url":      fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
			"ref":      map[string]any{"tag": src.Tag},
			"insecure": true, // zot is plain HTTP inside the cluster
		},
	}}
	kust := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": name, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "10m",
			"prune":    true,
			"wait":     true,
			"timeout":  "5m",
			"path":     "./",
			"sourceRef": map[string]any{
				"kind": "OCIRepository",
				"name": name,
			},
		},
	}}
	return []*unstructured.Unstructured{repo, kust}, nil
}
```

(API-version note: `OCIRepository` may still be `v1beta2` in the generated install.yaml — grep it and match; the test asserts kind/url/insecure/prune, not apiVersion.)

`Health`: list `Kustomization` objects in `flux-system` with the cube label via `a.Client()`, read the `Ready` condition into `[]engine.ComponentHealth`. ~30 lines.

`internal/oci/push.go`:

```go
// Package oci pushes rendered packs as Flux-compatible OCI artifacts.
package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	fluxoci "github.com/fluxcd/pkg/oci/client"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

func PushRendered(ctx context.Context, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error) {
	dir, err := os.MkdirTemp("", "cube-idp-artifact-*")
	if err != nil {
		return engine.ArtifactRef{}, err
	}
	defer os.RemoveAll(dir)
	var buf []byte
	for _, o := range r.Objects {
		y, err := yaml.Marshal(o.Object)
		if err != nil {
			return engine.ArtifactRef{}, err
		}
		buf = append(buf, []byte("---\n")...)
		buf = append(buf, y...)
	}
	if err := os.WriteFile(filepath.Join(dir, "all.yaml"), buf, 0o644); err != nil {
		return engine.ArtifactRef{}, err
	}

	ref := engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}
	url := fmt.Sprintf("oci://%s/%s:%s", registryAddr, ref.Repo, ref.Tag)
	c := fluxoci.NewClient(fluxoci.DefaultOptions()) // add insecure/plain-HTTP option for 127.0.0.1 per pkg/oci docs
	if _, err := c.Push(ctx, url, dir, fluxoci.WithPushMetadata(fluxoci.Metadata{
		Source: "cube-idp", Revision: r.Version,
	})); err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, "CUBE-5003",
			fmt.Sprintf("failed to push pack %q to the in-cluster registry", r.Name),
			"re-run `cube-idp up`; check that the zot pod is running")
	}
	return ref, nil
}
```

(API-drift note as in Task 6: match the `fluxcd/pkg/oci` client signatures of the installed version — the flux CLI's `flux push artifact` command source is the reference implementation.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/engine/... -v && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: GitOpsEngine interface, Flux implementation, OCI artifact push"
```

---

### Task 10: `up` orchestrator + command

**Files:**
- Create: `internal/up/up.go`, `cmd/up.go`, `cmd/init.go`
- Test: compile-level only (`go build ./...`); behavior is covered by Task 12's e2e — the orchestrator is intentionally a thin sequence of already-tested units.

**Interfaces:**
- Consumes: everything above.
- Produces: `up.Run(ctx context.Context, cfgPath string, out io.Writer) error` — cmd/up.go stays a flag-parsing shell.

- [ ] **Step 1: Implement the orchestrator**

`internal/up/up.go` — the spec §4.3 sequence, one readable function:

```go
// Package up orchestrates `cube-idp up` (spec §4.3). It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

const (
	clusterTimeout = 3 * time.Minute
	applyTimeout   = 2 * time.Minute
	healthTimeout  = 5 * time.Minute
)

func Run(ctx context.Context, cfgPath string, out io.Writer) error {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	step(out, "config", "cube %q loaded and validated", cube.Metadata.Name)

	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return err
	}
	conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
	if err != nil {
		return err
	}
	step(out, "cluster", "%s cluster ready (context %s)", cube.Spec.Cluster.Provider, conn.Context)

	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return err
	}
	eng, err := enginefactory.New(cube.Spec.Engine.Type)
	if err != nil {
		return err
	}

	if err := registry.Install(ctx, a, applyTimeout); err != nil {
		return err
	}
	regObjs, _ := registry.Manifests()
	if err := a.RecordInventory(ctx, regObjs); err != nil {
		return err
	}
	step(out, "registry", "zot ready at %s", registry.InClusterURL)

	if err := eng.Install(ctx, a, applyTimeout); err != nil {
		return err
	}
	step(out, "engine", "%s installed", cube.Spec.Engine.Type)

	tunnelAddr, stop, err := registry.PortForward(ctx, conn.REST)
	if err != nil {
		return err
	}
	defer stop()

	refs := cube.Spec.Packs
	refs = append([]config.PackRef{{Ref: "packs/" + cube.Spec.Gateway.Pack}}, refs...) // gateway first
	for _, pr := range refs {
		p, err := pack.Fetch(ctx, pr.Ref, cacheDir())
		if err != nil {
			return err
		}
		rendered, err := p.Render(pr.Values)
		if err != nil {
			return err
		}
		artifact, err := oci.PushRendered(ctx, rendered, tunnelAddr)
		if err != nil {
			return err
		}
		deliverObjs, err := eng.Deliver(ctx, rendered, artifact)
		if err != nil {
			return err
		}
		if err := a.Apply(ctx, deliverObjs, false, applyTimeout); err != nil {
			return err
		}
		if err := a.RecordInventory(ctx, deliverObjs); err != nil {
			return err
		}
		step(out, "pack", "%s@%s delivered", rendered.Name, rendered.Version)
	}

	if err := waitHealthy(ctx, eng, a, out, healthTimeout); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets\n",
		cube.Metadata.Name, cube.Spec.Gateway.Host, cube.Spec.Gateway.Port)
	return nil
}
```

Helpers in the same file: `step(w, stage, format, ...)` prints `▸ [stage] message`; `cacheDir()` returns `$HOME/.cache/cube-idp/packs` (create with MkdirAll); `waitHealthy` polls `eng.Health` every 5s until all ready or deadline → `CUBE-3004` listing the unready components and their messages (the "no infinite spinner" rule: include each component's Message in the error).

Also record the engine install objects in the inventory (`InstallManifests()` result) right after `eng.Install` — down must remove Flux too.

Note the gateway pack resolution: `"packs/" + pack name` resolves to the repo-local `packs/traefik` directory when running from a checkout; Task 11 wires the released form (`oci://ghcr.io/cube-idp/packs/<name>` fallback). MVP: local path is fine and exercised by e2e.

- [ ] **Step 2: Wire the commands**

`cmd/up.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/up"
)

func newUpCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "up",
		Short: "Create/ensure the cluster, install the engine, deliver all packs, exit",
		RunE: func(c *cobra.Command, _ []string) error {
			return up.Run(c.Context(), file, c.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
```

`cmd/init.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/config"
)

func newInitCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "init",
		Short: "Write the default cube.yaml (kind + flux + traefik + gitea + argocd, D9)",
		RunE: func(c *cobra.Command, _ []string) error {
			if _, err := os.Stat("cube.yaml"); err == nil {
				return fmt.Errorf("cube.yaml already exists — refusing to overwrite")
			}
			out, err := yaml.Marshal(config.Default(name))
			if err != nil {
				return err
			}
			if err := os.WriteFile("cube.yaml", out, 0o644); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "wrote cube.yaml — run `cube-idp up`")
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "dev", "cube name")
	return c
}
```

Register both in `cmd/root.go` (`root.AddCommand(newUpCmd(), newInitCmd())`). Add `c.SetContext` signal handling in `main.go`: wrap with `signal.NotifyContext(context.Background(), os.Interrupt)` and pass via `cmd.ExecuteContext(ctx)` (change `Execute` accordingly).

- [ ] **Step 3: Build and commit**

Run: `go build ./... && go test ./... -short`
Expected: clean

```bash
git add -A && git commit -m "feat: up orchestrator, up/init commands, signal handling"
```

---

### Task 11: down, status, get secrets commands

**Files:**
- Create: `cmd/down.go`, `cmd/status.go`, `cmd/get.go`
- Test: `cmd/get_test.go` (secret filtering logic factored into a testable function)

**Interfaces:**
- Consumes: `config.Load`, `cluster.New`, `apply.Applier`, engine factory.
- Produces: user-facing commands; `cmd.filterCLISecrets([]corev1.Secret) []secretRow` (unit-tested).

- [ ] **Step 1: Implement down**

`cmd/down.go`:

```go
package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
)

func newDownCmd() *cobra.Command {
	var file string
	var keepCluster bool
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete everything cube-idp created (inventory-driven cascade), then the cluster",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// existing clusters: remove only cube-idp-managed resources (spec §4.3)
			if cube.Spec.Cluster.Provider == "existing" || keepCluster {
				conn, err := prov.Ensure(c.Context(), cube.Metadata.Name, cube.Spec.Cluster)
				if err != nil {
					return err
				}
				a, err := apply.New(conn.REST, cube.Metadata.Name)
				if err != nil {
					return err
				}
				return a.DeleteAll(c.Context(), 5*time.Minute)
			}
			// kind: deleting the cluster IS the cascade
			return prov.Delete(c.Context(), cube.Metadata.Name)
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&keepCluster, "keep-cluster", false, "delete cube-idp resources but keep the cluster")
	return c
}
```

- [ ] **Step 2: Implement status**

`cmd/status.go` — connect (provider Ensure), build Applier, call the engine factory + `eng.Health`, print one line per component: `✔ gitea Ready` / `✗ backstage Progressing: <message>`; also print inventory object count. Exit code 1 if anything is not ready (scripts depend on this). ~60 lines following the down command's connection pattern.

- [ ] **Step 3: Implement get secrets with a failing test first**

`cmd/get_test.go`:

```go
package cmd

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterCLISecrets(t *testing.T) {
	in := []corev1.Secret{
		{ObjectMeta: metav1.ObjectMeta{Name: "gitea-admin", Namespace: "gitea",
			Labels: map[string]string{"cube-idp.dev/cli-secret": "true", "cube-idp.dev/pack-name": "gitea"}},
			Data: map[string][]byte{"username": []byte("gitea_admin"), "password": []byte("s3cr3t")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}},
	}
	rows := filterCLISecrets(in, "")
	if len(rows) != 1 || rows[0].Pack != "gitea" || rows[0].Fields["password"] != "s3cr3t" {
		t.Fatalf("rows: %+v", rows)
	}
	if len(filterCLISecrets(in, "argocd")) != 0 {
		t.Fatal("pack filter must exclude non-matching packs")
	}
}
```

`cmd/get.go`: `get` parent command + `get secrets [-p pack]` subcommand. It lists secrets across namespaces with label selector `cube-idp.dev/cli-secret=true` (client from `apply.New(...).Client()`), filters with:

```go
type secretRow struct {
	Pack, Namespace, Name string
	Fields                map[string]string
}

func filterCLISecrets(secrets []corev1.Secret, packFilter string) []secretRow {
	var rows []secretRow
	for _, s := range secrets {
		if s.Labels["cube-idp.dev/cli-secret"] != "true" {
			continue
		}
		pack := s.Labels["cube-idp.dev/pack-name"]
		if packFilter != "" && pack != packFilter {
			continue
		}
		row := secretRow{Pack: pack, Namespace: s.Namespace, Name: s.Name, Fields: map[string]string{}}
		for k, v := range s.Data {
			row.Fields[k] = string(v)
		}
		rows = append(rows, row)
	}
	return rows
}
```

and prints an aligned table (`text/tabwriter`): PACK / NAMESPACE / NAME / KEY=VALUE pairs.

- [ ] **Step 4: Run tests, register commands, commit**

Register `newDownCmd(), newStatusCmd(), newGetCmd()` in root. Run: `go test ./... -short && go build ./...`

```bash
git add -A && git commit -m "feat: down, status, and get secrets commands"
```

---

### Task 12: Starter packs — traefik (Gateway API), gitea, argocd

**Files:**
- Create: `packs/traefik/{pack.cue,chart.yaml,manifests/00-gateway-api-crds.yaml,manifests/10-gateway.yaml}`, `packs/gitea/{pack.cue,chart.yaml,manifests/10-secret.yaml,manifests/20-httproute.yaml}`, `packs/argocd/{pack.cue,manifests/...}`
- Test: `tests/packs_render_test.go` (golden-ish smoke: every starter pack renders)

**Pack contents (data only — no Go):**

`packs/traefik/pack.cue`:

```cue
name:    "traefik"
version: "0.1.0"
#Values: {
	replicas: int & >0 | *1
}
```

`packs/traefik/chart.yaml`:

```yaml
chart: traefik
repo: https://traefik.github.io/charts
version: "34.1.0"          # pin; bump deliberately
releaseName: traefik
namespace: traefik
```

`packs/traefik/manifests/00-gateway-api-crds.yaml`: the Gateway API standard-channel CRDs (D3). Vendor the pinned release file:

```bash
curl -sL https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml \
  > packs/traefik/manifests/00-gateway-api-crds.yaml
```

`packs/traefik/manifests/10-gateway.yaml`:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: cube-idp
  namespace: traefik
spec:
  gatewayClassName: traefik
  listeners:
    - name: web
      port: 8000
      protocol: HTTP
      allowedRoutes:
        namespaces: {from: All}
```

(Traefik's chart needs Gateway API provider enabled — add to `chart.yaml` a `values:` block support: extend `chartRef` in Task 8 with `Values map[string]any \`yaml:"values"\`` merged under the user values, and set `providers.kubernetesGateway.enabled: true`, `ports.web.exposedPort: 8000`. HTTPS/TLS via `cube-idp trust` is Phase 2 (D6); Phase 1 serves HTTP behind the host port — set the kind gateway mapping containerPort to Traefik's web NodePort: to keep MVP simple, expose Traefik as `hostPort`-less NodePort 30080 and change `gatewayContainerPort` in Task 5 to 30080 with service `type: NodePort`, `nodePorts.web: 30080` in the traefik values. Document this in the pack README.)

`packs/gitea/pack.cue`:

```cue
name:    "gitea"
version: "0.1.0"
#Values: {}
```

`packs/gitea/chart.yaml`:

```yaml
chart: gitea
repo: https://dl.gitea.com/charts/
version: "10.6.0"
releaseName: gitea
namespace: gitea
```

`packs/gitea/manifests/10-secret.yaml` (D9: credentials surfaced via `get secrets`):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gitea-admin-cube-idp
  namespace: gitea
  labels:
    cube-idp.dev/cli-secret: "true"
    cube-idp.dev/pack-name: gitea
type: Opaque
stringData:
  username: gitea_admin
  password: cube-idp-dev   # local-dev default, matches chart value below
```

(Set the same fixed admin credentials in `chart.yaml` values: `gitea.admin.username/password` — acceptable for a local dev platform, same posture as idpbuilder's `--dev-password`; note it in the pack README.)

`packs/gitea/manifests/20-httproute.yaml`:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: gitea
  namespace: gitea
spec:
  parentRefs: [{name: cube-idp, namespace: traefik}]
  hostnames: ["gitea.cube-idp.localtest.me"]
  rules:
    - backendRefs: [{name: gitea-http, port: 3000}]
```

`packs/argocd/pack.cue`: `name: "argocd"`, `version: "0.1.0"`. Manifests: vendor the pinned upstream install (`curl -sL https://raw.githubusercontent.com/argoproj/argo-cd/v2.13.3/manifests/install.yaml`, prepend a `Namespace: argocd` doc, add an HTTPRoute `argocd.cube-idp.localtest.me` → `argocd-server:80`, plus a values-less `pack.cue`). Add args `--insecure` to argocd-server (HTTP behind the gateway) via a small strategic patch checked into the manifests dir.

- [ ] **Step 1: Write the render smoke test**

`tests/packs_render_test.go`:

```go
package tests

import (
	"context"
	"testing"

	"github.com/rafpe/cube-idp/internal/pack"
)

func TestStarterPacksRender(t *testing.T) {
	if testing.Short() {
		t.Skip("helm renders hit the network")
	}
	for _, dir := range []string{"../packs/traefik", "../packs/gitea", "../packs/argocd"} {
		p, err := pack.Fetch(context.Background(), dir, t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		r, err := p.Render(nil)
		if err != nil {
			t.Fatalf("%s render: %v", dir, err)
		}
		if len(r.Objects) == 0 {
			t.Fatalf("%s rendered zero objects", dir)
		}
	}
}
```

- [ ] **Step 2: Create the pack files, run the test**

Run: `go test ./tests/ -run TestStarterPacksRender -v`
Expected: PASS (network required)

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: starter packs — traefik gateway, gitea (D9 default), argocd UI"
```

---

### Task 13: E2E test, CI workflow, README

**Files:**
- Create: `tests/e2e/e2e_test.go`, `.github/workflows/ci.yaml`, `README.md`

- [ ] **Step 1: E2E test (gated by CUBE_IDP_E2E=1)**

`tests/e2e/e2e_test.go`:

```go
package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Full loop on a real kind cluster: init -> up -> status -> get secrets -> down.
// Requires docker; run locally with: CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 20m
func TestUpStatusDown(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
	bin := build(t)
	dir := t.TempDir()

	run(t, dir, bin, "init", "--name", "e2e")
	run(t, dir, bin, "up")                     // must exit 0 (spec: diagnose loudly and exit)
	run(t, dir, bin, "up")                     // idempotency: re-run is the upgrade command
	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea", "argocd"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s:\n%s", comp, out)
		}
	}
	secrets := run(t, dir, bin, "get", "secrets", "-p", "gitea")
	if !strings.Contains(secrets, "gitea_admin") {
		t.Fatalf("gitea admin secret not surfaced (D9):\n%s", secrets)
	}
	run(t, dir, bin, "down")
}

func build(t *testing.T) string {
	t.Helper()
	bin := t.TempDir() + "/cube-idp"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("$ cube-idp %s\n%s", strings.Join(args, " "), out)
	if err != nil {
		t.Fatalf("cube-idp %v failed: %v", args, err)
	}
	return string(out)
}
```

Wrinkle to solve during implementation: `init` writes packs refs as `oci://ghcr.io/cube-idp/packs/...` which don't exist yet. For Phase 1, `config.Default` must emit **repo-local paths** (`./packs/gitea` etc.) resolved relative to the binary's repo — OR simpler and honest: `init --local <repo-root>` flag used by e2e that writes `packs:` refs pointing into the checkout. Choose the flag; published OCI packs arrive when CI starts pushing them (Phase 3 catalog work).

- [ ] **Step 2: CI workflow**

`.github/workflows/ci.yaml`:

```yaml
name: ci
on:
  push: {branches: [main]}
  pull_request:
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: "1.24"}
      - run: go vet ./...
      - run: go test ./... -short
      - run: make test-apply
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: "1.24"}
      - run: CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 25m
```

(GitHub-hosted runners include docker; kind works out of the box. Track `up` wall-time in the e2e log — the <60s goal is measured here, minus image pulls.)

- [ ] **Step 3: README**

`README.md`: what cube-idp is (one paragraph, "pusher not operator"), quickstart (`init` → `up` → `get secrets` → `down`), cube.yaml reference table, pack format section (pack.cue, manifests/, chart.yaml), link to the spec. ~120 lines; copy the vision paragraph from spec §1.

- [ ] **Step 4: Full verification + commit**

Run locally: `go vet ./... && go test ./... -short && make test-apply && CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 25m`
Expected: all green; e2e proves up→up→status→secrets→down on a real kind cluster.

```bash
git add -A && git commit -m "feat: e2e suite, CI workflow, README"
```

---

## Self-Review Notes (already applied)

- **Spec coverage:** §4.1 units → Tasks 2/4/5/6/8/9; §4.2 config incl. D10 → Tasks 3/5; §4.3 up/down flow → Tasks 10/11; §4.4 tier-1 packs → Tasks 8/12; D9 default profile + secrets → Tasks 3/11/12/13; §5 unit+contract+e2e → per-task tests + Task 13 (engine contract suite expands in Phase 2 when the second engine lands — Phase 1 has one implementation, the flux tests are its seed). Deliberately deferred per spec §6: trust/doctor/diff/lock (Phase 2), k3d/vendor/exec-plugins/sync (Phase 3).
- **Known API-drift points** (flagged inline where they occur): `fluxcd/pkg/ssa` method names (Task 6), `fluxcd/pkg/oci` client options (Task 9), Helm v4 action API (Task 8), kind `DetectNodeProvider` (Task 5), CUE default-application on Decode (Task 3). In each case the tests define behavior; adjust call sites mechanically against the installed versions and follow the flux CLI source as the reference consumer.
- **Type consistency:** `kube.Conn` leaf package decision (Task 5) retroactively applies to Task 4 signatures — implementer of Task 4 should create `internal/kube/conn.go` from the start. Engine factory lives in `internal/engine/factory` (Task 9) to avoid the import cycle; Task 10 imports it as `enginefactory`.
