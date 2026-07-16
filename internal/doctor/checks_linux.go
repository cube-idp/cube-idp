//go:build linux

package doctor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func CheckDiskSpace(dir string, minBytes uint64) *diag.Finding {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil // cannot measure: stay silent rather than guess
	}
	free := st.Bavail * uint64(st.Bsize)
	if free >= minBytes {
		return nil
	}
	return &diag.Finding{Code: diag.CodeDoctorDisk, Severity: diag.SeverityWarning,
		Message:     fmt.Sprintf("only %.1f GiB free at %s (kind images want ≥ %.0f GiB)", float64(free)/(1<<30), dir, float64(minBytes)/(1<<30)),
		Remediation: "free disk space or prune old images: `docker system prune`"}
}

// inotifyLimit is one /proc/sys/fs/inotify knob doctor checks against kind's
// commonly-needed minimum.
type inotifyLimit struct {
	path string
	min  int64
}

var inotifyLimits = []inotifyLimit{
	{"/proc/sys/fs/inotify/max_user_watches", 524288},
	{"/proc/sys/fs/inotify/max_user_instances", 512},
}

func CheckInotify() []diag.Finding {
	var out []diag.Finding
	for _, lim := range inotifyLimits {
		raw, err := os.ReadFile(lim.path)
		if err != nil {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || v >= lim.min {
			continue
		}
		out = append(out, diag.Finding{Code: diag.CodeDoctorInotify, Severity: diag.SeverityWarning,
			Message:     fmt.Sprintf("%s = %d (kind clusters commonly need ≥ %d)", lim.path, v, lim.min),
			Remediation: fmt.Sprintf("sudo sysctl %s=%d", strings.TrimPrefix(strings.ReplaceAll(lim.path, "/", "."), ".proc.sys."), lim.min)})
	}
	return out
}
