package runloop

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ytnobody/hermit/internal/state"
)

func testLogger() *log.Logger {
	return log.New(discardWriter{}, "", 0)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRun_RequiresInvoke(t *testing.T) {
	err := Run(context.Background(), Options{RootDir: t.TempDir(), Logger: testLogger()})
	if err == nil {
		t.Fatal("Run with nil Invoke: want error, got nil")
	}
}

// TestREQ019_NoOverlappingPasses verifies that Run never starts a new pass
// before the previous one's Invoke call has returned, even when a pass
// takes a while — Issue #181's "パスが長時間かかっても重複起動しない".
func TestREQ019_NoOverlappingPasses(t *testing.T) {
	dir := t.TempDir()

	var (
		mu          sync.Mutex
		concurrent  int
		maxSeen     int
		invokeCalls int32
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	invoke := func(ctx context.Context, d string) error {
		mu.Lock()
		concurrent++
		if concurrent > maxSeen {
			maxSeen = concurrent
		}
		mu.Unlock()

		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		concurrent--
		mu.Unlock()

		if atomic.AddInt32(&invokeCalls, 1) >= 3 {
			cancel()
		}
		return nil
	}

	opts := Options{
		RootDir:  dir,
		Invoke:   invoke,
		Interval: time.Millisecond,
		Logger:   testLogger(),
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxSeen > 1 {
		t.Errorf("observed %d concurrent passes, want at most 1", maxSeen)
	}
	if invokeCalls < 3 {
		t.Errorf("invokeCalls = %d, want at least 3", invokeCalls)
	}
}

// TestREQ019_GracefulShutdownDoesNotInterruptInFlightPass verifies that
// canceling the shutdown context while a pass is running lets that pass run
// to completion (its ctx is never canceled), and Run stops only after it
// finishes — Issue #181's "SIGINT/SIGTERM で実行中のパスを中断せずグレースフルに
// 停止する".
func TestREQ019_GracefulShutdownDoesNotInterruptInFlightPass(t *testing.T) {
	dir := t.TempDir()
	shutdownCtx, cancel := context.WithCancel(context.Background())

	var completed atomic.Bool
	started := make(chan struct{})

	invoke := func(passCtx context.Context, d string) error {
		close(started)
		// Cancel the shutdown signal partway through this pass.
		cancel()
		select {
		case <-passCtx.Done():
			t.Error("pass context was canceled; an in-flight pass must not be interrupted by shutdown")
		case <-time.After(20 * time.Millisecond):
		}
		completed.Store(true)
		return nil
	}

	opts := Options{
		RootDir:  dir,
		Invoke:   invoke,
		Interval: time.Millisecond,
		Logger:   testLogger(),
	}

	done := make(chan error, 1)
	go func() { done <- Run(shutdownCtx, opts) }()

	<-started
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after shutdown + one in-flight pass")
	}
	if !completed.Load() {
		t.Error("pass did not complete before Run returned")
	}
}

// TestREQ019_QuitFileStopsLoop verifies .hermit-quit is detected by the
// run loop itself (not only by the invoked Claude session).
func TestREQ019_QuitFileStopsLoop(t *testing.T) {
	dir := t.TempDir()
	var calls int32

	invoke := func(ctx context.Context, d string) error {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			if err := os.WriteFile(filepath.Join(dir, ".hermit-quit"), nil, 0o644); err != nil {
				t.Fatalf("write quit file: %v", err)
			}
		}
		return nil
	}

	opts := Options{
		RootDir:  dir,
		Invoke:   invoke,
		Interval: time.Millisecond,
		Logger:   testLogger(),
	}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), opts) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after .hermit-quit was created")
	}
	if calls != 1 {
		t.Errorf("invoke called %d times, want exactly 1 (loop should stop before a 2nd pass once quit is detected)", calls)
	}
}

