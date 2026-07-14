package syncer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// diagFakeErr stands in for a real sync failure (e.g. a YAML typo) so
// TestWatchSyncErrorRendersLoudly can assert on diag.Render's exact
// wrapping without depending on any real failure path.
func diagFakeErr() error {
	return diag.New(diag.CodeSyncNoManifests, "boom", "fix the file and save again")
}

// syncBuf is a mutex-guarded strings.Builder: Watch's loop writes to
// deps.Out from its own goroutine while the test's main goroutine polls the
// buffer via waitFor, so a bare strings.Builder would race under -race.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestWatchDebouncesAndResyncs is the debounce-coalescing proof: an initial
// sync fires on start, then a burst of rapid writes (an editor's atomic-save
// dance) must coalesce into exactly ONE extra sync, not five.
func TestWatchDebouncesAndResyncs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)

	var syncs atomic.Int32
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error { syncs.Add(1); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 50*time.Millisecond) }()

	waitFor(t, func() bool { return syncs.Load() == 1 }, "initial sync") // sync #1 on start
	// A burst of writes must coalesce into ONE debounced sync.
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 2\n"), 0o644)
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, func() bool { return syncs.Load() == 2 }, "debounced sync")
	time.Sleep(150 * time.Millisecond)
	if got := syncs.Load(); got != 2 {
		t.Fatalf("burst produced %d syncs, want 2 total", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch must return nil on cancellation: %v", err)
	}
}

// TestWatchSurvivesSyncErrors is the loud-non-fatal-failure proof: a sync
// error must not kill the loop — the developer is mid-edit and the very
// next save must still trigger a sync.
func TestWatchSurvivesSyncErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)
	calls := atomic.Int32{}
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error {
		if calls.Add(1) == 2 {
			return context.DeadlineExceeded // any error: loop must keep going
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 30*time.Millisecond) }()
	waitFor(t, func() bool { return calls.Load() == 1 }, "initial")
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 2\n"), 0o644) // -> failing sync #2
	waitFor(t, func() bool { return calls.Load() == 2 }, "failing sync")
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 3\n"), 0o644) // -> sync #3 proves survival
	waitFor(t, func() bool { return calls.Load() == 3 }, "post-error sync")
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch must return nil on cancellation even after a sync error: %v", err)
	}
}

// TestWatchSyncErrorRendersLoudly pins the "loud, non-fatal" contract: a
// failing sync must print the full diag.Render block (not just swallow the
// error) plus a note that the watch is still running.
func TestWatchSyncErrorRendersLoudly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)
	calls := atomic.Int32{}
	out := &syncBuf{}
	deps := Deps{Out: out, syncFn: func(context.Context) error {
		if calls.Add(1) == 1 {
			return diagFakeErr()
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 30*time.Millisecond) }()
	waitFor(t, func() bool { return calls.Load() == 1 }, "initial failing sync")
	waitFor(t, func() bool { return strings.Contains(out.String(), "boom") }, "rendered error")
	if !strings.Contains(out.String(), "still watching") {
		t.Fatalf("failing sync must note the watch continues, got %q", out.String())
	}
	cancel()
	<-done
}

// TestWatchNewSubdirJoinsWatch proves a subdirectory created after Watch
// starts is itself watched — a file dropped straight into it still
// triggers a debounced sync, not just the top-level dir.
func TestWatchNewSubdirJoinsWatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)

	var syncs atomic.Int32
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error { syncs.Add(1); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 30*time.Millisecond) }()
	waitFor(t, func() bool { return syncs.Load() == 1 }, "initial sync")

	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return syncs.Load() == 2 }, "sync on subdir creation")
	// Give fsnotify a moment to register the watch on sub before writing
	// into it — this is exactly the race Watch's Create handling exists
	// to close.
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(filepath.Join(sub, "nested.yaml"), []byte("b: 1\n"), 0o644)
	waitFor(t, func() bool { return syncs.Load() == 3 }, "sync on file inside new subdir")

	cancel()
	<-done
}

// TestWatchIgnoresEditorDroppings proves the ignore list (dotfiles, `~`,
// `.swp`, `.#*`, `4913`) never triggers a sync on its own.
func TestWatchIgnoresEditorDroppings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)

	var syncs atomic.Int32
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error { syncs.Add(1); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 30*time.Millisecond) }()
	waitFor(t, func() bool { return syncs.Load() == 1 }, "initial sync")

	for _, name := range []string{".cm.yaml.swp", "cm.yaml~", ".#cm.yaml", "4913"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	time.Sleep(150 * time.Millisecond)
	if got := syncs.Load(); got != 1 {
		t.Fatalf("editor droppings must not trigger a sync, got %d syncs", got)
	}

	// A real change still fires, proving the watcher itself is alive.
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 2\n"), 0o644)
	waitFor(t, func() bool { return syncs.Load() == 2 }, "real change after ignored droppings")

	cancel()
	<-done
}

// TestDefaultSyncFnNotesUnchangedDigest exercises Watch's real (non-fake)
// default syncFn — the one built from SyncOnce when Deps.syncFn is nil —
// against the envtest Applier + in-process OCI registry harness from
// synconce_test.go. Two consecutive syncs of an unchanged directory must
// push the same digest; the second call prints the quiet
// "no manifest changes" note.
func TestDefaultSyncFnNotesUnchangedDigest(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	a, err := apply.New(testREST, "watchdigestcube")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: watchdigest\n  namespace: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := newFakeOCIRegistry(t)
	addr := strings.TrimPrefix(registry.URL, "http://")

	var out strings.Builder
	deps := Deps{Applier: a, Engine: &fakeEngine{}, Out: &out, PushAddr: addr}

	syncFn := newDefaultSyncFn(deps, dir)
	if err := syncFn(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if strings.Contains(out.String(), "no manifest changes") {
		t.Fatalf("first sync must not print the unchanged note, got %q", out.String())
	}

	if err := syncFn(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !strings.Contains(out.String(), "no manifest changes — skipped push") {
		t.Fatalf("second sync of unchanged content must note the skip, got %q", out.String())
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second) // every wait has a deadline, tests included
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
