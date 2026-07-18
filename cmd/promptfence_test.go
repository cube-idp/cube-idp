package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestPromptFenceNeverBlocksOnBufferStdin is the spec §6.3 prompt-gating
// fence, made a TABLE so a future prompt-capable command cannot dodge it:
// every command that owns a consent or menu prompt is driven end-to-end
// with an empty bytes.Buffer as stdin and MUST complete — refusing, aborting,
// or erroring is fine; blocking is the one forbidden outcome (CI must never
// hang, Global Constraints). ui.PromptsAllowed is false for every
// non-*os.File stream combination (internal/ui/prompt_test.go proves that
// half), so no huh form may ever engage on these rows; the 5s select is
// headroom so a violation fails in seconds, not as a stuck suite.
//
// Rows and their expected non-blocking outcome:
//   - down            → CUBE-0010 refusal before any pipeline (TE-3.4 / R3)
//   - trust           → text-fallback prompt reads EOF → "aborted", exit 0
//   - upgrade --plan  → no cube.lock next to the fixture → typed error
//   - pack install    → bare invocation on non-TTY → CUBE-0010 refusal
//
// (plugin trust's non-interactive CUBE-7104 refusal keeps its own fence in
// the plugin tests — its wording is a frozen security contract.)
func TestPromptFenceNeverBlocksOnBufferStdin(t *testing.T) {
	rows := []struct {
		name  string
		setup func(t *testing.T) []string // per-row fixture; returns the argv
	}{
		{"down", func(t *testing.T) []string {
			return []string{"down", "-f", cubeYAMLFixture(t)}
		}},
		{"trust", func(t *testing.T) []string {
			restore := trustInstall
			trustInstall = func(string) error {
				t.Error("trust must never install without consent")
				return nil
			}
			t.Cleanup(func() { trustInstall = restore })
			return []string{"trust"}
		}},
		{"upgrade", func(t *testing.T) []string {
			return []string{"upgrade", "--plan", "-f", cubeYAMLFixture(t)}
		}},
		{"pack install", func(t *testing.T) []string {
			cubeYAMLFixture(t)
			return []string{"pack", "install"}
		}},
		{"spoke remove --delete-cluster", func(t *testing.T) []string {
			// S1: consent gate is live, deletion itself lands in S3 — the
			// row pins that the Confirm path refuses (CUBE-0010) on a
			// buffer stdin instead of blocking.
			p := writeSpokeFixture(t)
			mustRunCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
			return []string{"spoke", "remove", "staging", "--delete-cluster", "-f", p}
		}},
		{"plugin install (official index)", func(t *testing.T) []string {
			// P10: `plugin install` from the official index writes the binary
			// then hands off to the sha256 trust-consent seam. On a buffer
			// stdin (non-TTY) that seam must refuse with CUBE-7104 — never
			// engage a huh form — so this row pins non-blocking. seedPluginIndex
			// serves the index + platform blob off an in-process registry.
			seedPluginIndex(t, []byte("#!/bin/sh\nexit 0\n"))
			return []string{"plugin", "install", "hello"}
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			args := row.setup(t)
			var out bytes.Buffer
			done := make(chan error, 1)
			go func() {
				root := NewRootCmd()
				root.SetOut(&out)
				root.SetErr(&out)
				root.SetIn(&bytes.Buffer{}) // non-TTY stdin: prompting is forbidden
				root.SetArgs(args)
				done <- root.ExecuteContext(context.Background())
			}()
			select {
			case <-done:
				// Completion IS the assertion. Each row's exact outcome
				// (refusal code, abort wording, twin flags) is pinned by its
				// own command tests; this fence only forbids blocking.
			case <-time.After(5 * time.Second):
				t.Fatalf("%q blocked on empty-buffer stdin — a prompt escaped the ui.PromptsAllowed gate\noutput so far:\n%s",
					row.name, out.String())
			}
		})
	}
}
