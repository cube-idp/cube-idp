// Package bundle implements `cube-idp vendor` (spec §4.1): a pure cube.lock
// consumer that pulls every pinned pack and image into one self-contained
// tar.gz for air-gapped installs. Vendor produces the bundle; Open/Verify/
// PackDir/ImageTars/Close read one back (Task 7's loaders build on Open).
//
// Bundle layout (versioned via manifest.json's formatVersion):
//
//	manifest.json     — {"formatVersion":1,"platform":"linux/amd64",
//	                     "createdAt":RFC3339,"lockDigest":"sha256:…",
//	                     "images":{"<original ref>":"images/<n>.tar", …}}
//	cube.lock          — verbatim copy of the lock the bundle was built from
//	packs/<name>/       — pack source dir at the locked pin (Fetch-compatible)
//	images/<n>.tar      — ONE tar per locked image (Owner Decisions #2): a
//	                       single-image OCI layout, tarred; the original ref
//	                       is recorded in manifest.json's images map AND as
//	                       the layout index's org.opencontainers.image.ref.
//	                       name annotation. containerd is expected to accept
//	                       OCI-layout tars natively — what kind
//	                       (LoadImageArchive) and k3d
//	                       (ImageImportIntoClusterMulti) hand it — but that
//	                       is NOT YET PROVEN LIVE (Task 0 review finding):
//	                       plausible-but-unverified until Task 13's bundle
//	                       e2e exercises it. FALLBACK if either importer
//	                       rejects the OCI-layout tar: convert to
//	                       docker-archive at load time inside internal/bundle
//	                       (oras-go content walk + archive/tar — NOT by
//	                       promoting go-containerregistry out of test-only;
//	                       if that proves impractical, it is a plan change).
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/lock"
)

// Manifest is manifest.json's schema, versioned via FormatVersion.
type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	Platform      string            `json:"platform"` // GOOS/GOARCH the images were pulled for
	CreatedAt     string            `json:"createdAt"`
	LockDigest    string            `json:"lockDigest"` // sha256 of the embedded cube.lock bytes
	Images        map[string]string `json:"images"`     // ref -> tar path inside the bundle
}

// currentFormatVersion is the only Manifest.FormatVersion Open accepts.
const currentFormatVersion = 1

// Opened is a bundle extracted to a temporary directory, with its manifest
// and embedded cube.lock already parsed. Callers MUST call Close once done
// to remove the extraction directory.
type Opened struct {
	Dir      string // extraction root
	Manifest Manifest
	Lock     *lock.File // parsed embedded cube.lock
}

// Open extracts bundlePath to a fresh temp directory and parses its
// manifest.json and embedded cube.lock. Any failure to read, extract, or
// parse the bundle — not a tarball, truncated tarball, missing/invalid
// manifest.json, unsupported formatVersion, missing/corrupt cube.lock — is
// CUBE-7003: the bundle cannot be trusted as-is. On error the extraction
// directory (if one was created) is removed; callers never need to clean up
// after a failed Open.
func Open(bundlePath string) (*Opened, error) {
	dir, err := os.MkdirTemp("", "cube-idp-bundle-*")
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeVendorBundleCorrupt,
			"cannot create bundle extraction directory", "check TMPDIR permissions and disk space")
	}

	if err := extractTarGz(bundlePath, dir); err != nil {
		os.RemoveAll(dir)
		return nil, diag.Wrap(err, diag.CodeVendorBundleCorrupt,
			fmt.Sprintf("%s is unreadable or corrupt", bundlePath), "re-run `cube-idp vendor`")
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, diag.Wrap(err, diag.CodeVendorBundleCorrupt,
			fmt.Sprintf("%s has no manifest.json", bundlePath), "re-run `cube-idp vendor`")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		os.RemoveAll(dir)
		return nil, diag.Wrap(err, diag.CodeVendorBundleCorrupt,
			"bundle manifest.json is not valid JSON", "re-run `cube-idp vendor`")
	}
	if m.FormatVersion != currentFormatVersion {
		os.RemoveAll(dir)
		return nil, diag.New(diag.CodeVendorBundleCorrupt,
			fmt.Sprintf("bundle manifest formatVersion %d is not supported (want %d)", m.FormatVersion, currentFormatVersion),
			"re-run `cube-idp vendor` with a matching cube-idp version")
	}

	lf, err := lock.Read(filepath.Join(dir, "cube.lock"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, diag.Wrap(err, diag.CodeVendorBundleCorrupt,
			"bundle cube.lock is unreadable or corrupt", "re-run `cube-idp vendor`")
	}
	if lf == nil {
		os.RemoveAll(dir)
		return nil, diag.New(diag.CodeVendorBundleCorrupt,
			fmt.Sprintf("%s has no embedded cube.lock", bundlePath), "re-run `cube-idp vendor`")
	}

	return &Opened{Dir: dir, Manifest: m, Lock: lf}, nil
}

// PackDir returns the extraction-root path of the named pack's source
// directory (packs/<name>), verifying pack.cue is present there. CUBE-7004
// if the pack is absent from the bundle.
func (o *Opened) PackDir(name string) (string, error) {
	dir := filepath.Join(o.Dir, "packs", name)
	if info, err := os.Stat(filepath.Join(dir, "pack.cue")); err != nil || info.IsDir() {
		return "", diag.New(diag.CodeVendorIncomplete,
			fmt.Sprintf("bundle has no pack %q (packs/%s/pack.cue missing)", name, name),
			"re-run `cube-idp vendor`")
	}
	return dir, nil
}

