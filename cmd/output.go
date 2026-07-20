package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// validateProgressFlag rejects an unrecognized --progress value with a typed
// CUBE-0007 preflight error — mode selection happens once, in root.go's
// PersistentPreRunE, so an unknown value must be rejected there rather than
// preflight error"). The empty string never reaches here — the flag defaults
// to "auto" — but it is accepted for safety. auto/plain/live/json map onto the
// resolve ladder's rungs 1–3 (json/plain/live) or fall through (auto).
func validateProgressFlag(v string) error {
	switch v {
	case "", "auto", "plain", "live", "json":
		return nil
	}
	return diag.New(diag.CodeBadFlagValue,
		fmt.Sprintf("unknown --progress value %q", v),
		"use one of: auto, plain, live, json")
}

// validateColorFlag rejects an unrecognized --color value with the same
// CUBE-0007 preflight code the other enum flags use. The empty string never
// reaches here — the flag defaults to "auto" — but it is accepted for
// safety. The value itself is consumed by ui.SetColorPolicy, never by
// a mode rung.
func validateColorFlag(v string) error {
	switch v {
	case "", "auto", "always", "never":
		return nil
	}
	return diag.New(diag.CodeBadFlagValue,
		fmt.Sprintf("unknown --color value %q", v),
		"use one of: auto, always, never")
}

// docSchemaVersion is the "v" field every JSON document carries — the same
// experimental-until-D5-freeze contract the event stream uses (design doc
// §5.3). Documents (this file) are the request/response counterpart to the
// event stream: a single gh-style final object, never a line stream.
const docSchemaVersion = 1

// addOutputFlag registers the -o/--output flag on a request/response command
// (status, doctor, get secrets) whose only recognized value is "json" — the
// gh-style document mode. Empty means the command's normal
// styled/plain rendering.
func addOutputFlag(c *cobra.Command, target *string) {
	c.Flags().StringVarP(target, "output", "o", "", "output format: json (a single gh-style document; EXPERIMENTAL)")
}

// validateOutputFlag rejects an unrecognized --output value with the same
// CUBE-0007 code, so a typo like `--output yaml` fails loudly instead of
// silently falling back to text.
func validateOutputFlag(v string) error {
	switch v {
	case "", "json":
		return nil
	}
	return diag.New(diag.CodeBadFlagValue,
		fmt.Sprintf("unknown --output value %q", v),
		"the only supported document format is: json")
}

// wantJSONDoc reports whether a request/response command should emit its JSON
// document: either --output json was passed, or the process-wide mode resolved
// to ModeJSON (on a request/response command ModeJSON becomes a document,
// not the event stream). It also validates the flag, returning a CUBE-0007
// error on a bad value.
func wantJSONDoc(output string) (bool, error) {
	if err := validateOutputFlag(output); err != nil {
		return false, err
	}
	return output == "json" || ui.CurrentMode() == ui.ModeJSON, nil
}

// writeJSONDoc marshals v as a single pretty-printed JSON object followed by a
// newline — the gh convention: one readable document per invocation (distinct
// from the event stream's one-object-per-line). v is expected to embed a
// jsonDocHead so every document carries "v":docSchemaVersion.
func writeJSONDoc(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// jsonDocHead is the common prefix of every JSON document — the schema version
// field, mirroring the event stream's jsonHead.
type jsonDocHead struct {
	V int `json:"v"`
}
