// Package ref resolves a reference string to a tree of files or to a
// single file, and nothing else.
//
// It is a shared-infrastructure leaf: it imports only internal/cubeerr and
// its per-backend SDKs — never api/ and never a component domain — so the
// domains that need to locate content share one grammar instead of each
// growing their own (docs/ARCHITECTURE.md §2).
//
// The grammar recognizes explicit schemes only; there are no bare-git
// heuristics, because those overlap local paths, host-like directories,
// and future registry syntax:
//
//	./path, ../path, file:///abs/path                       tree or file
//	https://host/path                                       file
//	git+https://host/org/repo.git?ref=<rev>&path=<sub>      tree
//	oci://host/repo:tag                                     tree
//	s3://bucket/key                                         tree or file
//
// The last three are recognized by the parser but not implemented in this
// build: each returns its own code naming the backend and where it lands.
// Every resolution records a Pin, enforces containment (no path traversal,
// no symlink escape), and honors cancellation.
package ref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// Mode is the shape a reference is resolved into.
type Mode string

const (
	// ModeTree resolves a reference to a tree of files.
	ModeTree Mode = "tree"
	// ModeFile resolves a reference to a single file.
	ModeFile Mode = "file"
)

// Pin records what a resolution actually returned, so the same reference
// resolved later can be checked against it. All fields are values: a Pin
// is comparable and safe to copy.
type Pin struct {
	// Ref is the reference as the caller wrote it.
	Ref string
	// Source is the canonical location the content was read from: an
	// absolute filesystem path for local references, the URL otherwise.
	Source string
	// Digest is "sha256:<hex>" over the resolved content. For a tree it
	// covers a deterministic manifest of every path and its content, so it
	// changes when any file is added, removed, renamed, or edited.
	Digest string
}

// Verify reports whether p resolved to the same content as want, returning
// a CUBE-REF-005 error when it did not. Only Digest is compared: the same
// content reached through a different Source is still the same content.
func (p Pin) Verify(want Pin) error {
	if p.Digest == want.Digest {
		return nil
	}
	return newPinMismatchError(p.Ref, want.Digest, p.Digest)
}

// ResolvedTree is a reference resolved to a tree of files. The zero value
// is not usable; obtain one from ResolveTree.
type ResolvedTree struct {
	fsys fs.FS
	pin  Pin
}

// FS returns the tree as a read-only filesystem. It is a snapshot taken at
// resolve time — the Pin beside it describes exactly these bytes — so a
// walk over it is deterministic and cannot change underfoot.
func (t ResolvedTree) FS() fs.FS { return t.fsys }

// Pin returns the record of what this resolution returned.
func (t ResolvedTree) Pin() Pin { return t.pin }

// ResolvedFile is a reference resolved to a single file. The zero value is
// not usable; obtain one from ResolveFile.
type ResolvedFile struct {
	data []byte
	pin  Pin
}

// Bytes returns a copy of the file's content, so a caller cannot edit the
// bytes the Pin was computed over.
func (f ResolvedFile) Bytes() []byte { return slices.Clone(f.data) }

// Pin returns the record of what this resolution returned.
func (f ResolvedFile) Pin() Pin { return f.pin }

// ResolveTree resolves ref to a tree of files. Resolving a reference whose
// scheme yields a single file is a CUBE-REF-006 mode mismatch.
func ResolveTree(ctx context.Context, ref string) (ResolvedTree, error) {
	p, err := parse(ref)
	if err != nil {
		return ResolvedTree{}, err
	}
	return backendFor(p.scheme).tree(ctx, p)
}

// ResolveFile resolves ref to a single file. Resolving a reference that
// yields a tree is a CUBE-REF-006 mode mismatch.
func ResolveFile(ctx context.Context, ref string) (ResolvedFile, error) {
	p, err := parse(ref)
	if err != nil {
		return ResolvedFile{}, err
	}
	return backendFor(p.scheme).file(ctx, p)
}

// scheme names a backend in the table below. schemeLocal is the backend
// that serves both spellings of a local path (./x and file:///x), so it is
// not itself a URL scheme.
type scheme string

