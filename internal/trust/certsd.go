package trust

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// WriteCertsD prepares a containerd certs.d host directory that maps image
// refs on <registryHost> to <endpoint>. The kind provider bind-mounts dir to
// /etc/containerd/certs.d/<registryHost> on every node (D6).
//
// Endpoint choice: kind nodes cannot reach the gateway through localtest.me
// (it resolves to the node itself), so the default endpoint is the zot
// NodePort on the node's loopback: http://localhost:30500.
func WriteCertsD(dir, registryHost, endpoint, caCertPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return diag.Wrap(err, diag.CodeTrustCAFail, "cannot create the certs.d staging dir", "check permissions on the cube-idp config dir")
	}
	ca, err := os.ReadFile(caCertPath)
	if err != nil {
		return diag.Wrap(err, diag.CodeTrustCAFail, "cannot read the CA certificate", "run any cube-idp command that ensures the CA first (`up`)")
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca, 0o644); err != nil {
		return diag.Wrap(err, diag.CodeTrustCAFail, "cannot stage ca.crt", "check permissions")
	}
	hosts := fmt.Sprintf(`server = "https://%s"

[host.%q]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/%s/ca.crt"
`, registryHost, endpoint, registryHost)
	if err := os.WriteFile(filepath.Join(dir, "hosts.toml"), []byte(hosts), 0o644); err != nil {
		return diag.Wrap(err, diag.CodeTrustCAFail, "cannot write hosts.toml", "check permissions")
	}
	return nil
}
