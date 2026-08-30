package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// finishBootstrap runs the edge's post-install steps and reports. The splice
// runs after InstallEngine returns — which is after the gateway unit
// reconciled — and before success is reported, the two bounds the contract
// fixes, without threading a callback into internal/bootstrap.
//
// Each step is gated on the units that make it meaningful: the rewrite needs
// both halves of the fabric it points into (hasGatewayFabric), and the
// certificate artifact exists only where the ca-secrets unit ran. Syncing is
// never conditioned on the CA having been minted — a reused CA must still
// restore a file the user deleted.
func finishBootstrap(cmd *cobra.Command, splice coreDNSSplicer, out bootstrapOutcome, deps bootstrapDeps) error {
	domain := ""
	if hasGatewayFabric(out.cfg.Spec.Prerequisites) {
		if err := splice(cmd.Context(), out.cfg.Name, out.domain); err != nil {
			return err
		}
		domain = out.domain
	}
	certPath := ""
	if hasPrerequisite(out.cfg.Spec.Prerequisites, v1alpha1.PrerequisiteCASecrets) {
		root, err := artifactRoot(deps.homeDir)
		if err != nil {
			return err
		}
		certPath = ca.CertPath(root, out.cfg.Name)
		// Whether this run rewrote the file is not what the line reports.
		if _, err := syncCertFile(certPath, out.ensured.CA.CertPEM); err != nil {
			return err
		}
	}
	renderBootstrapResult(cmd, out.cfg.Spec.Engine, domain, certPath)
	return nil
}

// renderBootstrapResult reports what bootstrap installed: the engine and its
// sync source, then one line per fabric the resolved list actually carried —
// domain is empty when the list lacked either half of the gateway fabric
// (hasGatewayFabric), certPath when no ca-secrets unit ran.
//
// The gateway line deliberately does not say "ready": the emitted Gateway's
// readiness is not gated in M11 and the splice's resolution probe is not a
// readiness gate, so what the run established is that the implementation
// reconciled and the rewrite is spliced. It carries no URL, because a
// truthful one needs the host port, which the edge cannot know.
func renderBootstrapResult(cmd *cobra.Command, engineSpec *v1alpha1.EngineSpec, domain, certPath string) {
	out := cmd.OutOrStdout()
	writeEngineLine(out, engineSpec)
	if domain != "" {
		_, _ = fmt.Fprintf(out, "gateway installed — *.%s routed to %s\n", domain, gateway.ServiceFQDN)
	}
	if certPath != "" {
		_, _ = fmt.Fprintf(out, "cube CA written to %s\n", certPath)
	}
}

// writeEngineLine reports the engine, naming the sync source when one is
// configured.
func writeEngineLine(out io.Writer, engineSpec *v1alpha1.EngineSpec) {
	if engineSpec != nil && engineSpec.Source != nil && engineSpec.Source.URL != "" {
		_, _ = fmt.Fprintf(out, "flux %s installed — syncing from %s (%s)\n",
			substrate.Version, engineSpec.Source.URL, engineSpec.Source.Kind)
		return
	}
	_, _ = fmt.Fprintf(out, "flux %s installed\n", substrate.Version)
}
