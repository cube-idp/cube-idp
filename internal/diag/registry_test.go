package diag

import (
	"os"
	"regexp"
	"testing"
)

// Every registered code must be explainable; explain is the lookup half of
// the stable-code contract (rustc --explain pattern): a code that ships
// must be explainable by `cube-idp explain`.
func TestEveryCodeHasDescription(t *testing.T) {
	for _, c := range AllCodes() {
		d, ok := Describe(c)
		if !ok || d.Summary == "" {
			t.Fatalf("code %s has no description", c)
		}
	}
}

// AllCodes is sorted and non-trivial: the registry is the complete index of
// codes.go, not a sample.
func TestAllCodesSortedAndComplete(t *testing.T) {
	codes := AllCodes()
	if len(codes) < 60 {
		t.Fatalf("registry suspiciously small: %d codes", len(codes))
	}
	for i := 1; i < len(codes); i++ {
		if codes[i-1] >= codes[i] {
			t.Fatalf("AllCodes not sorted: %s before %s", codes[i-1], codes[i])
		}
	}
	if _, ok := Describe(Code("CUBE-9999")); ok {
		t.Fatal("Describe must miss on an unregistered code")
	}
}

// The registry is complete against codes.go itself: every CUBE-xxxx string
// literal declared there must have an entry, so a new code cannot ship
// without its description (codes are append-only — this fence holds
// forever). Quoted literals only: prose mentions in comments don't count.
func TestRegistryCoversEveryDeclaredCode(t *testing.T) {
	src, err := os.ReadFile("codes.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`"(CUBE-\d{4})"`).FindAllStringSubmatch(string(src), -1)
	if len(declared) == 0 {
		t.Fatal("no CUBE-xxxx literals found in codes.go — regex drifted")
	}
	for _, m := range declared {
		if _, ok := Describe(Code(m[1])); !ok {
			t.Fatalf("codes.go declares %s but the registry has no entry", m[1])
		}
	}
	if len(declared) != len(AllCodes()) {
		t.Fatalf("registry has %d entries, codes.go declares %d", len(AllCodes()), len(declared))
	}
}

// Every code resolves to a documented range meaning.
func TestRangeMeaningCoversAllCodes(t *testing.T) {
	for _, c := range AllCodes() {
		if RangeMeaning(c) == "" {
			t.Fatalf("code %s has no range meaning", c)
		}
	}
}
