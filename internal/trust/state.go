package trust

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// State records host-machine side effects so `down` can revert them: OS
// trust-store changes are made only by the explicitly consented `trust`
// command and must be fully undone on teardown, so they have to be tracked.
type State struct {
	Installed bool   `yaml:"installed" json:"installed"` // CA present in OS trust stores
	CACert    string `yaml:"caCert" json:"caCert"`
}

func statePath(dir string) string { return filepath.Join(dir, "trust-state.yaml") }

func LoadState(dir string) (*State, error) {
	raw, err := os.ReadFile(statePath(dir))
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustStateFail, "cannot read the trust state file", "check permissions on "+dir)
	}
	var s State
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustStateFail, fmt.Sprintf("%s is corrupt", statePath(dir)),
			"if you know the CA is trusted, run `cube-idp trust --uninstall` then `cube-idp trust`; otherwise delete the file")
	}
	return &s, nil
}

func SaveState(dir string, s *State) error {
	out, err := yaml.Marshal(s)
	if err != nil {
		return diag.Wrap(err, diag.CodeTrustStateFail, "cannot serialize trust state", "this is a cube-idp bug — please report it")
	}
	if err := os.WriteFile(statePath(dir), out, 0o600); err != nil {
		return diag.Wrap(err, diag.CodeTrustStateFail, "cannot write the trust state file", "check permissions on "+dir)
	}
	return nil
}
