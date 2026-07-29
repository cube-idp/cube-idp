# M3 Cluster Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver M3 — `spec.cluster` API, the `internal/cluster` driver seam with conformance suite, the kind provider, and `cube-idp init` — per the approved design `docs/design/2026-07-29-cluster-domain.md`.

**Architecture:** New domain package `internal/cluster` (Provisioner driver seam, kubeconfig Rebrand/Merge machinery, `Init` operation, `CUBE-CLU-*` catalog) with the kind implementation confined to `internal/cluster/kind`. The CLI edge loads config, picks the driver, and calls `cluster.Init`. Green gate stays hermetic: conformance runs against a stateful fake; real kind e2e is opt-in via `make test-e2e`.

**Tech Stack:** Go 1.26, `sigs.k8s.io/kind v0.32.0` (new, confined to `internal/cluster/kind`), existing `k8s.io/apimachinery`, `sigs.k8s.io/yaml`, `spf13/cobra`.

## Global Constraints

- Green = `make build && make test && make lint` pass AND `make generate` produces no diff.
- Functions <50 lines (funlen), files <300 lines (`make filelen`, `zz_generated*` exempt). Refactor, never raise limits.
- Import direction: `cli → cluster → api`; `cluster → cubeerr`; `internal/cluster/kind` is the ONLY importer of `sigs.k8s.io/kind`. `internal/cluster` never imports `internal/config`.
- Every error hop wrapped with `%w` + context; `context.Context` first param on anything doing I/O (the kind library is context-less — the driver accepts ctx and documents the limitation).
- Tests table-driven; error paths are first-class rows; error assertions via `errors.As` into `*cubeerr.Coded` + code equality — never string matching.
- Mocks are hand-rolled function-field structs. No mockgen.
- Working agreement (memory: sequential-chunked-delivery): each task ends with **owner review of the diff, then commit**. One task = one reviewable commit.
- Global `~/.kube/config` is never touched by tests or tooling; e2e uses `KUBECONFIG=$PWD/.kube/config` (CLAUDE.md §7).

---

### Task 1: `spec.cluster` API sub-struct

**Files:**
- Create: `api/config/v1alpha1/cluster_types.go`
- Create: `api/config/v1alpha1/defaults_test.go`
- Modify: `api/config/v1alpha1/config_types.go` (ConfigSpec)
- Modify: `api/config/v1alpha1/defaults.go`
- Modify: `api/config/v1alpha1/validation.go`
- Modify: `api/config/v1alpha1/validation_test.go`
- Modify: `examples/cube.yaml`
- Regenerate: `api/config/v1alpha1/zz_generated.deepcopy.go` (`make generate`)

**Interfaces:**
- Produces: `type ClusterProvider string`, `const ClusterProviderKind ClusterProvider = "kind"`, `type ClusterSpec struct { Provider ClusterProvider; ForProvider *runtime.RawExtension }`, `ConfigSpec.Cluster *ClusterSpec`. Later tasks rely on these exact names.

- [ ] **Step 1: Write failing tests** — extend the `Validate` table and add a `Default` table:

The existing table in `api/config/v1alpha1/validation_test.go` uses
`mutate func(*v1alpha1.Config)` + `wantField string` rows in the external
package `v1alpha1_test` (and calls `c.Default()` before `Validate()`).
Absent cluster is already covered by the "valid minimal" row. Add exactly:

```go
{"valid cluster with kind provider", func(c *v1alpha1.Config) {
    c.Spec.Cluster = &v1alpha1.ClusterSpec{Provider: v1alpha1.ClusterProviderKind}
}, ""},
{"empty provider is defaulted before validate", func(c *v1alpha1.Config) {
    c.Spec.Cluster = &v1alpha1.ClusterSpec{}
}, ""},
{"unknown cluster provider", func(c *v1alpha1.Config) {
    c.Spec.Cluster = &v1alpha1.ClusterSpec{Provider: "k3d"}
}, "spec.cluster.provider"},
```

New `api/config/v1alpha1/defaults_test.go` (external package, matching the
existing test files):

```go
package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

func TestDefaultCluster(t *testing.T) {
	tests := []struct {
		name string
		in   v1alpha1.ConfigSpec
		want v1alpha1.ClusterProvider // "" means Cluster must stay nil
	}{
		{name: "absent cluster stays nil", in: v1alpha1.ConfigSpec{}, want: ""},
		{name: "empty provider defaults to kind",
			in:   v1alpha1.ConfigSpec{Cluster: &v1alpha1.ClusterSpec{}},
			want: v1alpha1.ClusterProviderKind},
		{name: "set provider untouched (idempotent)",
			in:   v1alpha1.ConfigSpec{Cluster: &v1alpha1.ClusterSpec{Provider: v1alpha1.ClusterProviderKind}},
			want: v1alpha1.ClusterProviderKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{Spec: tt.in}
			c.Default()
			if tt.want == "" {
				if c.Spec.Cluster != nil {
					t.Fatalf("Cluster = %+v, want nil", c.Spec.Cluster)
				}
				return
			}
			if got := c.Spec.Cluster.Provider; got != tt.want {
				t.Fatalf("Provider = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests, verify they fail to compile** — `go test ./api/... -count=1` → expected: `undefined: ClusterSpec`.

- [ ] **Step 3: Implement.** New `api/config/v1alpha1/cluster_types.go`:

```go
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterProvider identifies a cluster backend implementation.
type ClusterProvider string

