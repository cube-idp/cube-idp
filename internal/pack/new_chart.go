package pack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"cuelang.org/go/cue/format"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The files --from-chart reads out of a chart directory, and nothing else.
// Templates, subcharts, and the rest of the chart are never touched: this is a
// metadata read, and the scaffolded pack stays thin.
const (
	chartFile  = "Chart.yaml"
	valuesFile = "values.yaml"
)

// chartPlaceholderURL is written into the scaffolded chart block because a
// local directory does not say where the chart is *published*, and the block
// needs a URL to be a valid pack at all.
//
// The host is under .invalid, which RFC 2606 reserves precisely so it can
// never resolve: the pack loads and renders immediately — which is what makes
// the scaffold checkable — while an author who forgets to replace it gets a
// visible Flux failure rather than a request to somebody else's registry.
const chartPlaceholderURL = "https://charts.example.invalid/REPLACE-ME"

// scaffoldPackVersion is the version every scaffolded pack starts at. It is
// the *pack's* version, deliberately not the chart's: they version
// independently, and a pack that inherited 6.5.4 would claim to be a release
// of software it only points at.
const scaffoldPackVersion = "0.1.0"

// chartMetadata is the part of a Chart.yaml this command reads.
type chartMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// chartScaffoldedFiles builds a thin helm pack from a local chart directory.
//
// The chart is read, never copied: what the pack keeps is coordinates and a
// values surface derived from the chart's own defaults. name overrides the
// chart's own name; dir supplies the last fallback, matching what an author
// means by `pack new ./traefik`.
func chartScaffoldedFiles(name, dir, chartDir string) (map[string][]byte, error) {
	meta, values, err := readChartDir(chartDir)
	if err != nil {
		return nil, err
	}
	if err := checkChartVersion(meta.Version); err != nil {
		return nil, newChartVersionUnusableError(chartDir, meta.Version)
	}

	packName := firstNonEmpty(name, meta.Name, filepath.Base(dir))
	// Canonicalize with CUE's own formatter: a derived file should look like
	// one an author wrote, and a formatter that parses it is a free check that
	// the derivation produced valid CUE before decodeMetadata reads it.
	src, err := format.Source([]byte(chartPackCUE(packName, meta, values)))
	if err != nil {
		return nil, newChartValuesError(chartDir, err)
	}
	files := map[string][]byte{MetadataFile: src}
	if _, _, err := decodeMetadata(src); err != nil {
		return nil, err
	}
	return files, nil
}

// readChartDir reads the chart's metadata and its default values. A missing
// values.yaml is not an error — a chart may expose nothing — but a values.yaml
// that is not a mapping is, because a values surface is a mapping by
// definition.
func readChartDir(chartDir string) (chartMetadata, map[string]any, error) {
	fsys := os.DirFS(chartDir)

	raw, err := fs.ReadFile(fsys, chartFile)
	if err != nil {
		return chartMetadata{}, nil, newChartSourceError(chartDir, err)
	}
	var meta chartMetadata
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return chartMetadata{}, nil, newChartSourceError(chartDir, err)
	}
	if meta.Name == "" || meta.Version == "" {
		return chartMetadata{}, nil, newChartSourceError(chartDir,
			fmt.Errorf("%s declares no name and version", chartFile))
	}

	raw, err = fs.ReadFile(fsys, valuesFile)
	if err != nil {
		return meta, nil, nil
	}
	var values map[string]any
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return chartMetadata{}, nil, newChartValuesError(chartDir, err)
	}
	return meta, values, nil
}

// chartPackCUE renders the scaffolded pack.cue: the pack's own identity, the
// chart coordinates with the URL left for the author, and the derived values
// surface.
func chartPackCUE(packName string, meta chartMetadata, values map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// %s — scaffolded by cube-idp from the %s chart.\n", packName, meta.Name)
	fmt.Fprintf(&b, "name:    %q\n", packName)
	fmt.Fprintf(&b, "version: %q\n", scaffoldPackVersion)
	b.WriteString("type:    \"helm\"\n\n")

	b.WriteString("// TODO: replace url with the repository this chart is published under.\n")
	b.WriteString("// A local directory does not say where a chart is published, so this is a\n")
	b.WriteString("// placeholder: the host is reserved and can never resolve. For a chart\n")
	b.WriteString("// published as an OCI artifact, replace the whole block with:\n")
	b.WriteString("//   chart: {kind: \"oci\", url: \"oci://<host>/<path>\", version: \"...\"}\n")
	b.WriteString("chart: {\n")
	fmt.Fprintf(&b, "\tkind:    %q\n", ChartKindRepo)
	fmt.Fprintf(&b, "\turl:     %q\n", chartPlaceholderURL)
	fmt.Fprintf(&b, "\tname:    %q\n", meta.Name)
	fmt.Fprintf(&b, "\tversion: %q\n", meta.Version)
	b.WriteString("}\n\n")

	b.WriteString(derivedValuesCUE(values))
	return b.String()
}

