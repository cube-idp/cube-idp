package upgrade

import (
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/lock"
)

func TestPlanRowClassification(t *testing.T) {
	locked := &lock.Entry{Ref: "./p", Name: "p", Resolved: "dir:h1:old"}
	if row := classify(locked, "dir:h1:old"); row.Change != "up to date" {
		t.Fatalf("same pin: %+v", row)
	}
	if row := classify(locked, "dir:h1:new"); row.Change != "update available" {
		t.Fatalf("moved pin: %+v", row)
	}
	if row := classify(nil, "dir:h1:new"); row.Change != "new (not in cube.lock)" {
		t.Fatalf("missing lock entry: %+v", row)
	}
}

func TestRenderTableAligns(t *testing.T) {
	out := renderTable([]Row{{Name: "gitea", Current: "oci:sha256:aaaa", Latest: "oci:sha256:bbbb", Change: "update available"}})
	if !strings.Contains(out, "gitea") || !strings.Contains(out, "update available") {
		t.Fatalf("table: %s", out)
	}
}
