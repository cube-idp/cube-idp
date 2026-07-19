package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/lock"
)

func TestPlanRowClassification(t *testing.T) {
	if row := classify("dir:h1:old", "dir:h1:old"); row.Change != "up to date" {
		t.Fatalf("same pin: %+v", row)
	}
	if row := classify("dir:h1:old", "dir:h1:new"); row.Change != "update available" {
		t.Fatalf("moved pin: %+v", row)
	}
	if row := classify("", "dir:h1:new"); row.Change != "new (not in cube.lock)" {
		t.Fatalf("missing lock entry: %+v", row)
	}
}

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

// A pack whose valuesRef moved since the lock was written gets its OWN
// `values(<ref>)` row, carrying the locked valuesPin as CURRENT and the
// would-be pin as LATEST. Local-file refs pin as file:<sha256> on both the
// lock side (pack.FetchFile) and the probe side (pack.ResolveRemote), so the
// two compare like-for-like.
func TestRefRowsValuesSourceDrift(t *testing.T) {
	vf := filepath.Join(t.TempDir(), "values.yaml")
	content := []byte("replicas: 3\n")
	if err := os.WriteFile(vf, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	wantLatest := "file:" + hex.EncodeToString(sum[:])

	cube := &config.Cube{}
	cube.Spec.Packs = []config.PackRef{{Ref: "./p", ValuesRef: vf}}
	lf := &lock.File{Packs: []lock.Entry{{Ref: "./p", ValuesRef: vf, ValuesPin: "file:" + strings.Repeat("a", 64)}}}

	rows, err := refRows(context.Background(), cube, lf, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want exactly one values row", rows)
	}
	if want := "values(" + vf + ")"; rows[0].Name != want {
		t.Fatalf("name = %q, want %q", rows[0].Name, want)
	}
	if rows[0].Change != "update available" {
		t.Fatalf("change = %q, want update available", rows[0].Change)
	}
	if rows[0].Latest != wantLatest {
		t.Fatalf("latest = %q, want %q", rows[0].Latest, wantLatest)
	}
	if out := renderTable(rows); !strings.Contains(out, "values(") || !strings.Contains(out, "update available") {
		t.Fatalf("table: %s", out)
	}
}

// The cluster's providerConfigRef is attributed against cube.lock's cluster
// section (hub cluster only — spoke providerConfigRefs are out of scope,
// plan Amendment 3). A cube with no cluster section in the lock reads as new.
func TestRefRowsProviderConfig(t *testing.T) {
	pf := filepath.Join(t.TempDir(), "kind.yaml")
	if err := os.WriteFile(pf, []byte("networking: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cube := &config.Cube{}
	cube.Spec.Cluster.ProviderConfigRef = pf

	rows, err := refRows(context.Background(), cube, &lock.File{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "providerConfig("+pf+")" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Change != "new (not in cube.lock)" {
		t.Fatalf("change = %q", rows[0].Change)
	}

	lf := &lock.File{Cluster: &lock.ClusterLock{ProviderConfigRef: pf, ProviderConfigPin: rows[0].Latest}}
	rows, err = refRows(context.Background(), cube, lf, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Change != "up to date" {
		t.Fatalf("change = %q, want up to date", rows[0].Change)
	}
}

// A cube with no remote sources adds no rows at all — the table stays
// byte-identical to pre-RV5 output.
func TestRefRowsNoneWhenInline(t *testing.T) {
	cube := &config.Cube{}
	cube.Spec.Packs = []config.PackRef{{Ref: "./p"}}
	rows, err := refRows(context.Background(), cube, &lock.File{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

func TestRenderTableAligns(t *testing.T) {
	out := renderTable([]Row{{Name: "gitea", Current: "oci:sha256:aaaa", Latest: "oci:sha256:bbbb", Change: "update available"}})
	if !strings.Contains(out, "gitea") || !strings.Contains(out, "update available") {
		t.Fatalf("table: %s", out)
	}
}
