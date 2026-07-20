package ui

import (
	"io"
	"os"

	huh "charm.land/huh/v2"
)

// PromptsAllowed is the single gate every interactive surface routes
// through (spec Decisions 4/5): both streams are real TTYs, the resolved
// mode is rich, and no event pipeline currently owns the terminal.
func PromptsAllowed(in io.Reader, out io.Writer) bool {
	if pipelineActive.Load() {
		return false
	}
	if !IsTerminal(in) || !IsTerminal(out) {
		return false
	}
	m := CurrentMode()
	return m == ModeStyled || m == ModeLive
}

// ConfirmOpts configures one yes/no consent prompt.
type ConfirmOpts struct {
	Title       string
	Description string
	Default     bool // returned verbatim when prompting is not allowed
}

// Confirm asks a yes/no question through huh v2. When prompting is not
// allowed it returns o.Default immediately — it MUST NOT read or write.
// $ACCESSIBLE (non-empty) swaps the TUI for sequential prompts (gh's
// documented retrofit; spec Decision 8).
func Confirm(in io.Reader, out io.Writer, o ConfirmOpts) (bool, error) {
	if !PromptsAllowed(in, out) {
		return o.Default, nil
	}
	ok := o.Default
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(o.Title).Description(o.Description).Value(&ok),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}

// InputExact is the severe-tier consent (terraform/gh-repo-delete
// model): returns true only when the user types want exactly.
func InputExact(in io.Reader, out io.Writer, title, want string) (bool, error) {
	if !PromptsAllowed(in, out) {
		return false, nil
	}
	var got string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Value(&got),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return false, err
	}
	return got == want, nil
}