// ClusterProviderKind provisions clusters with kind (Kubernetes-in-Docker).
const ClusterProviderKind ClusterProvider = "kind"

// ClusterSpec declares the cluster cube-idp manages.
type ClusterSpec struct {
	// Provider selects the backend. Defaults to "kind".
	Provider ClusterProvider `json:"provider,omitempty"`

	// ForProvider carries provider-specific configuration, passed through
	// opaquely at load time and strictly decoded + validated by the
	// selected provider (for kind: a kind.x-k8s.io/v1alpha4 Cluster).
	ForProvider *runtime.RawExtension `json:"forProvider,omitempty"`
}
```

In `config_types.go`, replace `type ConfigSpec struct{}` with:

```go
// ConfigSpec grows one typed sub-struct per component domain, together
// with its defaults and validation — the loading machinery never changes.
type ConfigSpec struct {
	// Cluster is optional: a config without it is valid (config-only use);
	// `init` requires it and fails with CUBE-CLU-001 when absent.
	Cluster *ClusterSpec `json:"cluster,omitempty"`
}
```

In `defaults.go`, body of `Default()`:

```go
func (c *Config) Default() {
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider == "" {
		c.Spec.Cluster.Provider = ClusterProviderKind
	}
}
```

In `validation.go`, append to `Validate` before `return errs`:

```go
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider != ClusterProviderKind {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "cluster", "provider"),
			string(c.Spec.Cluster.Provider), []string{string(ClusterProviderKind)}))
	}
```

Update `examples/cube.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    forProvider:
      # kind.x-k8s.io/v1alpha4 Cluster fields
      nodes:
        - role: control-plane
```

- [ ] **Step 4: Regenerate deepcopy** — `make generate`; confirm `zz_generated.deepcopy.go` gains `ClusterSpec.DeepCopy`.
- [ ] **Step 5: Run gates** — `make build && make test && make lint && make generate` (then `git diff --stat` must show only intended files).
- [ ] **Step 6: Owner review of diff, then commit** — `git add -A && git commit -m "feat(api): spec.cluster sub-struct — typed provider const + opaque forProvider"`.

---

### Task 2: `internal/cluster` seam, error catalog, conformance suite, fake

**Files:**
- Create: `internal/cluster/cluster.go`
- Create: `internal/cluster/errors.go`
- Create: `internal/cluster/conformance.go`
- Create: `internal/cluster/conformance_test.go` (stateful fake + suite run)

**Interfaces:**
- Consumes: nothing from earlier tasks (`runtime.RawExtension` from apimachinery).
- Produces: `type Spec struct { Name string; ForProvider *runtime.RawExtension }`; `type Provisioner interface { Ensure(ctx, Spec) error; Exists(ctx, string) (bool, error); Delete(ctx, string) error; Kubeconfig(ctx, string) ([]byte, error) }`; `RunClusterConformance(t *testing.T, factory func() Provisioner)`; codes `CodeNoClusterConfigured..CodeKubeconfigFailed`; exported constructors `ErrNoClusterConfigured() error`, `ErrUnsupportedProvider(p string) error`, `ErrInvalidForProvider(cause error) error`, `ErrProvisionFailed(action, name string, cause error) error`, `ErrKubeconfigFailed(cause error) error`.

- [ ] **Step 1: Write the seam.** `internal/cluster/cluster.go`:

```go
// Package cluster is the cluster-provisioning domain: the Provisioner
// driver seam, its conformance suite, the kubeconfig machinery that brands
// provider-native kubeconfigs as cube-owned contexts, and the Init
// operation the CLI calls. Implementations live in subpackages (kind);
// driver selection happens at the CLI edge, never here.
package cluster

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
)

// Spec is the provider-neutral input to a Provisioner.
type Spec struct {
	Name        string                // cluster name (the cube identity)
	ForProvider *runtime.RawExtension // provider-specific config, opaque here
}

// Provisioner is the driver seam for cluster backends (design §4).
// Implementations must satisfy RunClusterConformance.
type Provisioner interface {
	// Ensure creates the cluster if absent; no-op if it exists.
	// Idempotent by name: it does not diff a live cluster against Spec.
	Ensure(ctx context.Context, s Spec) error
	Exists(ctx context.Context, name string) (bool, error)
	// Delete removes the cluster; deleting an absent cluster is a no-op.
	Delete(ctx context.Context, name string) error
	// Kubeconfig returns the raw admin kubeconfig with provider-native
	// entry names; rebranding to cube-owned names is the domain's job.
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
}
```

- [ ] **Step 2: Write the catalog.** `internal/cluster/errors.go` (constructors are exported — drivers in subpackages and the CLI edge raise them):

```go
package cluster

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The cluster domain owns the CUBE-CLU-* code range (design §6); the
// human-readable registry row lives in the design docs.
const (
	CodeNoClusterConfigured cubeerr.Code = "CUBE-CLU-001"
	CodeUnsupportedProvider cubeerr.Code = "CUBE-CLU-002"
	CodeInvalidForProvider  cubeerr.Code = "CUBE-CLU-003"
	CodeProvisionFailed     cubeerr.Code = "CUBE-CLU-004"
	CodeKubeconfigFailed    cubeerr.Code = "CUBE-CLU-005"
)

func ErrNoClusterConfigured() error {
	return cubeerr.Wrap(CodeNoClusterConfigured,
		"no cluster configured",
		"add spec.cluster to the config to let cube-idp manage a cluster", nil)
}

