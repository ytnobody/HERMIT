package requirements

import (
	"os/exec"
	"strings"
)

// TestStatus is the outcome of running the test(s) associated with a single
// requirement.
type TestStatus string

const (
	// TestPassed means at least one matching test ran and all of them passed.
	TestPassed TestStatus = "passed"
	// TestFailed means at least one matching test ran and at least one of
	// them failed (a regression).
	TestFailed TestStatus = "failed"
	// TestNotFound means the test command ran but no test matching the
	// requirement's ID could be found (the requirement is unimplemented).
	TestNotFound TestStatus = "not_found"
)

// Runner executes the test(s) associated with a requirement ID and reports
// whether they were found, and if so whether they passed.
type Runner interface {
	Run(reqID string) (TestStatus, string, error)
}

// reqIDPlaceholder is substituted with the requirement ID inside a
// test_command template, e.g. "go test ./... -run '^{req_id}$' -v".
const reqIDPlaceholder = "{req_id}"

// CommandRunner runs a shell command template (from harness.toml's
// [requirements] test_command) to determine a requirement's test status.
//
// The template must produce output containing "=== RUN" markers for every
// test that was actually selected/executed (i.e. `go test -v` style
// output). This is how CommandRunner distinguishes "no test exists for this
// requirement" (zero RUN markers) from "the test exists and failed" (one or
// more RUN markers, non-zero exit code) — a bare non-zero exit code alone is
// ambiguous, since `go test -run <pattern>` exits 0 when the pattern matches
// nothing.
type CommandRunner struct {
	// Template is the command to run, with "{req_id}" substituted for the
	// requirement ID being checked.
	Template string
}

// Run substitutes reqID into the configured template, executes it via
// `sh -c`, and classifies the result.
func (r CommandRunner) Run(reqID string) (TestStatus, string, error) {
	cmdStr := strings.ReplaceAll(r.Template, reqIDPlaceholder, reqID)
	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if strings.Count(output, "=== RUN") == 0 {
		return TestNotFound, output, nil
	}
	if err == nil {
		return TestPassed, output, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		// Tests were selected (we saw RUN markers) but the command exited
		// non-zero: at least one of them failed.
		return TestFailed, output, nil
	}
	// The command itself could not be run at all (e.g. binary missing).
	return TestNotFound, output, err
}
