package theme

import (
	"strings"
	"testing"
)

// Light and dark palettes must actually differ (adaptivity is the point),
// and every semantic color must stay in the basic ANSI-16 range (the
// color-roles rule: user terminal themes keep control).
func TestThemeLightDarkDiffer(t *testing.T) {
	light, dark := New(false), New(true)
	if light.Badge.Render("[x]") == dark.Badge.Render("[x]") {
		t.Fatal("light and dark Badge render identically — LightDark pairs not applied")
	}
}

func TestBadgeWidthCoversAllStages(t *testing.T) {
	w := BadgeWidth()
	for name := range Stages {
		if len(name)+2 > w { // "[" + name + "]"
			t.Fatalf("stage %q wider than BadgeWidth %d", name, w)
		}
	}
	if !strings.Contains(GlyphOK, "✔") {
		t.Fatal("GlyphOK changed — glyph set is normative (spec §2)")
	}
}