// derivedValuesCUE renders a #Values definition from the chart's own defaults.
//
// It is a lossy starting point and says so: every field is optional and every
// nested struct is left open, so the definition locks the top-level surface
// without forbidding a nested knob the chart's defaults happen to omit.
// Narrowing it is the author's job.
func derivedValuesCUE(values map[string]any) string {
	var b strings.Builder
	b.WriteString("// #Values is a closed definition: only the fields declared here may be\n")
	b.WriteString("// supplied. It was DERIVED from the chart's values.yaml and is a lossy\n")
	b.WriteString("// starting point, not a contract:\n")
	b.WriteString("//   - every field is optional, and nested structs are left open (...),\n")
	b.WriteString("//     so nothing the chart accepts is forbidden;\n")
	b.WriteString("//   - types and defaults are whatever the chart's own defaults happened\n")
	b.WriteString("//     to be — a list of one string reads as a list of strings.\n")
	b.WriteString("// Narrow it to the surface this pack means to expose: make fields\n")
	b.WriteString("// required, tighten types, close sub-structs, delete what is not yours.\n")
	b.WriteString("#Values: {\n")
	b.WriteString(cueFields(values, "\t"))
	b.WriteString("}\n")
	return b.String()
}

// cueFields renders one struct's fields, sorted so the same chart always
// scaffolds the same file.
func cueFields(values map[string]any, indent string) string {
	var b strings.Builder
	for _, key := range slices.Sorted(maps.Keys(values)) {
		fmt.Fprintf(&b, "%s%s?: %s\n", indent, cueKey(key), cueValue(values[key], indent))
	}
	return b.String()
}

// cueValue renders one value as a constraint carrying its observed default.
// A mapping becomes an open struct; everything else becomes its type with the
// observed value as the default, written as JSON — which is CUE for every
// shape a values document can hold.
func cueValue(value any, indent string) string {
	if nested, ok := value.(map[string]any); ok {
		return "{\n" + cueFields(nested, indent+"\t") + indent + "\t...\n" + indent + "}"
	}
	kind := cueKind(value)
	if value == nil {
		return kind
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		// Unreachable: the value came out of a YAML document decoded through
		// JSON, so it re-encodes. Degrade to the bare type rather than
		// writing a broken default.
		return kind
	}
	return kind + " | *" + string(encoded)
}

// cueKind names the CUE type a decoded values entry constrains to.
func cueKind(value any) string {
	switch v := value.(type) {
	case nil:
		return "_"
	case bool:
		return "bool"
	case string:
		return "string"
	case float64:
		if v == math.Trunc(v) && math.Abs(v) <= maxExactInt {
			return "int"
		}
		return "number"
	case []any:
		return "[...]"
	default:
		return "_"
	}
}

// cueIdent matches a key that may be written unquoted in CUE.
var cueIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// cueKey renders a values key as a field name, quoting anything that is not a
// plain identifier or that would collide with a CUE keyword.
func cueKey(key string) string {
	keywords := []string{"package", "import", "if", "for", "in", "let", "null", "true", "false"}
	if !cueIdent.MatchString(key) || slices.Contains(keywords, key) {
		return fmt.Sprintf("%q", key)
	}
	return key
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// The --from-chart coded errors live here beside the only code that raises
// them; the CUBE-PKG-* catalog itself stays in errors.go. All three are
// CodeScaffoldFailed: from the caller's side one thing went wrong — the pack
// could not be created — and the summary says which part of the chart caused
// it.

// newSourceConflictError reports both source flags given at once. Each names a
// different operation on a different kind of input, so there is no combined
// meaning to pick.
func newSourceConflictError() error {
	return cubeerr.Wrap(CodeScaffoldFailed,
		"--from and --from-chart were both given",
		"--from forks an existing pack and --from-chart scaffolds one from a chart: give exactly one", nil)
}

// newChartSourceError reports a --from-chart directory that is not a readable
// chart.
func newChartSourceError(chartDir string, cause error) error {
	return cubeerr.Wrap(CodeScaffoldFailed,
		fmt.Sprintf("cannot read a chart at %q", chartDir),
		fmt.Sprintf("point --from-chart at a directory holding a %s with a name and a version", chartFile), cause)
}

// newChartValuesError reports a chart whose values.yaml is unreadable or is
// not a mapping. A values surface is a mapping by definition, so a list or a
// scalar is not a values document this command can derive from.
func newChartValuesError(chartDir string, cause error) error {
	return cubeerr.Wrap(CodeScaffoldFailed,
		fmt.Sprintf("cannot read %s in the chart at %q", valuesFile, chartDir),
		fmt.Sprintf("%s must be one YAML mapping, or absent — a chart that exposes nothing scaffolds an empty #Values", valuesFile), nil)
}

// newChartVersionUnusableError reports a chart whose own version cannot be a
// pack's chart version. Helm requires SemVer in Chart.yaml, so this is rare —
// a leading "v" is the usual cause — and it is reported against the chart
// rather than as a schema failure in a file the author never wrote.
func newChartVersionUnusableError(chartDir, version string) error {
	return cubeerr.Wrap(CodeScaffoldFailed,
		fmt.Sprintf("the chart at %q declares version %q, which is not an exact SemVer", chartDir, version),
		"scaffold the pack without --from-chart and write the published version by hand: a chart version must be exact (6.5.4), with no leading v, range, or build metadata", nil)
}