// TestREQ019_PauseFileSkipsPassesWithoutStoppingLoop verifies .hermit-paused
// is detected by the run loop itself, and that it skips Invoke rather than
// stopping the loop entirely.
func TestREQ019_PauseFileSkipsPassesWithoutStoppingLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hermit-paused"), nil, 0o644); err != nil {
		t.Fatalf("write pause file: %v", err)
	}

	var calls int32
	invoke := func(ctx context.Context, d string) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	opts := Options{
		RootDir:  dir,
		Invoke:   invoke,
		Interval: 2 * time.Millisecond,
		Logger:   testLogger(),
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 0 {
		t.Errorf("invoke called %d times while paused, want 0", calls)
	}
}

// TestREQ019_RecordsSuccessAndResetsFailures verifies a successful pass
// updates last_success_tick and resets consecutive_failures in
// .hermit/superintendent-state.json — the Go-owned state file.
func TestREQ019_RecordsSuccessAndResetsFailures(t *testing.T) {
	dir := t.TempDir()
	fixedNow := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	// Seed a prior failure count to verify it gets reset on success.
	if err := state.Save(state.Path(dir), state.LoopState{ConsecutiveFailures: 2}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	invoke := func(ctx context.Context, d string) error {
		if atomic.AddInt32(&calls, 1) >= 1 {
			cancel()
		}
		return nil
	}

	opts := Options{
		RootDir:  dir,
		Invoke:   invoke,
		Interval: time.Millisecond,
		Now:      func() time.Time { return fixedNow },
		Logger:   testLogger(),
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	st, err := state.Load(state.Path(dir))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 after success", st.ConsecutiveFailures)
	}
	if st.LastSuccessTick == nil || !st.LastSuccessTick.Equal(fixedNow) {
		t.Errorf("LastSuccessTick = %v, want %v", st.LastSuccessTick, fixedNow)
	}
}

// TestREQ019_NotifiesAfterConsecutiveFailureThreshold verifies the
// liveness/notification requirement: after N consecutive failed passes, a
// notification fires via the configured webhook (N configurable).
func TestREQ019_NotifiesAfterConsecutiveFailureThreshold(t *testing.T) {
	dir := t.TempDir()

	var notifyCalls int32
	var lastMsg string
	notify := func(webhookURL, webhookType, event, message string) error {
		atomic.AddInt32(&notifyCalls, 1)
		lastMsg = message
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	invoke := func(ctx context.Context, d string) error {
		n := atomic.AddInt32(&calls, 1)
		if n >= 3 {
			cancel()
		}
		return errors.New("boom")
	}

	opts := Options{
		RootDir:                dir,
		Invoke:                 invoke,
		Interval:               time.Millisecond,
		FailureNotifyThreshold: 2,
		Notify:                 notify,
		Logger:                 testLogger(),
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if notifyCalls != 1 {
		t.Fatalf("notify called %d times, want exactly 1 (fires once when the threshold is first reached)", notifyCalls)
	}
	if lastMsg == "" {
		t.Error("notification message was empty")
	}

	st, err := state.Load(state.Path(dir))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.ConsecutiveFailures < 2 {
		t.Errorf("ConsecutiveFailures = %d, want >= 2", st.ConsecutiveFailures)
	}
}

// TestREQ019_ZeroThresholdDisablesNotification verifies
// FailureNotifyThreshold <= 0 never calls Notify, matching the codebase's
// existing "<=0 means use/disable default" convention (see loadConfig's
// LoopInterval/MaxEngineers handling in cmd/hermit).
func TestREQ019_ZeroThresholdDisablesNotification(t *testing.T) {
	dir := t.TempDir()
	var notifyCalls int32
	notify := func(webhookURL, webhookType, event, message string) error {
		atomic.AddInt32(&notifyCalls, 1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	invoke := func(ctx context.Context, d string) error {
		n := atomic.AddInt32(&calls, 1)
		if n >= 5 {
			cancel()
		}
		return errors.New("boom")
	}

	opts := Options{
		RootDir:                dir,
		Invoke:                 invoke,
		Interval:               time.Millisecond,
		FailureNotifyThreshold: 0,
		Notify:                 notify,
		Logger:                 testLogger(),
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if notifyCalls != 0 {
		t.Errorf("notify called %d times with threshold disabled, want 0", notifyCalls)
	}
}