func ErrUnsupportedProvider(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no driver for provider %q", provider),
		"use a supported spec.cluster.provider (kind)", nil)
}

func ErrInvalidForProvider(cause error) error {
	return cubeerr.Wrap(CodeInvalidForProvider,
		"invalid spec.cluster.forProvider payload",
		"fix the provider config fields listed above (kind: kind.x-k8s.io/v1alpha4 Cluster)", cause)
}

func ErrProvisionFailed(action, name string, cause error) error {
	return cubeerr.Wrap(CodeProvisionFailed,
		fmt.Sprintf("%s cluster %q failed", action, name),
		"check that the container runtime (Docker/Podman) is running; see cause above", cause)
}

func ErrKubeconfigFailed(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigFailed,
		"kubeconfig generation failed",
		"see cause above; check file permissions on the kubeconfig target", cause)
}
```

- [ ] **Step 3: Write the conformance suite.** `internal/cluster/conformance.go`:

```go
package cluster

import (
	"context"
	"testing"

	"sigs.k8s.io/yaml"
)

// RunClusterConformance asserts the behavioral contract every Provisioner
// must satisfy (design §7). Drivers run it from their own test packages.
func RunClusterConformance(t *testing.T, factory func() Provisioner) {
	t.Helper()
	const name = "cube-conformance"
	ctx := context.Background()
	p := factory()
	t.Cleanup(func() { _ = p.Delete(ctx, name) })

	assertExists := func(want bool, when string) {
		t.Helper()
		got, err := p.Exists(ctx, name)
		if err != nil {
			t.Fatalf("Exists %s: %v", when, err)
		}
		if got != want {
			t.Fatalf("Exists %s = %v, want %v", when, got, want)
		}
	}

	assertExists(false, "before Ensure")
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertExists(true, "after Ensure")
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure (second, must no-op): %v", err)
	}

	raw, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	var kc struct {
		Contexts []struct {
			Name string `json:"name"`
		} `json:"contexts"`
	}
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		t.Fatalf("Kubeconfig not parseable YAML: %v", err)
	}
	if len(kc.Contexts) == 0 {
		t.Fatal("Kubeconfig has no contexts")
	}

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertExists(false, "after Delete")
	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete (second, must no-op): %v", err)
	}
	if _, err := p.Kubeconfig(ctx, name); err == nil {
		t.Fatal("Kubeconfig after Delete: want error, got nil")
	}
}
```

- [ ] **Step 4: Write the stateful fake + run the suite.** `internal/cluster/conformance_test.go`:

```go
package cluster

import (
	"context"
	"fmt"
	"testing"
)

// fakeProvisioner is the hand-rolled stateful reference implementation:
// it proves the conformance suite itself, Docker-free (design §7).
type fakeProvisioner struct {
	clusters map[string]bool
}

func newFake() *fakeProvisioner { return &fakeProvisioner{clusters: map[string]bool{}} }

func (f *fakeProvisioner) Ensure(_ context.Context, s Spec) error {
	f.clusters[s.Name] = true
	return nil
}

func (f *fakeProvisioner) Exists(_ context.Context, name string) (bool, error) {
	return f.clusters[name], nil
}

func (f *fakeProvisioner) Delete(_ context.Context, name string) error {
	delete(f.clusters, name)
	return nil
}

func (f *fakeProvisioner) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	if !f.clusters[name] {
		return nil, fmt.Errorf("cluster %q not found", name)
	}
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: fake-%[1]s
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: fake-%[1]s
    context:
      cluster: fake-%[1]s
      user: fake-%[1]s
users:
  - name: fake-%[1]s
    user:
      token: fake
current-context: fake-%[1]s
`, name)), nil
}

func TestConformance_Fake(t *testing.T) {
	RunClusterConformance(t, func() Provisioner { return newFake() })
}
```

- [ ] **Step 5: Run** — `go test ./internal/cluster/... -count=1 -v` → expected: `TestConformance_Fake` PASS.
- [ ] **Step 6: Full gates** — `make build && make test && make lint`.
- [ ] **Step 7: Owner review of diff, then commit** — `git commit -m "feat(cluster): Provisioner driver seam, CUBE-CLU-* catalog, conformance suite + stateful fake"`.

---

### Task 3: Kubeconfig machinery — `ContextName`, `Rebrand`, `Merge`

**Files:**
- Create: `internal/cluster/kubeconfig.go`
- Create: `internal/cluster/kubeconfig_test.go`

