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
	Path    string   `json:"path"` // e.g. "cube-idp pack push"
	Use     string   `json:"use"`
	Short   string   `json:"short"`
	Aliases []string `json:"aliases,omitempty"`
	Flags   []Flag   `json:"flags,omitempty"`
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
