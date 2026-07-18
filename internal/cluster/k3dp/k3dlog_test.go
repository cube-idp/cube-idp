package k3dp

import (
	"strings"
	"testing"

	l "github.com/k3d-io/k3d/v5/pkg/logger"
)

// TestK3dLogForwarderSendsInfoToSink pins the logrus forwarder: after
// SetLogSink, a line logged through k3d's global logger arrives at the
// sink; a second SetLogSink swaps the destination without double-installing
// the hook (each line arrives exactly once, at the current sink only).
func TestK3dLogForwarderSendsInfoToSink(t *testing.T) {
	var first []string
	k := &K3d{}
	k.SetLogSink(func(line string) { first = append(first, line) })
	l.Log().Info("x")
	if joined := strings.Join(first, "\n"); !strings.Contains(joined, "x") {
		t.Fatalf("sink did not receive the Info line: %q", joined)
	}

	var second []string
	k.SetLogSink(func(line string) { second = append(second, line) })
	before := len(first)
	l.Log().Warn("swapped")
	if joined := strings.Join(second, "\n"); !strings.Contains(joined, "swapped") {
		t.Fatalf("swapped sink did not receive the Warn line: %q", joined)
	}
	if len(first) != before {
		t.Fatalf("old sink still receives lines after swap (hook double-installed?): %v", first)
	}
	if n := strings.Count(strings.Join(second, "\n"), "swapped"); n != 1 {
		t.Fatalf("line forwarded %d times, want exactly once", n)
	}
}
