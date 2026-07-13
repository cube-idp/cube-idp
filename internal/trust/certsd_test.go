package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCertsD(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	os.WriteFile(caPath, []byte("PEM"), 0o644)
	hostDir := filepath.Join(dir, "certsd")
	if err := WriteCertsD(hostDir, "registry.cube-idp.localtest.me", "http://localhost:30500", caPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(hostDir, "hosts.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `server = "https://registry.cube-idp.localtest.me"`) ||
		!strings.Contains(s, `[host."http://localhost:30500"]`) {
		t.Fatalf("hosts.toml:\n%s", s)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "ca.crt")); err != nil {
		t.Fatal("ca.crt must be copied alongside hosts.toml")
	}
}
