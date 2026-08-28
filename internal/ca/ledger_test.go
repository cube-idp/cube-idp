package ca_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

const validLedgerYAML = `entries:
- cube: dev
  fingerprint: fce7a0ea053961041be2f12aa126ab20b1c38dff1656451b32199bfda93e0702
  store: macos-login
  date: "2026-08-28"
`

// TestParseLedger is the malformed-ledger contract: nothing recorded is
// an empty ledger, and everything a half-written or hand-edited file can
// look like is CUBE-CA-003 rather than a silently dropped row.
func TestParseLedger(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		want     ca.Ledger
		wantCode cubeerr.Code
	}{
		{name: "empty bytes are an empty ledger"},
		{name: "whitespace only is an empty ledger", data: "\n  \n"},
		{name: "an empty entry list parses", data: "entries: []\n", want: ca.Ledger{Entries: []ca.Entry{}}},
		{
			name: "a valid ledger round-trips",
			data: validLedgerYAML,
			want: ca.Ledger{Entries: []ca.Entry{{
				Cube:        "dev",
				Fingerprint: "fce7a0ea053961041be2f12aa126ab20b1c38dff1656451b32199bfda93e0702",
				Store:       "macos-login",
				Date:        "2026-08-28",
			}}},
		},
		{name: "unknown top-level field", data: validLedgerYAML + "extra: true\n", wantCode: ca.CodeLedger},
		{
			name:     "unknown field inside an entry",
			data:     strings.Replace(validLedgerYAML, "  store:", "  issuer: someone\n  store:", 1),
			wantCode: ca.CodeLedger,
		},
		{name: "entries is not a list", data: "entries: nope\n", wantCode: ca.CodeLedger},
		{name: "date is not a string", data: strings.Replace(validLedgerYAML, `"2026-08-28"`, "[1]", 1), wantCode: ca.CodeLedger},
		{name: "invalid YAML", data: "entries: [\n", wantCode: ca.CodeLedger},
		{name: "entry without a cube name", data: strings.Replace(validLedgerYAML, "cube: dev", `cube: ""`, 1), wantCode: ca.CodeLedger},
		{
			name:     "entry without a fingerprint",
			data:     strings.Replace(validLedgerYAML, "fingerprint: fce7a0ea", "fingerprint: #", 1),
			wantCode: ca.CodeLedger,
		},
		{name: "entry without a store", data: strings.Replace(validLedgerYAML, "store: macos-login", `store: ""`, 1), wantCode: ca.CodeLedger},
		{name: "entry without a date", data: strings.Replace(validLedgerYAML, `date: "2026-08-28"`, `date: ""`, 1), wantCode: ca.CodeLedger},
		{
			name: "duplicate cube across entries",
			data: validLedgerYAML + "- cube: dev\n" +
				"  fingerprint: aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa1\n" +
				"  store: p11-kit\n" +
				"  date: \"2026-08-29\"\n",
			wantCode: ca.CodeLedger,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ca.ParseLedger([]byte(tc.data))
			if tc.wantCode != "" {
				coded := assertCode(t, err, tc.wantCode)
				if !strings.Contains(coded.Remediation, "trust.yaml") {
					t.Errorf("remediation %q must name the ledger file", coded.Remediation)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLedger() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseLedger() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestLedgerFindUpsertRemove pins the mutation contract, including the
// property the value receivers make easy to break: every operation
// returns a new ledger and leaves the receiver's backing array alone.
func TestLedgerFindUpsertRemove(t *testing.T) {
	dev := ca.Entry{Cube: "dev", Fingerprint: "aa", Store: "macos-login", Date: "2026-08-28"}
	prod := ca.Entry{Cube: "prod", Fingerprint: "bb", Store: "p11-kit", Date: "2026-08-27"}
	stage := ca.Entry{Cube: "stage", Fingerprint: "cc", Store: "p11-kit", Date: "2026-08-26"}
	reinstalled := ca.Entry{Cube: "dev", Fingerprint: "zz", Store: "p11-kit", Date: "2026-08-29"}
	base := ca.Ledger{Entries: []ca.Entry{dev, prod}}

	cases := []struct {
		name string
		got  ca.Ledger
		want []ca.Entry
	}{
		{"upsert of a new cube appends", base.Upsert(stage), []ca.Entry{dev, prod, stage}},
		{"upsert of a known cube replaces in place", base.Upsert(reinstalled), []ca.Entry{reinstalled, prod}},
		{"remove drops the entry", base.Remove("dev"), []ca.Entry{prod}},
		{"remove of an absent cube is a no-op", base.Remove("nope"), []ca.Entry{dev, prod}},
		{"upsert into an empty ledger", ca.Ledger{}.Upsert(dev), []ca.Entry{dev}},
		{"remove from an empty ledger", ca.Ledger{}.Remove("dev"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got.Entries, tc.want) {
				t.Errorf("entries = %+v, want %+v", tc.got.Entries, tc.want)
			}
		})
	}
	if !reflect.DeepEqual(base.Entries, []ca.Entry{dev, prod}) {
		t.Errorf("the receiver was mutated: %+v", base.Entries)
	}

	if got, ok := base.Find("prod"); !ok || got != prod {
		t.Errorf("Find(prod) = (%+v, %v), want (%+v, true)", got, ok, prod)
	}
	if got, ok := base.Find("nope"); ok || got != (ca.Entry{}) {
		t.Errorf("Find(nope) = (%+v, %v), want (zero, false)", got, ok)
	}
}

// TestMarshalDeterministic: the ledger is sorted by cube on marshal, so
// the file's bytes never depend on the order installations happened in.
func TestMarshalDeterministic(t *testing.T) {
	dev := ca.Entry{Cube: "dev", Fingerprint: "aa", Store: "macos-login", Date: "2026-08-28"}
	prod := ca.Entry{Cube: "prod", Fingerprint: "bb", Store: "p11-kit", Date: "2026-08-27"}
	ordered := ca.Ledger{Entries: []ca.Entry{dev, prod}}
	reversed := ca.Ledger{Entries: []ca.Entry{prod, dev}}

	first, err := ordered.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := reversed.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("marshal is order-dependent:\n%s\nvs\n%s", first, second)
	}
	if !reflect.DeepEqual(reversed.Entries, []ca.Entry{prod, dev}) {
		t.Errorf("Marshal sorted the receiver in place: %+v", reversed.Entries)
	}

	parsed, err := ca.ParseLedger(first)
	if err != nil {
		t.Fatalf("ParseLedger(Marshal()) error = %v", err)
	}
	if !reflect.DeepEqual(parsed.Entries, []ca.Entry{dev, prod}) {
		t.Errorf("round trip = %+v, want %+v", parsed.Entries, []ca.Entry{dev, prod})
	}
}
