package render

import "testing"

func TestJSONSchemasCoversEveryEnvelopeType(t *testing.T) {
	// If a new json* envelope struct is added to json.go without being
	// registered here, the truth index silently under-reports the machine
	// output surface. Count is asserted so the failure names the gap.
	if got := len(JSONSchemas()); got != 12 {
		t.Fatalf("JSONSchemas() = %d entries, want 12 — update JSONSchemas() and this count together", got)
	}
}
