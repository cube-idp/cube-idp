package bundle

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ui"
)

// TestVendorPlainByteStable is Step 2's golden test (Task R3): Vendor driven
// through ui.RunPipeline with ModePlain forced must emit exactly today's
// plain sequence — the engine pack line (engine-as-pack: vendored first,
// like every pack), the demo "pack" line, no image lines (the fixture pins
// no Entry.Images), then the final "bundle written:" line — per G7's pinned
// bytes. This is the byte-identity arbiter for Vendor's io.Writer ->
// *ui.Console signature migration.
func TestVendorPlainByteStable(t *testing.T) {
	lockPath := writeLockFixture(t)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")

	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "vendor", &buf,
		func(ctx context.Context, con *ui.Console) error {
			con.Start("vendor", "")
			return Vendor(ctx, lockPath, out, "", con)
		})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// Engine-as-pack: the engine pack is vendored first, so the leading line
	// is now its start line; the demo pack line still appears.
	wantEngine := "▸ [vendor] pack cube-engine-flux (oci:sha256:" // digest varies per push
	if !bytes.HasPrefix([]byte(got), []byte(wantEngine)) {
		t.Fatalf("plain projection missing expected engine pack line prefix:\ngot:  %q\nwant prefix: %q", got, wantEngine)
	}
	if !bytes.Contains([]byte(got), []byte("▸ [vendor] pack demo (oci:sha256:")) {
		t.Fatalf("plain projection missing expected demo pack line:\ngot: %q", got)
	}
	const wantSuffix = "packs, 0 images)\n"
	if !bytes.HasSuffix([]byte(got), []byte(wantSuffix)) {
		t.Fatalf("plain projection missing expected bundle-written suffix:\ngot: %q\nwant suffix: %q", got, wantSuffix)
	}
	// Exactly five lines: engine pack start+done, demo pack start+done, and
	// the bundle-written line (no image lines — the fixture pins no images).
	if n := bytes.Count([]byte(got), []byte("\n")); n != 5 {
		t.Fatalf("want exactly 5 plain lines (engine+demo pack start/done + bundle written), got %d:\n%q", n, got)
	}
}

// TestVendorImagePlainByteStable covers the per-image line shape with
// writeLockFixtureWithImage: pack line, image line, bundle-written line.
func TestVendorImagePlainByteStable(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")

	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "vendor", &buf,
		func(ctx context.Context, con *ui.Console) error {
			con.Start("vendor", "")
			return Vendor(ctx, lockPath, out, "", con)
		})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	wantImageLine := "▸ [vendor] image " + imgRef + "\n"
	if !bytes.Contains([]byte(got), []byte(wantImageLine)) {
		t.Fatalf("plain projection missing image line %q, got:\n%q", wantImageLine, got)
	}
	// R1 start lines for the pack and image steps double their line count;
	// engine-as-pack adds the engine pack's start+done pair, so: engine pack
	// start+done, demo pack start+done, image start+done, bundle written.
	if n := bytes.Count([]byte(got), []byte("\n")); n != 7 {
		t.Fatalf("want exactly 7 plain lines (engine+demo pack start/done, image start/done, bundle written), got %d:\n%q", n, got)
	}
}

// TestVendorJSONStreamEmitsExpectedEventTypes is Step 2.4's JSON golden:
// --progress=json (ModeJSON) over the fixture emits one event per line
// covering the run lifecycle — run_started, step_started/step_done pairs
// for the pack (and, via the image fixture, the image), and a terminal
// run_done{ok:true}. Mirrors render/json_test.go's pattern: assert event
// types and key fields rather than full-line byte equality (timestamps are
// non-deterministic here; JSONWithClock's injectable-clock exact-byte
// coverage already lives in internal/ui/render).
func TestVendorJSONStreamEmitsExpectedEventTypes(t *testing.T) {
	prev := ui.CurrentMode()
	ui.SetMode(ui.ModeJSON)
	defer ui.SetMode(prev)

	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")

	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "vendor", &buf,
		func(ctx context.Context, con *ui.Console) error {
			con.Start("vendor", "")
			return Vendor(ctx, lockPath, out, "", con)
		})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	for _, want := range []string{
		`"type":"run_started","cmd":"vendor","cube":""`,
		`"type":"step_started","stage":"vendor","msg":"pack demo`,
		`"type":"step_done","stage":"vendor","msg":"pack demo`,
		`"type":"step_started","stage":"vendor","msg":"image ` + imgRef + `"`,
		`"type":"step_done","stage":"vendor","msg":"image ` + imgRef + `"`,
		`"type":"step_done","stage":"vendor","msg":"bundle written:`,
		`"type":"run_done","ok":true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON stream missing %q, got:\n%s", want, got)
		}
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !strings.Contains(lines[0], `"type":"run_started"`) {
		t.Fatalf("run_started must be the first line, got:\n%s", lines[0])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, `"type":"run_done"`) {
		t.Fatalf("run_done must be the last line on success, got:\n%s", last)
	}
}
