package trust

import (
	"github.com/smallstep/truststore"

	"github.com/rafpe/cube-idp/internal/diag"
)

// InstallOS installs the cube-idp CA into the OS trust stores (the mkcert
// mechanism, D6). Callers MUST have obtained explicit user consent first.
func InstallOS(dir string) error {
	ca, err := EnsureCA(dir)
	if err != nil {
		return err
	}
	if err := truststore.InstallFile(ca.CertPath, truststore.WithFirefox()); err != nil {
		return diag.Wrap(err, diag.CodeTrustOSStoreFail, "installing the CA into the OS trust store failed",
			"you may be prompted for your password/sudo by the OS; re-run `cube-idp trust`. Manual alternative: import "+ca.CertPath+" into your trust store")
	}
	return SaveState(dir, &State{Installed: true, CACert: ca.CertPath})
}

// UninstallOS reverts InstallOS. Safe to call when nothing was installed.
func UninstallOS(dir string) error {
	st, err := LoadState(dir)
	if err != nil {
		return err
	}
	if !st.Installed {
		return nil
	}
	if err := truststore.UninstallFile(st.CACert, truststore.WithFirefox()); err != nil {
		return diag.Wrap(err, diag.CodeTrustOSStoreRevert, "removing the CA from the OS trust store failed",
			"remove it manually from your OS trust store: "+st.CACert+", then delete the trust state file")
	}
	return SaveState(dir, &State{Installed: false})
}