**Interfaces:**
- Consumes: `v1alpha1.GroupVersion.Group` (Task 1's package, existing constant).
- Produces: `ContextName(clusterName string) string`; `Rebrand(raw []byte, contextName, namespace string) ([]byte, error)`; `Merge(existing, incoming []byte) ([]byte, error)`. Task 4 calls all three.

- [ ] **Step 1: Write failing tests.** `internal/cluster/kubeconfig_test.go` — table-driven; key rows (write all of them):

```go
package cluster

import (
	"strings"
	"testing"
)

const kindStyleKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: kind-dev
    cluster:
      server: https://127.0.0.1:52556
      certificate-authority-data: Zm9v
contexts:
  - name: kind-dev
    context:
      cluster: kind-dev
      user: kind-dev
users:
  - name: kind-dev
    user:
      client-certificate-data: YmFy
current-context: kind-dev
`

func TestContextName(t *testing.T) {
	if got, want := ContextName("dev"), "cube-idp.dev/dev"; got != want {
		t.Fatalf("ContextName = %q, want %q", got, want)
	}
}

func TestRebrand(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		contextName string
		namespace   string
		wantErr     bool
		wantSubstr  []string
		notSubstr   []string
	}{
		{
			name: "renames all entries and current-context",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev",
			wantSubstr: []string{"cube-idp.dev/dev", "certificate-authority-data: Zm9v", "client-certificate-data: YmFy"},
			notSubstr:  []string{"kind-dev"},
		},
		{
			name: "stamps namespace when set",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev", namespace: "platform",
			wantSubstr: []string{"namespace: platform"},
		},
		{
			name: "omits namespace when empty",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev",
			notSubstr: []string{"namespace:"},
		},
		{
			name: "rejects multi-context kubeconfig",
			raw: kindStyleKubeconfig + `  - name: other
    context:
      cluster: other
      user: other
`,
			contextName: "x", wantErr: true,
		},
		{name: "rejects unparseable input", raw: ":\tnot yaml", contextName: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Rebrand([]byte(tt.raw), tt.contextName, tt.namespace)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Rebrand: %v", err)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(string(out), s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}
			for _, s := range tt.notSubstr {
				if strings.Contains(string(out), s) {
					t.Errorf("output still contains %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestMerge(t *testing.T) {
	branded, err := Rebrand([]byte(kindStyleKubeconfig), "cube-idp.dev/dev", "")
	if err != nil {
		t.Fatalf("Rebrand fixture: %v", err)
	}
	existing := `apiVersion: v1
kind: Config
clusters:
  - name: other
    cluster:
      server: https://example.com
contexts:
  - name: other
    context:
      cluster: other
      user: other
users:
  - name: other
    user:
      token: abc
current-context: other
`
	tests := []struct {
		name       string
		existing   string
		wantSubstr []string
	}{
		{
			name: "into empty file yields incoming",
			wantSubstr: []string{"cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
		{
			name: "preserves other entries, upserts ours, takes current-context",
			existing:   existing,
			wantSubstr: []string{"name: other", "token: abc", "cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Merge([]byte(tt.existing), branded)
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(string(out), s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}
		})
	}
	// merging the same incoming twice must not duplicate entries
	once, _ := Merge([]byte(existing), branded)
	twice, err := Merge(once, branded)
	if err != nil {
		t.Fatalf("Merge (idempotent): %v", err)
	}
	if strings.Count(string(twice), "name: cube-idp.dev/dev") != strings.Count(string(once), "name: cube-idp.dev/dev") {
		t.Fatal("Merge duplicated entries on re-merge")
	}
}
```

- [ ] **Step 2: Run, verify compile failure** — `go test ./internal/cluster/... -count=1` → `undefined: ContextName`.

- [ ] **Step 3: Implement.** `internal/cluster/kubeconfig.go` — minimal typed model over `sigs.k8s.io/yaml`; untouched entry bodies are `json.RawMessage` so unknown fields round-trip:

```go
package cluster

import (
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// ContextName derives the cube-owned kubeconfig context name. The prefix
// is the API group — one source of truth, never a second literal.
func ContextName(clusterName string) string {
	return v1alpha1.GroupVersion.Group + "/" + clusterName
}

// kubeconfig is a minimal kubeconfig model: just enough structure to
// rename and merge entries. Entry bodies stay raw so every field a
// provider emitted round-trips unchanged. Context bodies are fully typed —
// cluster/user/namespace/extensions is the complete upstream field set.
type kubeconfig struct {
	APIVersion     string          `json:"apiVersion,omitempty"`
	Kind           string          `json:"kind,omitempty"`
	Preferences    json.RawMessage `json:"preferences,omitempty"`
	Clusters       []namedEntry    `json:"clusters,omitempty"`
	Contexts       []contextEntry  `json:"contexts,omitempty"`
	Users          []namedEntry    `json:"users,omitempty"`
	CurrentContext string          `json:"current-context,omitempty"`
	Extensions     json.RawMessage `json:"extensions,omitempty"`
}

type namedEntry struct {
	Name    string          `json:"name"`
	Cluster json.RawMessage `json:"cluster,omitempty"`
	User    json.RawMessage `json:"user,omitempty"`
}

type contextEntry struct {
	Name    string      `json:"name"`
	Context contextBody `json:"context"`
}

type contextBody struct {
	Cluster    string          `json:"cluster"`
	User       string          `json:"user"`
	Namespace  string          `json:"namespace,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
}

// Rebrand renames a single-cluster, provider-native kubeconfig to
// contextName and stamps the context namespace when non-empty (design §4).
func Rebrand(raw []byte, contextName, namespace string) ([]byte, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	if len(kc.Clusters) != 1 || len(kc.Contexts) != 1 || len(kc.Users) != 1 {
		return nil, fmt.Errorf("expected single-cluster kubeconfig, got %d clusters / %d contexts / %d users",
			len(kc.Clusters), len(kc.Contexts), len(kc.Users))
	}
	kc.Clusters[0].Name = contextName
	kc.Users[0].Name = contextName
	kc.Contexts[0].Name = contextName
	kc.Contexts[0].Context.Cluster = contextName
	kc.Contexts[0].Context.User = contextName
	if namespace != "" {
		kc.Contexts[0].Context.Namespace = namespace
	}
	kc.CurrentContext = contextName
	out, err := yaml.Marshal(kc)
	if err != nil {
		return nil, fmt.Errorf("render kubeconfig: %w", err)
	}
	return out, nil
}

// Merge upserts incoming's entries into existing by name and adopts
// incoming's current-context. An empty existing yields incoming as-is.
func Merge(existing, incoming []byte) ([]byte, error) {
	if len(existing) == 0 {
		return incoming, nil
	}
	var dst, src kubeconfig
	if err := yaml.Unmarshal(existing, &dst); err != nil {
		return nil, fmt.Errorf("parse existing kubeconfig: %w", err)
	}
	if err := yaml.Unmarshal(incoming, &src); err != nil {
		return nil, fmt.Errorf("parse incoming kubeconfig: %w", err)
	}
	dst.Clusters = upsertNamed(dst.Clusters, src.Clusters)
	dst.Users = upsertNamed(dst.Users, src.Users)
	dst.Contexts = upsertContexts(dst.Contexts, src.Contexts)
	if src.CurrentContext != "" {
		dst.CurrentContext = src.CurrentContext
	}
	if dst.APIVersion == "" {
		dst.APIVersion, dst.Kind = src.APIVersion, src.Kind
	}
	out, err := yaml.Marshal(dst)
	if err != nil {
		return nil, fmt.Errorf("render merged kubeconfig: %w", err)
	}
	return out, nil
}

func upsertNamed(dst, src []namedEntry) []namedEntry {
	for _, s := range src {
		replaced := false
		for i := range dst {
			if dst[i].Name == s.Name {
				dst[i], replaced = s, true
				break
			}
		}
		if !replaced {
			dst = append(dst, s)
		}
	}
	return dst
}

func upsertContexts(dst, src []contextEntry) []contextEntry {
	for _, s := range src {
		replaced := false
		for i := range dst {
			if dst[i].Name == s.Name {
				dst[i], replaced = s, true
				break
			}
		}
		if !replaced {
			dst = append(dst, s)
		}
	}
	return dst
}
```

- [ ] **Step 4: Run** — `go test ./internal/cluster/... -count=1` → PASS. Note: file is ~140 lines — within the 300 gate; the two upsert helpers stay separate to keep functions <50.
- [ ] **Step 5: Full gates** — `make build && make test && make lint`.
- [ ] **Step 6: Owner review of diff, then commit** — `git commit -m "feat(cluster): kubeconfig machinery — ContextName, Rebrand, Merge (no client-go)"`.

---

### Task 4: The `Init` operation

**Files:**
- Create: `internal/cluster/init.go`
- Create: `internal/cluster/init_test.go`

**Interfaces:**
- Consumes: `Provisioner`, `Spec`, `ContextName`, `Rebrand`, `Merge`, `ErrKubeconfigFailed` (Tasks 2–3); the test reuses the `kindStyleKubeconfig` const declared in Task 3's `kubeconfig_test.go` (same package).
- Produces: `type InitOptions struct { Spec Spec; ContextName, Namespace, KubeconfigPath string }`; `Init(ctx context.Context, p Provisioner, opts InitOptions) error`. Task 6's CLI calls this.

- [ ] **Step 1: Write failing tests.** `internal/cluster/init_test.go` — function-field mock, `t.Setenv`/`t.TempDir`, never the real home dir:

```go
package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

type mockProvisioner struct {
	EnsureFunc     func(ctx context.Context, s Spec) error
	KubeconfigFunc func(ctx context.Context, name string) ([]byte, error)
}

func (m *mockProvisioner) Ensure(ctx context.Context, s Spec) error {
	if m.EnsureFunc != nil {
		return m.EnsureFunc(ctx, s)
	}
	return nil
}
func (m *mockProvisioner) Exists(context.Context, string) (bool, error) { return false, nil }
func (m *mockProvisioner) Delete(context.Context, string) error         { return nil }
func (m *mockProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	if m.KubeconfigFunc != nil {
		return m.KubeconfigFunc(ctx, name)
	}
	return []byte(strings.ReplaceAll(kindStyleKubeconfig, "kind-dev", "kind-"+name)), nil
}

func TestInit(t *testing.T) {
	tests := []struct {
		name     string
		opts     InitOptions
		mock     *mockProvisioner
		wantCode cubeerr.Code // "" = success
		wantIn   []string     // substrings expected in the target file
		explicit bool         // true → assert default location untouched
	}{
		{
			name: "merges into KUBECONFIG default with derived context",
			opts: InitOptions{Spec: Spec{Name: "dev"}},
			mock: &mockProvisioner{},
			wantIn: []string{"cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
		{
			name: "explicit path writes file, no merge into default",
			opts: InitOptions{Spec: Spec{Name: "dev"}}, // KubeconfigPath set in test body
			mock: &mockProvisioner{}, explicit: true,
			wantIn: []string{"cube-idp.dev/dev"},
		},
		{
			name: "context name override and namespace stamped",
			opts: InitOptions{Spec: Spec{Name: "dev"}, ContextName: "my-ctx", Namespace: "platform"},
			mock: &mockProvisioner{},
			wantIn: []string{"name: my-ctx", "namespace: platform"},
		},
		{
			name: "ensure failure surfaces driver error untouched",
			opts: InitOptions{Spec: Spec{Name: "dev"}},
			mock: &mockProvisioner{EnsureFunc: func(context.Context, Spec) error {
				return ErrProvisionFailed("create", "dev", errors.New("boom"))
			}},
			wantCode: CodeProvisionFailed,
		},
		{
			name: "kubeconfig failure wraps as CLU-005",
			opts: InitOptions{Spec: Spec{Name: "dev"}},
			mock: &mockProvisioner{KubeconfigFunc: func(context.Context, string) ([]byte, error) {
				return nil, fmt.Errorf("boom")
			}},
			wantCode: CodeKubeconfigFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			defaultPath := filepath.Join(dir, "default-kubeconfig")
			t.Setenv("KUBECONFIG", defaultPath)
			target := defaultPath
			if tt.explicit {
				target = filepath.Join(dir, "explicit-kubeconfig")
				tt.opts.KubeconfigPath = target
			}

			err := Init(context.Background(), tt.mock, tt.opts)

			if tt.wantCode != "" {
				var coded *cubeerr.Coded
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init: %v", err)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			for _, s := range tt.wantIn {
				if !strings.Contains(string(got), s) {
					t.Errorf("target missing %q:\n%s", s, got)
				}
			}
			if tt.explicit {
				if _, err := os.Stat(defaultPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("default kubeconfig was touched despite explicit --kubeconfig path")
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run, verify compile failure** — `go test ./internal/cluster/... -count=1` → `undefined: InitOptions`.

- [ ] **Step 3: Implement.** `internal/cluster/init.go`:

```go
package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// InitOptions parameterizes Init. Namespace is a method-level option by
// design (no spec surface): today the CLI passes it empty; future flows
// set it programmatically (design §1 decision 6).
type InitOptions struct {
	Spec        Spec
	ContextName string // "" → ContextName(Spec.Name)
	Namespace   string // "" → omitted from the generated context
	// KubeconfigPath, when set, is written as a standalone file — no merge
	// into the default location. When empty, the rebranded config is
	// merged into $KUBECONFIG (first entry) or ~/.kube/config.
	KubeconfigPath string
}

// Init ensures the cluster exists and installs its cube-branded
// kubeconfig context: Ensure → Kubeconfig → Rebrand → merge-or-write.
func Init(ctx context.Context, p Provisioner, opts InitOptions) error {
	if err := p.Ensure(ctx, opts.Spec); err != nil {
		return err // drivers return coded errors already
	}
	raw, err := p.Kubeconfig(ctx, opts.Spec.Name)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("fetch kubeconfig for %s: %w", opts.Spec.Name, err))
	}
	name := opts.ContextName
	if name == "" {
		name = ContextName(opts.Spec.Name)
	}
	branded, err := Rebrand(raw, name, opts.Namespace)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("rebrand kubeconfig as %s: %w", name, err))
	}
	if opts.KubeconfigPath != "" {
		return writeKubeconfig(opts.KubeconfigPath, branded)
	}
	return mergeIntoDefault(branded)
}

func mergeIntoDefault(branded []byte) error {
	target := defaultKubeconfigPath()
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return ErrKubeconfigFailed(fmt.Errorf("read kubeconfig %s: %w", target, err))
	}
	merged, err := Merge(existing, branded)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("merge into %s: %w", target, err))
	}
	return writeKubeconfig(target, merged)
}

func writeKubeconfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("create kubeconfig dir for %s: %w", path, err))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("write kubeconfig %s: %w", path, err))
	}
	return nil
}