const (
	schemeLocal scheme = "local"
	schemeFile  scheme = "file"
	schemeHTTPS scheme = "https"
	schemeGit   scheme = "git+https"
	schemeOCI   scheme = "oci"
	schemeS3    scheme = "s3"
)

// parsed is one reference after the grammar has accepted it.
type parsed struct {
	// ref is the reference as written, kept for error messages.
	ref string
	// scheme selects the backend.
	scheme scheme
	// location is what that backend consumes: a filesystem path for local
	// references, the reference itself for the URL-shaped ones.
	location string
}

// parse applies the grammar. It is the only place a reference string is
// interpreted; every backend receives an already-validated parsed.
func parse(ref string) (parsed, error) {
	if ref == "" {
		return parsed{}, newMalformedRefError(ref, "reference is empty")
	}
	if isLocalPath(ref) {
		return parsed{ref: ref, scheme: schemeLocal, location: ref}, nil
	}
	name, rest, ok := strings.Cut(ref, "://")
	if !ok {
		return parsed{}, newMalformedRefError(ref,
			"no scheme, and a local path must start with ./ or ../")
	}
	if rest == "" {
		return parsed{}, newMalformedRefError(ref, fmt.Sprintf("scheme %q has no location", name))
	}
	switch scheme(name) {
	case schemeFile:
		if !strings.HasPrefix(rest, "/") {
			return parsed{}, newMalformedRefError(ref,
				"a file reference must be file:///abs/path, with an empty host")
		}
		return parsed{ref: ref, scheme: schemeLocal, location: rest}, nil
	case schemeHTTPS, schemeGit, schemeOCI, schemeS3:
		return parsed{ref: ref, scheme: scheme(name), location: ref}, nil
	default:
		return parsed{}, newUnsupportedSchemeError(ref, name)
	}
}

// isLocalPath reports whether ref is the explicit relative-path form. "."
// and ".." are the zero-length spellings of ./path and ../path; a bare
// path like "foo/bar" is not accepted, because guessing there is what
// makes local paths collide with host-shaped and registry references.
func isLocalPath(ref string) bool {
	return ref == "." || ref == ".." ||
		strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../")
}

// backend is one scheme's pair of resolvers. Both are always set: a
// backend serving only one mode answers the other with a CUBE-REF-006 mode
// mismatch, and a deferred backend answers both with its own
// not-implemented code.
type backend struct {
	tree func(ctx context.Context, p parsed) (ResolvedTree, error)
	file func(ctx context.Context, p parsed) (ResolvedFile, error)
}

// backendFor is the single scheme table shared by both entry points. It is
// a function rather than a package-level map because mutable
// package-level state is banned outside main (CLAUDE.md §2).
func backendFor(s scheme) backend {
	switch s {
	case schemeLocal:
		return backend{tree: localTree, file: localFile}
	case schemeHTTPS:
		return backend{tree: httpsTree, file: httpsFile}
	case schemeGit:
		return backend{tree: gitTree, file: gitFile}
	case schemeOCI:
		return backend{tree: ociTree, file: ociFile}
	case schemeS3:
		return backend{tree: s3Tree, file: s3File}
	default:
		// Unreachable: parse admits no other scheme. Answering with the
		// grammar error keeps a future table edit from panicking.
		return backend{
			tree: func(_ context.Context, p parsed) (ResolvedTree, error) {
				return ResolvedTree{}, newUnsupportedSchemeError(p.ref, string(s))
			},
			file: func(_ context.Context, p parsed) (ResolvedFile, error) {
				return ResolvedFile{}, newUnsupportedSchemeError(p.ref, string(s))
			},
		}
	}
}

// fileDigest is the pin digest of a single file's bytes.
func fileDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// treeDigest is the pin digest of a whole tree: sha256 over a manifest of
// every path and its content hash, walked in sorted order so the digest
// depends on the content and never on map or directory iteration order.
func treeDigest(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	h := sha256.New()
	for _, p := range paths {
		sum := sha256.Sum256(files[p])
		// hash.Hash never returns a write error.
		_, _ = fmt.Fprintf(h, "%s %s\n", hex.EncodeToString(sum[:]), p)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
