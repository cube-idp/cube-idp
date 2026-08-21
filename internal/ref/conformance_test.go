package ref

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// backendCase describes one backend for the shared behavioral suite below.
// Every backend in the scheme table runs it, so pins, containment, mode
// restriction and cancellation are checked the same way for all of them
// instead of each backend inventing its own idea of correct.
//
// This is a test helper on purpose and not an exported Resolver seam: ref
// has one implementation and one consumer today, and the interface
// doctrine says a seam arrives with a real second one
// (docs/domains/pack.md, "Interface doctrine applied").
type backendCase struct {
	name string

	// resolveTree and resolveFile are the entry points under test. They
	// are fields rather than hard-coded calls to ResolveTree/ResolveFile
	// so the https case can pass the client httptest.Server publishes for
	// its own certificate; every other backend passes the exported
	// functions unchanged.
	resolveTree func(ctx context.Context, ref string) (ResolvedTree, error)
	resolveFile func(ctx context.Context, ref string) (ResolvedFile, error)

	// treeRef is a reference this backend resolves as a tree, nil when it
	// has no tree form. A deferred backend puts its reference here.
	treeRef func(t *testing.T) string
	// fileRef is a reference this backend resolves as a file, nil when it
	// has no file form, and fileWant is the content behind it.
	fileRef  func(t *testing.T) string
	fileWant []byte
	// escapingRef is a reference whose content leaves its root, nil when
	// this backend cannot express one.
	escapingRef func(t *testing.T) string
	// deferredCode is the not-implemented code both modes answer with;
	// empty for a backend this build carries.
	deferredCode cubeerr.Code
}

// runBackendConformance is the shared behavioral suite.
func runBackendConformance(t *testing.T, c backendCase) {
	t.Helper()

	if c.deferredCode != "" {
		checkDeferred(t, c)
		return
	}
	if c.treeRef != nil {
		checkTree(t, c)
	}
	if c.fileRef != nil {
		checkFile(t, c)
	}
	if c.escapingRef != nil {
		t.Run("containment", func(t *testing.T) {
			r := c.escapingRef(t)
			_, err := c.resolveTree(t.Context(), r)
			requireCode(t, err, CodeEscapesRoot)
		})
	}
}

// checkTree covers the tree mode: a recorded, stable pin; the file mode
// refused; cancellation honored.
func checkTree(t *testing.T, c backendCase) {
	t.Helper()

	t.Run("tree records a pin", func(t *testing.T) {
		r := c.treeRef(t)
		got, err := c.resolveTree(t.Context(), r)
		if err != nil {
			t.Fatalf("ResolveTree(%q) error = %v, want nil", r, err)
		}
		if got.FS() == nil {
			t.Fatalf("ResolveTree(%q).FS() = nil, want a filesystem", r)
		}
		checkPin(t, "ResolveTree", r, got.Pin())

		again, err := c.resolveTree(t.Context(), r)
		if err != nil {
			t.Fatalf("ResolveTree(%q) second call error = %v, want nil", r, err)
		}
		if err := again.Pin().Verify(got.Pin()); err != nil {
			t.Errorf("ResolveTree(%q) pin is not stable across calls: %v", r, err)
		}
	})

	t.Run("tree refuses file mode", func(t *testing.T) {
		r := c.treeRef(t)
		_, err := c.resolveFile(t.Context(), r)
		requireCode(t, err, CodeModeMismatch)
	})

	t.Run("tree honors cancellation", func(t *testing.T) {
		r := c.treeRef(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := c.resolveTree(ctx, r)
		requireCode(t, err, CodeFetchFailed)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ResolveTree(%q) on a cancelled context: error = %v, want it to wrap context.Canceled", r, err)
		}
	})
}

// checkFile covers the file mode, including that the bytes handed back are
// a copy the caller cannot use to edit the pinned content.
func checkFile(t *testing.T, c backendCase) {
	t.Helper()

	t.Run("file records a pin", func(t *testing.T) {
		r := c.fileRef(t)
		got, err := c.resolveFile(t.Context(), r)
		if err != nil {
			t.Fatalf("ResolveFile(%q) error = %v, want nil", r, err)
		}
		if !bytes.Equal(got.Bytes(), c.fileWant) {
			t.Errorf("ResolveFile(%q).Bytes() = %q, want %q", r, got.Bytes(), c.fileWant)
		}
		checkPin(t, "ResolveFile", r, got.Pin())
	})

	t.Run("file bytes are copied", func(t *testing.T) {
		r := c.fileRef(t)
		got, err := c.resolveFile(t.Context(), r)
		if err != nil {
			t.Fatalf("ResolveFile(%q) error = %v, want nil", r, err)
		}
		if edited := got.Bytes(); len(edited) > 0 {
			edited[0] ^= 0xff
		}
		if !bytes.Equal(got.Bytes(), c.fileWant) {
			t.Errorf("ResolveFile(%q).Bytes() = %q after a caller edited the returned slice, want %q",
				r, got.Bytes(), c.fileWant)
		}
	})

	t.Run("file refuses tree mode", func(t *testing.T) {
		r := c.fileRef(t)
		_, err := c.resolveTree(t.Context(), r)
		requireCode(t, err, CodeModeMismatch)
	})

	t.Run("file honors cancellation", func(t *testing.T) {
		r := c.fileRef(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := c.resolveFile(ctx, r)
		requireCode(t, err, CodeFetchFailed)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ResolveFile(%q) on a cancelled context: error = %v, want it to wrap context.Canceled", r, err)
		}
	})
}

// checkDeferred covers a backend the parser recognizes but this build does
// not carry: both modes must name it with its own code.
func checkDeferred(t *testing.T, c backendCase) {
	t.Helper()

	r := c.treeRef(t)
	t.Run("tree is deferred", func(t *testing.T) {
		_, err := c.resolveTree(t.Context(), r)
		requireCode(t, err, c.deferredCode)
	})
	t.Run("file is deferred", func(t *testing.T) {
		_, err := c.resolveFile(t.Context(), r)
		requireCode(t, err, c.deferredCode)
	})
}

// checkPin asserts the shape every backend's pin must have.
func checkPin(t *testing.T, op, r string, pin Pin) {
	t.Helper()

	if pin.Ref != r {
		t.Errorf("%s(%q).Pin().Ref = %q, want %q", op, r, pin.Ref, r)
	}
	if pin.Source == "" {
		t.Errorf("%s(%q).Pin().Source is empty, want the location actually read", op, r)
	}
	if !strings.HasPrefix(pin.Digest, "sha256:") {
		t.Errorf("%s(%q).Pin().Digest = %q, want a sha256: digest", op, r, pin.Digest)
	}
}

// requireCode asserts error identity the way this repo always does: an
// errors.As into *cubeerr.Coded plus code equality, never a string match.
func requireCode(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("got nil error, want %s", want)
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("got %v (%T), want a *cubeerr.Coded carrying %s", err, err, want)
	}
	if coded.Code != want {
		t.Fatalf("got code %s (%v), want %s", coded.Code, err, want)
	}
}
