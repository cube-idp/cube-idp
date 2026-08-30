package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestRenderBootstrapResult: the report names only what the resolved list
// actually installed, and says what the run established — not that anything
// is "ready", and never a URL, whose port the edge cannot know.
func TestRenderBootstrapResult(t *testing.T) {
	t.Parallel()
	source := &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/repo",
	}}
	cases := []struct {
		name     string
		engine   *v1alpha1.EngineSpec
		domain   string
		certPath string
		want     []string
		absent   []string
	}{
		{
			name: "the engine alone", want: []string{"flux " + substrate.Version + " installed\n"},
			absent: []string{"gateway installed", "cube CA"},
		},
		{
			name: "the engine with a sync source", engine: source,
			want: []string{"syncing from https://github.com/org/repo (git)"},
		},
		{
			name: "the full fabric", domain: "dev.cube.test", certPath: "/home/you/.cube-idp/dev/ca.crt",
			want: []string{
				"gateway installed — *.dev.cube.test routed to " + gateway.ServiceFQDN + "\n",
				"cube CA written to /home/you/.cube-idp/dev/ca.crt\n",
			},
			absent: []string{"ready", "https://", ":8443"},
		},
		{
			name: "a gateway without the CA unit", domain: "dev.cube.test",
			want: []string{"gateway installed"}, absent: []string{"cube CA"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)

			renderBootstrapResult(cmd, tc.engine, tc.domain, tc.certPath)
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not carry %q", out.String(), want)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output %q carries %q, which it must not claim", out.String(), absent)
				}
			}
		})
	}
}

// TestFinishBootstrapSyncsCertificate: the CA artifact is written wherever
// the list carries the ca-secrets unit, and the sync is never conditioned on
// the CA having been minted — a reused CA must still restore a deleted file.
// A nil client is the assertion that no splice runs without gateway-platform.
func TestFinishBootstrapSyncsCertificate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		units    []v1alpha1.PrerequisiteSpec
		wantFile bool
	}{
		{name: "the ca-secrets unit writes the certificate",
			units:    []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteCASecrets}},
			wantFile: true},
		{name: "a list without it writes nothing",
			units: []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayAPICRDs}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Prerequisites: tc.units}}
			cfg.Name = "dev"
			out := bootstrapOutcome{cfg: cfg, domain: "dev.cube.test", ensured: ca.EnsureResult{
				CA: ca.Material{CertPEM: []byte("-----BEGIN CERTIFICATE-----\n")},
			}}
			deps := bootstrapDeps{now: func() time.Time { return testNow },
				homeDir: func() (string, error) { return home, nil }}
			cmd := &cobra.Command{}
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)

			if err := finishBootstrap(cmd, refuseSplice(t), out, deps); err != nil {
				t.Fatalf("finishBootstrap: %v", err)
			}
			path := ca.CertPath(trustRoot(home), "dev")
			got, err := os.ReadFile(path)
			if !tc.wantFile {
				if err == nil {
					t.Fatalf("a list without ca-secrets wrote %s", path)
				}
				if strings.Contains(stdout.String(), "cube CA") {
					t.Errorf("output %q reports a certificate that was not written", stdout.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if !bytes.Equal(got, out.ensured.CA.CertPEM) {
				t.Errorf("wrote %q, want the effective CA certificate", got)
			}
			if !strings.Contains(stdout.String(), "cube CA written to "+path) {
				t.Errorf("output %q does not name the certificate path", stdout.String())
			}
		})
	}
}

// refuseSplice is a splicer that fails the test if it runs — the assertion
// for every row whose list does not carry the whole gateway fabric.
func refuseSplice(t *testing.T) coreDNSSplicer {
	t.Helper()
	return func(context.Context, string, string) error {
		t.Error("the splice ran for a list that does not install the whole gateway fabric")
		return nil
	}
}

// TestFinishBootstrapSplicesOnlyWithTheWholeFabric: the rewrite needs a
// target to point at (the platform unit's stable Service) and an
// implementation behind it (the gateway unit). A list carrying one half is
// constructible by choice and gets no splice and no gateway line — pointing
// cluster DNS at a half fabric would be the silent degrade the safety
// envelope forbids.
func TestFinishBootstrapSplicesOnlyWithTheWholeFabric(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		units      []string
		wantSplice bool
	}{
		{name: "the platform unit alone splices nothing",
			units: []string{v1alpha1.PrerequisiteGatewayPlatform}},
		{name: "the gateway unit alone splices nothing",
			units: []string{v1alpha1.PrerequisiteTraefikGateway}},
		{name: "neither half splices nothing",
			units: []string{v1alpha1.PrerequisiteGatewayAPICRDs}},
		{name: "both halves splice",
			units:      []string{v1alpha1.PrerequisiteGatewayPlatform, v1alpha1.PrerequisiteTraefikGateway},
			wantSplice: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			units := make([]v1alpha1.PrerequisiteSpec, 0, len(tc.units))
			for _, name := range tc.units {
				units = append(units, v1alpha1.PrerequisiteSpec{Name: name})
			}
			cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Prerequisites: units}}
			cfg.Name = "dev"
			spliced := 0
			splice := func(_ context.Context, cube, domain string) error {
				spliced++
				if cube != "dev" || domain != testDomain {
					t.Errorf("splice(%q, %q), want (dev, %s)", cube, domain, testDomain)
				}
				return nil
			}
			cmd := &cobra.Command{}
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)

			err := finishBootstrap(cmd, splice, bootstrapOutcome{cfg: cfg, domain: testDomain}, bootstrapDeps{})
			if err != nil {
				t.Fatalf("finishBootstrap: %v", err)
			}
			want := 0
			if tc.wantSplice {
				want = 1
			}
			if spliced != want {
				t.Errorf("spliced %d times, want %d", spliced, want)
			}
			if got := strings.Contains(stdout.String(), "gateway installed"); got != tc.wantSplice {
				t.Errorf("output %q reports a gateway line = %v, want %v", stdout.String(), got, tc.wantSplice)
			}
		})
	}
}
