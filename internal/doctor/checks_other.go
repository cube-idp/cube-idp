//go:build !linux

package doctor

import (
	"golang.org/x/sys/unix"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func CheckDiskSpace(dir string, minBytes uint64) *diag.Finding {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free >= minBytes {
		return nil
	}
	return &diag.Finding{Code: diag.CodeDoctorDisk, Severity: diag.SeverityWarning,
		Message:     "low disk space at " + dir,
		Remediation: "free disk space or prune old images: `docker system prune`"}
}

func CheckInotify() []diag.Finding { return nil } // linux-only concern
