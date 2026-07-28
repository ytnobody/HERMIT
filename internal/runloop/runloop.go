// Package runloop implements the ticker that drives `hermit run` (Issue
// #181): a long-lived process, external to any Claude Code session, that
// invokes one Superintendent pass at a time and waits [agent].loop_interval
// after each pass completes before starting the next one.
//
// This intentionally replaces the Claude-Code-session-hosted `/loop`
// mechanism (previously driven by CLAUDE.md's Superintendent cycle) as the
// thing that keeps ticking. It does NOT reintroduce a background-subagent
// loop inside a Claude Code session (issue #147's design, which exhausted
// the session's subagent-spawn cap per issue #171) — there is no subagent
// spawn here at all; each tick is a plain child-process invocation
// (`claude -p ...`) of a Go long-lived process, the same shape `hermit
// serve` already uses for the MCP server.
package runloop

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ytnobody/hermit/internal/state"
)

// Invoker runs one Superintendent pass to completion, using dir as the
// working directory, and returns any error. ctx passed to Invoke is always
// context.Background()-derived from Run's perspective, never the shutdown
// context — see Run's doc comment for why a pass is never interrupted.
type Invoker func(ctx context.Context, dir string) error

// NotifyFunc sends a notification (e.g. internal/notification.Send).
type NotifyFunc func(webhookURL, webhookType, event, message string) error

// Options configures Run.
type Options struct {
	// RootDir is the project root: both the working directory passed to
	// Invoke and the directory containing .hermit/superintendent-state.json.
	RootDir string

	// Invoke runs one Superintendent pass. Required.
	Invoke Invoker

	// Interval is how long Run waits after a pass finishes before starting
	// the next one (harness.toml's [agent].loop_interval).
	Interval time.Duration

	// FailureNotifyThreshold is the number of consecutive failed passes
	// after which Run sends one notification via Notify. <= 0 disables
	// failure notification.
	FailureNotifyThreshold int

	// WebhookURL / WebhookType are passed through to Notify.
	WebhookURL  string
	WebhookType string

	// Notify sends the failure notification. Required only when
	// FailureNotifyThreshold > 0; if nil in that case, Run logs a warning
	// and skips notifying rather than panicking.
	Notify NotifyFunc

	// Now returns the current time; defaults to time.Now when nil.
	Now func() time.Time

	// Logger receives progress/diagnostic output; defaults to log.Default()
	// when nil.
	Logger *log.Logger
}

// sleeper abstracts waiting for Interval so tests don't need real time.
// It returns true if the wait was cut short by shutdownCtx being canceled.
type sleeper func(shutdownCtx context.Context, d time.Duration) (canceled bool)

func defaultSleeper(shutdownCtx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-shutdownCtx.Done():
			return true
		default:
			return false
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-shutdownCtx.Done():
		return true
	case <-t.C:
		return false
	}
}

// Run drives the tick loop until shutdownCtx is canceled (SIGINT/SIGTERM in
// `hermit run`'s case) or the state file's Status is state.StatusQuit.
//
// Overlap safety: each iteration blocks on opts.Invoke before doing
// anything else in the loop body, so two passes running at once is
// structurally impossible within one Run call — there is no separate
// goroutine or timer that could fire a second Invoke while the first is
// still in flight.
//
// Graceful shutdown: shutdownCtx is only ever consulted BETWEEN passes —
// before starting the next Invoke call, and while waiting out Interval — and
// is deliberately never wired into the context passed to Invoke itself
// (Invoke always receives a fresh context.Background()-derived context).
// This means SIGINT/SIGTERM never aborts a Superintendent pass that has
// already started; Run finishes the current pass, records its result, and
// then stops instead of starting another one.
func Run(shutdownCtx context.Context, opts Options) error {
	return run(shutdownCtx, opts, defaultSleeper)
}

func run(shutdownCtx context.Context, opts Options, sleep sleeper) error {
	if opts.Invoke == nil {
		return fmt.Errorf("runloop: Options.Invoke is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	statePath := state.Path(opts.RootDir)

	for {
		if shutdownRequested(shutdownCtx) {
			logger.Println("hermit run: shutdown signal received, stopping")
			return nil
		}
		curSt, loadErr := state.Load(statePath)
		if loadErr != nil {
			logger.Printf("hermit run: warning: failed to load state: %v", loadErr)
		}
		if curSt.Status == state.StatusQuit {
			logger.Println("hermit run: quit status detected, stopping (not resumable; run `hermit run` again to restart)")
			return nil
		}
		if curSt.Status == state.StatusPaused {
			logger.Println("hermit run: paused status detected, skipping this tick")
			if sleep(shutdownCtx, opts.Interval) {
				logger.Println("hermit run: shutdown signal received while paused, stopping")
				return nil
			}
			continue
		}

		logger.Println("hermit run: starting pass")
		// Deliberately context.Background(), not shutdownCtx: see Run's doc
		// comment on why an in-flight pass is never interrupted.
		passErr := opts.Invoke(context.Background(), opts.RootDir)

		st, loadErr := state.Load(statePath)
		if loadErr != nil {
			logger.Printf("hermit run: warning: failed to load state: %v", loadErr)
		}
		n := now()
		if passErr != nil {
			st.ConsecutiveFailures++
			logger.Printf("hermit run: pass failed (%d consecutive failure(s)): %v", st.ConsecutiveFailures, passErr)
			if opts.FailureNotifyThreshold > 0 && st.ConsecutiveFailures == opts.FailureNotifyThreshold {
				msg := fmt.Sprintf("HERMIT: %d consecutive `hermit run` passes have failed. Last error: %v", st.ConsecutiveFailures, passErr)
				if opts.Notify == nil {
					logger.Println("hermit run: warning: failure threshold reached but no Notify function configured")
				} else if err := opts.Notify(opts.WebhookURL, opts.WebhookType, "run_failure", msg); err != nil {
					logger.Printf("hermit run: warning: failed to send failure notification: %v", err)
				}
			}
		} else {
			st.ConsecutiveFailures = 0
			st.LastSuccessTick = &n
			logger.Println("hermit run: pass completed successfully")
		}
		if err := state.Save(statePath, st); err != nil {
			logger.Printf("hermit run: warning: failed to save state: %v", err)
		}

		if shutdownRequested(shutdownCtx) {
			logger.Println("hermit run: shutdown signal received after pass, stopping")
			return nil
		}
		if sleep(shutdownCtx, opts.Interval) {
			logger.Println("hermit run: shutdown signal received while waiting, stopping")
			return nil
		}
	}
}

func shutdownRequested(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

