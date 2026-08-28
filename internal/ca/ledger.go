package ca

import (
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// Entry records one CA a user installed into an OS trust store.
// Exactly four fields, fixed at the M11 design gate: the ledger is a
// record of installations, not a certificate database
// (docs/domains/ca.md, trust distribution).
type Entry struct {
	// Cube is the name of the cube whose CA this entry records.
	Cube string `json:"cube"`
	// Fingerprint is the CA certificate's SHA-256, lowercase colon-free
	// hex, as Fingerprint renders it. It is the entry's identity — the
	// marker alone never authorizes a trust-store removal.
	Fingerprint string `json:"fingerprint"`
	// Store names the OS trust store the certificate went into.
	Store string `json:"store"`
	// Date is the installation date as YYYY-MM-DD. The domain never
	// reads the clock: the edge formats an injected now into this field.
	Date string `json:"date"`
}

// Ledger is the whole of ~/.cube-idp/trust.yaml — every cube, every
// store, one file. It is a plain config document, not a KRM object, so
// it carries no apiVersion or kind.
type Ledger struct {
	// Entries are the recorded installations, at most one per cube.
	Entries []Entry `json:"entries"`
}

// ParseLedger decodes ledger bytes. Empty or whitespace-only input is
// the empty ledger and not an error: an absent or emptied file records
// nothing. Decoding is otherwise strict — unparseable YAML, unknown
// fields, wrong types, an entry missing any of its four fields, and a
// duplicate cube across entries are all CUBE-CA-003, so a half-written
// or hand-edited ledger fails loudly instead of silently losing or
// duplicating a row.
func ParseLedger(data []byte) (Ledger, error) {
	if strings.TrimSpace(string(data)) == "" {
		return Ledger{}, nil
	}
	var ledger Ledger
	if err := yaml.UnmarshalStrict(data, &ledger); err != nil {
		return Ledger{}, newLedgerError("does not parse", err)
	}
	seen := make(map[string]bool, len(ledger.Entries))
	for i, e := range ledger.Entries {
		switch {
		case e.Cube == "":
			return Ledger{}, newLedgerError(fmt.Sprintf("is malformed: entry %d has no cube name", i), nil)
		case e.Fingerprint == "":
			return Ledger{}, newLedgerError(
				fmt.Sprintf("is malformed: entry %d (cube %q) has no fingerprint", i, e.Cube), nil)
		case e.Store == "":
			return Ledger{}, newLedgerError(
				fmt.Sprintf("is malformed: entry %d (cube %q) has no store", i, e.Cube), nil)
		case e.Date == "":
			return Ledger{}, newLedgerError(
				fmt.Sprintf("is malformed: entry %d (cube %q) has no date", i, e.Cube), nil)
		case seen[e.Cube]:
			return Ledger{}, newLedgerError(
				fmt.Sprintf("is malformed: cube %q has more than one entry", e.Cube), nil)
		}
		seen[e.Cube] = true
	}
	return ledger, nil
}

// Marshal renders the ledger as YAML, sorted by cube name so the file is
// byte-deterministic however the entries were accumulated. The receiver
// is never mutated.
func (l Ledger) Marshal() ([]byte, error) {
	sorted := slices.Clone(l.Entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return strings.Compare(a.Cube, b.Cube) })
	data, err := yaml.Marshal(Ledger{Entries: sorted})
	if err != nil {
		return nil, newLedgerError("cannot be rendered", err)
	}
	return data, nil
}

// Find returns the entry recorded for a cube, if the ledger has one.
func (l Ledger) Find(cube string) (Entry, bool) {
	i := slices.IndexFunc(l.Entries, func(e Entry) bool { return e.Cube == cube })
	if i < 0 {
		return Entry{}, false
	}
	return l.Entries[i], true
}

// Upsert returns a ledger with e recorded. One entry per cube: an
// existing entry for the same cube is replaced in place, never appended
// beside — a re-install must not grow a second row. The receiver and its
// backing array are left untouched.
func (l Ledger) Upsert(e Entry) Ledger {
	entries := slices.Clone(l.Entries)
	if i := slices.IndexFunc(entries, func(x Entry) bool { return x.Cube == e.Cube }); i >= 0 {
		entries[i] = e
		return Ledger{Entries: entries}
	}
	return Ledger{Entries: append(entries, e)}
}

// Remove returns a ledger without the cube's entry. Removing a cube the
// ledger never held is a no-op, not an error — the verbs are idempotent.
func (l Ledger) Remove(cube string) Ledger {
	entries := slices.DeleteFunc(slices.Clone(l.Entries), func(e Entry) bool { return e.Cube == cube })
	return Ledger{Entries: entries}
}
