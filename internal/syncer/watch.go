// Task 11 adds D7's watch half around SyncOnce (see syncer.go's package
// doc): `sync --watch` re-syncs a directory on every debounced filesystem
// change instead of once. Watch semantics (all decided, none deferred):
// recursive watch of dir; 300ms debounce coalesces an editor's write burst
// into one sync; dotfiles/dirs and editor droppings (*~, *.swp, .#*, 4913)
// are ignored; new subdirectories join the watch on creation; a sync
// failure mid-watch is rendered loudly (the full diag.Render block) and the
// watch CONTINUES — this is documented behavior, not a silent fallback: the
// developer is mid-edit, and killing the loop on a YAML typo would defeat
// the feature. Ctrl-C (main.go's signal.NotifyContext, flowing through
// Execute(ctx) into every RunE) cancels ctx, and Watch returns nil.
package syncer

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/rafpe/cube-idp/internal/diag"
)

// Watch runs SyncOnce, then blocks: re-syncs on every debounced change
// under dir until ctx is cancelled. Returns nil on cancellation, an error
// only for unrecoverable setup failures (CUBE-7202).
func Watch(ctx context.Context, deps Deps, dir string, debounce time.Duration) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return diag.Wrap(err, diag.CodeSyncWatchSetupFail, "cannot start the filesystem watcher",
			"on Linux, raise fs.inotify.max_user_watches (sysctl); then retry")
	}
	defer w.Close()

	addRecursive := func(root string) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() || (strings.HasPrefix(d.Name(), ".") && p != root) {
				if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != root {
					return filepath.SkipDir
				}
				return err
			}
			return w.Add(p)
		})
	}
	if err := addRecursive(dir); err != nil {
		return diag.Wrap(err, diag.CodeSyncWatchSetupFail, "cannot watch "+dir,
			"check the directory exists and is readable")
	}

	syncOnce := deps.syncFn
	if syncOnce == nil {
		syncOnce = newDefaultSyncFn(deps, dir)
	}
	runSync := func() {
		if err := syncOnce(ctx); err != nil {
			fmt.Fprintln(deps.Out, diag.Render(err)) // loud, non-fatal: developer is mid-edit
			fmt.Fprintln(deps.Out, "  (still watching — fix the file and save again)")
		}
	}

	runSync() // initial sync on start

	var timer *time.Timer
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
		}
	}
	fire := make(chan struct{}, 1)
	for {
		select {
		case <-ctx.Done():
			stopTimer()
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				stopTimer()
				return nil
			}
			if ignored(ev.Name) {
				continue
			}
			if ev.Op.Has(fsnotify.Create) {
				_ = addRecursive(ev.Name) // new subdirs join the watch; non-dirs are a harmless no-op
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		case <-fire:
			runSync()
		case err, ok := <-w.Errors:
			if !ok {
				stopTimer()
				return nil
			}
			fmt.Fprintln(deps.Out, diag.Render(diag.Wrap(err, diag.CodeSyncWatchSetupFail, "filesystem watcher error",
				"if this repeats, restart `cube-idp sync --watch`")))
		}
	}
}

// newDefaultSyncFn builds Watch's production syncFn: call the real
// SyncOnce, then compare the pushed artifact's digest (Task 10's
// ArtifactRef.Digest, threaded through Result) to the last digest this
// watch loop observed. An unchanged digest means the directory's rendered
// content didn't actually change — the OCI push was a no-op at the
// content-addressed layer — so a quiet note replaces silence, without
// treating the repeat as an error.
func newDefaultSyncFn(deps Deps, dir string) func(context.Context) error {
	var lastDigest string
	return func(c context.Context) error {
		res, err := SyncOnce(c, deps, dir)
		if err != nil {
			return err
		}
		if res.Digest != "" && res.Digest == lastDigest {
			fmt.Fprintln(deps.Out, "▸ [sync] no manifest changes — skipped push")
		}
		lastDigest = res.Digest
		return nil
	}
}

// ignored reports whether path is an editor dropping or dotfile/dir that
// must never trigger a sync on its own: dotfiles/dirs, `*~` (vim/emacs
// backups), `*.swp` (vim swap files), `.#*` (emacs lock files), and `4913`
// (vim's writability probe file).
func ignored(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") ||
		strings.HasSuffix(base, ".swp") || strings.HasPrefix(base, ".#") || base == "4913"
}
