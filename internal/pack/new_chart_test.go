package pack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// writeChart lays out a chart directory. An empty values body writes no
// values.yaml at all, which is how a chart that exposes nothing looks.
func writeChart(t *testing.T, chart, values string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "chart")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", dir, err)
	}
	if chart != "" {
		if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chart), 0o600); err != nil {
			t.Fatalf("write Chart.yaml = %v", err)
		}
	}
	if values != "" {
		if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(values), 0o600); err != nil {
			t.Fatalf("write values.yaml = %v", err)
		}
	}
	return dir
}

// scaffoldFromChart runs pack new --from-chart into a fresh directory and
// returns that directory and the pack.cue it wrote.
func scaffoldFromChart(t *testing.T, opts pack.NewOptions, chartDir string) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pack")
	opts.Dir, opts.FromChart = dir, chartDir
	if err := pack.New(t.Context(), opts); err != nil {
		t.Fatalf("New(--from-chart) = error %v, want a pack", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "pack.cue"))
	if err != nil {
		t.Fatalf("read scaffolded pack.cue = %v", err)
	}
	return dir, string(src)
}

const podinfoChart = "apiVersion: v2\nname: podinfo\nversion: 6.5.4\ndescription: tiny\n"

// The round trip is the whole point: a chart scaffolds a pack that loads and
// renders the delegation, so an author can check what they were handed.
func TestNewFromChartRoundTrips(t *testing.T) {
	chart := writeChart(t, podinfoChart, "replicaCount: 2\n")
	dir, _ := scaffoldFromChart(t, pack.NewOptions{}, chart)

	p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
	if err != nil {
		t.Fatalf("Load(scaffolded) = error %v, want a pack", err)
	}
	meta := p.Metadata()
	if meta.Name != "podinfo" || meta.Type != pack.TypeHelm || meta.Version != "0.1.0" {
		t.Errorf("Metadata() = %+v, want podinfo/0.1.0/helm", meta)
	}
	if meta.Chart == nil {
		t.Fatal("scaffolded pack carries no chart block")
	}
	// The chart's version is the chart's; the pack starts at its own 0.1.0.
	if meta.Chart.Version != "6.5.4" || meta.Chart.Name != "podinfo" {
		t.Errorf("Chart = %+v, want name podinfo version 6.5.4", *meta.Chart)
	}
	if meta.Chart.Kind != pack.ChartKindRepo {
		t.Errorf("Chart.Kind = %q, want %q", meta.Chart.Kind, pack.ChartKindRepo)
	}

	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render(scaffolded) = error %v, want a plan", err)
	}
	if len(plan.Objects) != 2 {
		t.Fatalf("Render() produced %d objects, want 2", len(plan.Objects))
	}
	for i, want := range []string{"HelmRepository", "HelmRelease"} {
		if got := plan.Objects[i].GetKind(); got != want {
			t.Errorf("Render() object %d = %s, want %s", i, got, want)
		}
	}
}

// A thin pack means the chart is read, not copied: nothing but pack.cue is
// written, however much the chart directory holds.
func TestNewFromChartWritesOnlyPackCUE(t *testing.T) {
	chart := writeChart(t, podinfoChart, "replicaCount: 2\n")
	for _, extra := range []string{"templates/deploy.yaml", "charts/sub/Chart.yaml", ".helmignore"} {
		path := filepath.Join(chart, extra)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll = %v", err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
			t.Fatalf("write %s = %v", extra, err)
		}
	}

	dir, _ := scaffoldFromChart(t, pack.NewOptions{}, chart)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) = %v", dir, err)
	}
	if len(entries) != 1 || entries[0].Name() != "pack.cue" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("scaffolded pack holds %v, want only [pack.cue]", names)
	}
}

// The url cannot be derived from a directory, so the scaffold carries a
// placeholder under a reserved host and says so — the pack still loads, which
// is what lets an author render it before filling the url in.
func TestNewFromChartURLIsAReservedPlaceholder(t *testing.T) {
	chart := writeChart(t, podinfoChart, "")
	_, src := scaffoldFromChart(t, pack.NewOptions{}, chart)

	if !strings.Contains(src, ".invalid") {
		t.Errorf("scaffolded pack.cue url is not under a reserved host:\n%s", src)
	}
	if !strings.Contains(src, "TODO") {
		t.Errorf("scaffolded pack.cue does not tell the author to replace the url:\n%s", src)
	}
}

// The pack's name comes from --name first, then the chart, then the directory.
func TestNewFromChartName(t *testing.T) {
	tests := []struct {
		name  string
		chart string
		opts  pack.NewOptions
		want  string
	}{
		{
			name:  "the chart's name by default",
			chart: podinfoChart,
			want:  "podinfo",
		},
		{
			name:  "--name overrides the chart's",
			chart: podinfoChart,
			opts:  pack.NewOptions{Name: "gateway"},
			want:  "gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := writeChart(t, tt.chart, "")
			dir, _ := scaffoldFromChart(t, tt.opts, chart)
			p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			if got := p.Metadata().Name; got != tt.want {
				t.Errorf("pack name = %q, want %q", got, tt.want)
			}
			// The chart block always keeps the chart's own name: renaming the
			// pack does not rename the chart it points at.
			if got := p.Metadata().Chart.Name; got != "podinfo" {
				t.Errorf("chart name = %q, want podinfo", got)
			}
		})
	}
}

// Every way a chart directory can fail to be one is the same coded scaffold
// failure, with a summary naming which part of the chart was at fault.
func TestNewFromChartErrors(t *testing.T) {
	tests := []struct {
		name   string
		chart  string
		values string
		want   cubeerr.Code
	}{
		{
			name: "no Chart.yaml at all",
			want: pack.CodeScaffoldFailed,
		},
		{
			name:  "Chart.yaml is not YAML",
			chart: "name: [oops\n",
			want:  pack.CodeScaffoldFailed,
		},
		{
			name:  "Chart.yaml declares no version",
			chart: "name: podinfo\n",
			want:  pack.CodeScaffoldFailed,
		},
		{
			name:  "Chart.yaml declares no name",
			chart: "version: 1.0.0\n",
			want:  pack.CodeScaffoldFailed,
		},
		{
			// Helm requires SemVer, so this is rare — but a leading v cannot
			// be a chart version here, and the error names the chart.
			name:  "chart version is not exact",
			chart: "name: podinfo\nversion: v6.5.4\n",
			want:  pack.CodeScaffoldFailed,
		},
		{
			name:   "values.yaml is not a mapping",
			chart:  podinfoChart,
			values: "- a\n- b\n",
			want:   pack.CodeScaffoldFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := writeChart(t, tt.chart, tt.values)
			dir := filepath.Join(t.TempDir(), "pack")
			err := pack.New(t.Context(), pack.NewOptions{Dir: dir, FromChart: chart})
			wantCode(t, err, tt.want)
			// Nothing is assembled on disk until the pack is known to be sound.
			if _, statErr := os.Stat(dir); statErr == nil {
				t.Errorf("Stat(%s) succeeded, want no directory left behind", dir)
			}
		})
	}
}

// --from forks a pack and --from-chart scaffolds one from a chart: each names
// a different operation on a different kind of input, so both at once has no
// combined meaning.
func TestNewFromChartConflictsWithFrom(t *testing.T) {
	chart := writeChart(t, podinfoChart, "")
	err := pack.New(t.Context(), pack.NewOptions{
		Dir:       filepath.Join(t.TempDir(), "pack"),
		From:      "./somewhere",
		FromChart: chart,
	})
	wantCode(t, err, pack.CodeScaffoldFailed)
}
