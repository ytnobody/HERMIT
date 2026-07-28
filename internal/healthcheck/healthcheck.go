// Package healthcheck implements the production health-check sweep
// described in HERMIT Issue #190: a project can declare a list of
// [[health_checks]] commands in harness.toml, and the Superintendent loop
// periodically runs them, opening (deduped) GitHub issues for newly-failing
// checks and posting a one-time "recovered" comment when a previously
// failing check starts passing again.
//
// This intentionally mirrors internal/requirements' config-loading,
// command-execution/timeout, and MCP-tool-registration patterns (see
// internal/requirements/runner.go and internal/requirements/reconcile.go) so
// the two sweeps stay consistent for anyone reading both.
package healthcheck

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Check is a single configured health check.
type Check struct {
	// Name identifies the check (used in the issue title-prefix dedup
	// convention "[health-check: <name>]" — see TitlePrefix).
	Name string
	// Command is the shell command executed to determine health. Exit code
	// 0 is treated as healthy (ok:true); any other exit code, or a command
	// that fails to start, is treated as unhealthy (ok:false).
	Command string
	// Type selects the check mechanism. Only "command" (the zero value
	// also defaults to "command") is supported today; any other value is
	// skipped by RunChecks as a forward-compatible no-op (e.g. a future
	// "http" type declared in harness.toml by a newer HERMIT version).
	Type string
}

// Result is the outcome of running a single Check.
type Result struct {
	Name   string `json:"name"`
	Ok     bool   `json:"ok"`
	Output string `json:"output"`
}

// Timeout is the maximum duration a single health-check command may run
// before being treated as a failure. It is a var (not a const) so tests can
// shrink it to keep the timeout path fast to exercise; production code
// should treat it as a constant and leave it at its default value.
var Timeout = 30 * time.Second

// WaitDelay bounds how long a timed-out check's CombinedOutput call may
// additionally block waiting for output pipes to close (see runOne). Also a
// var so tests can shrink it.
var WaitDelay = 2 * time.Second

// TypeCommand is the only currently-supported Check.Type value.
const TypeCommand = "command"

// RunChecks executes each configured check's command and returns one Result
// per supported check, in the same order as checks. Checks whose Type is
// set to anything other than "" or "command" are silently skipped (not
// included in the returned slice) — see Check.Type.
func RunChecks(checks []Check) []Result {
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		if c.Type != "" && c.Type != TypeCommand {
			continue
		}
		results = append(results, runOne(c))
	}
	return results
}

// runOne runs a single check's command with a Timeout deadline, via
// `sh -c` (matching requirements.CommandRunner's convention).
func runOne(c Check) Result {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	// WaitDelay bounds how long CombinedOutput waits for the output pipes
	// to close after the process is killed. Without it, a command whose
	// shell forks a grandchild that inherits stdout/stderr (rather than
	// exec-replacing itself, which depends on the shell) can leave
	// CombinedOutput blocked for the grandchild's full runtime even though
	// the immediate child was killed on the Timeout deadline — silently
	// defeating the timeout. See https://pkg.go.dev/os/exec#Cmd.WaitDelay.
	cmd.WaitDelay = WaitDelay
	out, err := cmd.CombinedOutput()
	output := string(out)

	if ctx.Err() == context.DeadlineExceeded {
		return Result{
			Name:   c.Name,
			Ok:     false,
			Output: appendNote(output, fmt.Sprintf("(timed out after %s)", Timeout)),
		}
	}
	return Result{Name: c.Name, Ok: err == nil, Output: output}
}

// appendNote appends note to output on its own trailing line, avoiding a
// leading blank line when output is empty.
func appendNote(output, note string) string {
	if output == "" {
		return note
	}
	return output + "\n" + note
}