// PackDirLookup returns a resolver from pack name to the pack's source
// directory within the extraction root, reporting presence via the bool. It
// is the offline seam up.resolveBundleRefs rewrites cube pack refs through:
// a name the bundle carries resolves to its packs/<name> dir; anything else
// returns (_, false) so the caller fails loudly instead of hitting the
// network. Presence is decided exactly as PackDir does — packs/<name>/pack.cue
// must exist and be a regular file.
func (o *Opened) PackDirLookup() func(name string) (string, bool) {
	return func(name string) (string, bool) {
		dir := filepath.Join(o.Dir, "packs", name)
		if info, err := os.Stat(filepath.Join(dir, "pack.cue")); err != nil || info.IsDir() {
			return "", false
		}
		return dir, true
	}
}

// ImageTars returns every locked image's absolute tar path within the
// extraction root, keyed by the original image reference (Manifest.Images).
func (o *Opened) ImageTars() map[string]string {
	out := make(map[string]string, len(o.Manifest.Images))
	for ref, rel := range o.Manifest.Images {
		out[ref] = filepath.Join(o.Dir, filepath.FromSlash(rel))
	}
	return out
}

// Verify checks exactly three things: the embedded cube.lock's content
// matches Manifest.LockDigest (a real content-hash check, so tampering with
// or truncating cube.lock itself is caught); every lock-pinned pack has a
// non-empty packs/<name>/pack.cue; and every Manifest.Images entry has a
// non-empty tar on disk. The pack and image checks are presence-and-size
// only — they catch a missing or zero-length file but NOT a swapped-in file
// of the same size with different content. Content-hash verification of
// packs and images (matching them against per-entry digests recorded at
// vendor time) is a known gap, tracked for Phase 4. Any gap Verify does
// check — missing OR present-but-corrupt (e.g. truncated to zero bytes) — is
// CUBE-7004, naming the offending pack or image.
func (o *Opened) Verify() error {
	raw, err := os.ReadFile(filepath.Join(o.Dir, "cube.lock"))
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorIncomplete,
			"bundle is missing its embedded cube.lock", "re-run `cube-idp vendor`")
	}
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != o.Manifest.LockDigest {
		return diag.New(diag.CodeVendorIncomplete,
			"bundle's embedded cube.lock does not match its manifest lockDigest (bundle is corrupt or was tampered with)",
			"re-run `cube-idp vendor`")
	}

	for _, entry := range o.Lock.Packs {
		p := filepath.Join(o.Dir, "packs", entry.Name, "pack.cue")
		info, statErr := os.Stat(p)
		if statErr != nil || info.Size() == 0 {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle is missing or has a corrupt pack %q (packs/%s/pack.cue)", entry.Name, entry.Name),
				"re-run `cube-idp vendor`")
		}
	}

	for ref, rel := range o.Manifest.Images {
		abs := filepath.Join(o.Dir, filepath.FromSlash(rel))
		info, statErr := os.Stat(abs)
		if statErr != nil || info.Size() == 0 {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle is missing or has a corrupt tar for image %q (%s)", ref, rel),
				"re-run `cube-idp vendor`")
		}
	}
	return nil
}

// Close removes the extraction directory. Safe to call once after any
// successful Open; a zero-value Opened's Close is a no-op.
func (o *Opened) Close() {
	if o.Dir != "" {
		os.RemoveAll(o.Dir)
	}
}

// writeJSON marshals v as indented JSON and writes it to path.
func writeJSON(path string, v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// copyTree recursively copies the directory tree at src into dst (created
// if absent). Regular files and directories only — symlinks and other
// irregular entries are skipped, matching internal/oci's buildDirLayer
// (pack source trees are data-only directories).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case rel == ".":
			return os.MkdirAll(dst, 0o755)
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, in)
			return err
		default:
			return nil // symlinks etc.: not part of the pack contract
		}
	})
}

// tarDir archives the directory tree at srcDir into a PLAIN (uncompressed)
// tar file at destPath. Used for each per-image OCI layout, which nests
// inside the outer gzip-compressed bundle tarball (tarGzDir) — a second
// layer of compression here would be wasted work.
func tarDir(srcDir, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	if err := writeTarTree(tw, srcDir); err != nil {
		return err
	}
	return tw.Close()
}

// tarGzDir archives the directory tree at srcDir as a gzip-compressed tar,
// written atomically: staged at destPath+".tmp" in destPath's own directory
// (so the final rename is same-filesystem and atomic) and renamed into
// place only once fully written and closed.
func tarGzDir(srcDir, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := writeTarTree(tw, srcDir); err != nil {
		tw.Close()
		gw.Close()
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := tw.Close(); err != nil {
		gw.Close()
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := gw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

// writeTarTree walks srcDir and writes every directory and regular file
// into tw with slash-separated, srcDir-relative names, in
// filepath.WalkDir's deterministic lexical order.
func writeTarTree(tw *tar.Writer, srcDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root itself needs no tar entry
		}
		name := filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: info.Size()}); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		default:
			return nil // symlinks etc.
		}
	})
}

// extractTarGz extracts the gzip-compressed tar at srcPath into destDir,
// rejecting any entry whose path would escape destDir (zip-slip guard: no
// ".." segment, no absolute path).
func extractTarGz(srcPath, destDir string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

// safeJoin joins destDir and rel, rejecting any path that would escape
// destDir — a ".." segment anywhere in rel, an absolute rel, or (as a
// second line of defense) a joined-and-cleaned result that still doesn't
// stay under destDir.
func safeJoin(destDir, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("bundle contains an unsafe path %q", rel)
	}
	target := filepath.Join(destDir, rel)
	if target != destDir && !strings.HasPrefix(target, destDir+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle contains an unsafe path %q", rel)
	}
	return target, nil
}