// defaultKubeconfigPath mirrors kubectl's resolution: first KUBECONFIG
// list entry, else ~/.kube/config.
func defaultKubeconfigPath() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, p := range filepath.SplitList(env) {
			if p != "" {
				return p
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}
```

- [ ] **Step 4: Run** — `go test ./internal/cluster/... -count=1` → PASS.
- [ ] **Step 5: Full gates** — `make build && make test && make lint`.
- [ ] **Step 6: Owner review of diff, then commit** — `git commit -m "feat(cluster): Init operation — ensure, rebrand, merge-or-write kubeconfig"`.

---

### Task 5: The kind provider + `make test-e2e`

**Files:**
- Create: `internal/cluster/kind/kind.go`
- Create: `internal/cluster/kind/kind_test.go`
- Modify: `go.mod` / `go.sum` (`go get sigs.k8s.io/kind@v0.32.0`)
- Modify: `Makefile` (add `test-e2e` target)

**Interfaces:**
- Consumes: `cluster.Spec`, `cluster.Provisioner`, `cluster.RunClusterConformance`, `cluster.ErrInvalidForProvider`, `cluster.ErrProvisionFailed` (Task 2).
- Produces: `kind.New() (*Provider, error)` where `*Provider` satisfies `cluster.Provisioner`. Task 6's factory calls `New`.

- [ ] **Step 1: Add the dependency** — `go get sigs.k8s.io/kind@v0.32.0 && go mod tidy`. This is the design §8 approved addition; nothing outside `internal/cluster/kind` may import it.

- [ ] **Step 2: Write the e2e test first.** `internal/cluster/kind/kind_test.go` — opt-in via `CUBE_E2E=1` (keeps `make test` fast even on Docker-equipped machines), auto-skip when no runtime:

```go
package kind

import (
	"os"
	"os/exec"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cluster"
)

func TestConformance(t *testing.T) {
	if os.Getenv("CUBE_E2E") != "1" {
		t.Skip("kind e2e is opt-in: run via `make test-e2e` (sets CUBE_E2E=1)")
	}
	if !runtimeAvailable() {
		t.Skip("no container runtime reachable (docker/podman) — skipping kind e2e")
	}
	cluster.RunClusterConformance(t, func() cluster.Provisioner {
		p, err := New()
		if err != nil {
			t.Fatalf("kind.New: %v", err)
		}
		return p
	})
}

func runtimeAvailable() bool {
	for _, rt := range []string{"docker", "podman"} {
		if exec.Command(rt, "info").Run() == nil {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Implement.** `internal/cluster/kind/kind.go`:

```go
// Package kind implements the cluster.Provisioner driver seam with kind
// (Kubernetes-in-Docker), driven as a Go library. It is the ONLY package
// allowed to import sigs.k8s.io/kind (design §8).
package kind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster"
)

// Provider drives kind. The kind library takes no context.Context; the
// seam's ctx parameters are accepted and unused — a documented library
// limitation, not a design choice.
type Provider struct {
	kp *kindcluster.Provider
}

// New detects the container runtime (docker/podman/nerdctl) and returns
// a kind-backed Provisioner.
func New() (*Provider, error) {
	opt, err := kindcluster.DetectNodeProvider()
	if err != nil {
		return nil, fmt.Errorf("detect container runtime for kind: %w", err)
	}
	return &Provider{kp: kindcluster.NewProvider(opt)}, nil
}

// Ensure creates the cluster if absent. Idempotency is by name only —
// it never diffs a live cluster against the spec (design §5).
func (p *Provider) Ensure(ctx context.Context, s cluster.Spec) error {
	exists, err := p.Exists(ctx, s.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	cfg := &v1alpha4.Cluster{}
	if s.ForProvider != nil && len(s.ForProvider.Raw) > 0 {
		if err := yaml.UnmarshalStrict(s.ForProvider.Raw, cfg); err != nil {
			return cluster.ErrInvalidForProvider(fmt.Errorf("decode forProvider as kind.x-k8s.io/v1alpha4 Cluster: %w", err))
		}
	}
	cfg.Name = s.Name

	// Point kind's own kubeconfig export at a throwaway path so it never
	// touches the user's file; the domain owns kubeconfig installation.
	tmp, err := os.MkdirTemp("", "cube-idp-kind-*")
	if err != nil {
		return cluster.ErrProvisionFailed("create", s.Name, fmt.Errorf("temp kubeconfig dir: %w", err))
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := p.kp.Create(s.Name,
		kindcluster.CreateWithV1Alpha4Config(cfg),
		kindcluster.CreateWithKubeconfigPath(filepath.Join(tmp, "kubeconfig")),
	); err != nil {
		return cluster.ErrProvisionFailed("create", s.Name, err)
	}
	return nil
}

func (p *Provider) Exists(_ context.Context, name string) (bool, error) {
	names, err := p.kp.List()
	if err != nil {
		return false, cluster.ErrProvisionFailed("list", name, err)
	}
	return slices.Contains(names, name), nil
}

func (p *Provider) Delete(_ context.Context, name string) error {
	// kind's Delete is a no-op for absent clusters, matching the seam.
	if err := p.kp.Delete(name, ""); err != nil {
		return cluster.ErrProvisionFailed("delete", name, err)
	}
	return nil
}

func (p *Provider) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kc, err := p.kp.KubeConfig(name, false)
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig for %s: %w", name, err)
	}
	return []byte(kc), nil
}
```

- [ ] **Step 4: Compile-time seam check** — the conformance factory in the test already enforces `*Provider` satisfies `cluster.Provisioner`; run `go build ./... && go vet ./...`.

- [ ] **Step 5: Add the e2e target.** In `Makefile` (extend `.PHONY` accordingly):

```make
# Real kind conformance — needs Docker/Podman; never part of the green gate.
# KUBECONFIG stays inside the worktree per CLAUDE.md §7.
test-e2e:
	CUBE_E2E=1 KUBECONFIG=$(CURDIR)/.kube/config $(GO) test ./internal/cluster/kind/... -count=1 -timeout 20m -v
```

- [ ] **Step 6: Run the hermetic gates** — `make build && make test && make lint` (kind e2e must show as SKIP in `make test` output).
- [ ] **Step 7: Run e2e if a runtime is available** — `make test-e2e`; expected: real cluster create→exists→kubeconfig→delete cycle PASS (or documented SKIP if no runtime on this machine). Delete `$(CURDIR)/.kube/config` afterwards if created.
- [ ] **Step 8: Owner review of diff, then commit** — `git commit -m "feat(cluster/kind): kind driver via sigs.k8s.io/kind v0.32.0 + make test-e2e"`.

---

### Task 6: CLI — `cube-idp init`

**Files:**
- Create: `internal/cli/init.go`
- Create: `internal/cli/init_internal_test.go` (package `cli` — needs the unexported factory hook)
- Modify: `internal/cli/root.go` (register command)
- Modify: `internal/cli/cli_test.go` (error-path test, external package `cli_test`)

**Interfaces:**
- Consumes: `config.LoadFile`, `cluster.Init`, `cluster.InitOptions`, `cluster.Spec`, `cluster.ErrNoClusterConfigured`, `cluster.ErrUnsupportedProvider`, `kind.New`, `v1alpha1.ClusterProviderKind` (Tasks 1, 2, 4, 5).
- Produces: the `init` command; package-level `var newProvisioner` factory hook (unexported — override only from same-package test files).

- [ ] **Step 1: Write failing CLI tests.** The existing `cli_test.go` is the **external** package `cli_test` with helpers `writeTemp(t, content) string` and `run(t, args...) (code, stdout, stderr)` — it cannot see `newProvisioner`. Split accordingly.

Error path — append to `internal/cli/cli_test.go` (its `validYAML` const has `spec: {}`, i.e. no cluster):

```go
func TestInitRequiresCluster(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, _, stderr := run(t, "init", "-f", path)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Errorf("stderr %q should carry CUBE-CLU-001", stderr)
	}
}
```

Success path — new `internal/cli/init_internal_test.go` in package `cli`, with its own hand-rolled mock (same function-field shape as Task 4's):

```go
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

type mockProvisioner struct{}

func (mockProvisioner) Ensure(context.Context, cluster.Spec) error          { return nil }
func (mockProvisioner) Exists(context.Context, string) (bool, error)       { return true, nil }
func (mockProvisioner) Delete(context.Context, string) error               { return nil }
func (mockProvisioner) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kc := `apiVersion: v1
kind: Config
clusters:
  - name: kind-NAME
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: kind-NAME
    context:
      cluster: kind-NAME
      user: kind-NAME
users:
  - name: kind-NAME
    user:
      token: fake
current-context: kind-NAME
`
	return []byte(strings.ReplaceAll(kc, "NAME", name)), nil
}

func TestInitWritesKubeconfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	cfgYAML := `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	restore := newProvisioner
	newProvisioner = func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	}
	defer func() { newProvisioner = restore }()

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(),
		[]string{"init", "-f", cfgPath, "--kubeconfig", kubeconfigPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("kubeconfig not written: %v", err)
	}
	if !strings.Contains(string(raw), "cube-idp.dev/dev") {
		t.Fatalf("kubeconfig missing cube context:\n%s", raw)
	}
}
```

- [ ] **Step 2: Run, verify failure** — `go test ./internal/cli/... -count=1` → `undefined: newProvisioner` / unknown command "init".

- [ ] **Step 3: Implement.** `internal/cli/init.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	kindprov "github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/config"
)

// newProvisioner maps a validated provider to its driver. Factories live
// at the CLI edge (design §2); a var so tests inject a mock seam.
var newProvisioner = func(p v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
	switch p {
	case v1alpha1.ClusterProviderKind:
		return kindprov.New()
	default:
		return nil, cluster.ErrUnsupportedProvider(string(p))
	}
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the cluster from the Config and install its kubeconfig context",
		RunE:  runInit,
	}
	cmd.Flags().String("kubeconfig", "",
		"write the kubeconfig to this file instead of merging into the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the generated context name (default: cube-idp.dev/<cluster-name>)")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
	contextName, _ := cmd.Flags().GetString("kubeconfig-context-name")

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Spec.Cluster == nil {
		return cluster.ErrNoClusterConfigured()
	}
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return err
	}
	if err := cluster.Init(cmd.Context(), p, cluster.InitOptions{
		Spec:           cluster.Spec{Name: cfg.Name, ForProvider: cfg.Spec.Cluster.ForProvider},
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	}); err != nil {
		return err
	}
	name := contextName
	if name == "" {
		name = cluster.ContextName(cfg.Name)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cluster %q ready — kubeconfig context %q installed\n", cfg.Name, name)
	return nil
}
```

In `root.go`: `root.AddCommand(newConfigCmd(), newInitCmd())`.

- [ ] **Step 4: Run** — `go test ./internal/cli/... -count=1` → PASS.
- [ ] **Step 5: Full gates** — `make build && make test && make lint`.
- [ ] **Step 6: Smoke test the binary** — `./cube-idp init --help` shows both flags; then a config without cluster must exit 1 with `CUBE-CLU-001` guidance:

```bash
printf 'apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec: {}\n' > /tmp/no-cluster.yaml
./cube-idp init -f /tmp/no-cluster.yaml; echo $?   # → CUBE-CLU-001 on stderr, exit 1
rm /tmp/no-cluster.yaml
```
- [ ] **Step 7: Owner review of diff, then commit** — `git commit -m "feat(cli): cube-idp init — driver factory at the edge, kubeconfig flags"`.

---

### Task 7: Milestone close-out — CHANGELOG, end-to-end verification

**Files:**
- Modify: `CHANGELOG.md` (entry following its existing format — read it first)

**Interfaces:** none — verification only.

- [ ] **Step 1: CHANGELOG** — add an entry for M3 under the current unreleased section, matching the file's established style: `spec.cluster` API, `internal/cluster` driver seam + conformance, kind provider (`sigs.k8s.io/kind v0.32.0`), `cube-idp init` with cube-owned kubeconfig contexts.
- [ ] **Step 2: Full green sweep** — `make build && make test && make lint && make generate && git diff --exit-code` (generate must produce no diff).
- [ ] **Step 3: Real end-to-end (Docker available)** — from the worktree:

```bash
KUBECONFIG=$PWD/.kube/config ./cube-idp init -f examples/cube.yaml
KUBECONFIG=$PWD/.kube/config kubectl config current-context   # → cube-idp.dev/dev
KUBECONFIG=$PWD/.kube/config kubectl get nodes                # → 1 control-plane Ready
kind delete cluster --name dev && rm -f $PWD/.kube/config     # cleanup + CLAUDE.md §7
```

- [ ] **Step 4: Owner review of diff, then commit** — `git commit -m "docs: CHANGELOG for M3 cluster milestone"`.
- [ ] **Step 5: PR** — M3 lands as one green PR to `main` (branch `RafPe/implement-core-cluster-features`), per CLAUDE.md §6.
