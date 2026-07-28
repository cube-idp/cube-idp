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
		// Only well-formed CUBE-CFG-NNN codes map to 2; malformed codes fall
		// back to the generic error exit.
		{"malformed bare code maps to 1", cubeerr.Wrap("WEIRD", "s", "r", nil), cubeerr.ExitError},
		{"malformed two-part code maps to 1", cubeerr.Wrap("CUBE-CFG", "s", "r", nil), cubeerr.ExitError},
		{"lowercase code maps to 1", cubeerr.Wrap("cube-cfg-001", "s", "r", nil), cubeerr.ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cubeerr.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
