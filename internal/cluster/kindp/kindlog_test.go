package kindp

import (
	"strings"
	"testing"
)

func TestKindLogAdapterForwardsInfoAndWarns(t *testing.T) {
	var got []string
	l := newKindLogger(func(line string) { got = append(got, line) })
	l.V(0).Infof("Ensuring node image (%s) ...", "kindest/node:v1.33.1")
	l.Warn("a warning")
	l.Error("an error")
	l.V(3).Info("debug noise") // must be dropped
	joined := strings.Join(got, "\n")
	for _, want := range []string{"Ensuring node image", "a warning", "an error"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "debug noise") {
		t.Fatalf("V(3) must be dropped: %q", joined)
	}
}
