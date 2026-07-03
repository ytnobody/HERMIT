package requirements

import (
	"strings"
	"testing"
)

func TestCommandRunner_Passed(t *testing.T) {
	r := CommandRunner{Template: `echo "=== RUN   TestREQ001_Foo"; echo "--- PASS: TestREQ001_Foo"; exit 0`}
	status, output, err := r.Run("REQ-001")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if status != TestPassed {
		t.Errorf("status = %q, want %q (output=%q)", status, TestPassed, output)
	}
}

func TestCommandRunner_Failed(t *testing.T) {
	r := CommandRunner{Template: `echo "=== RUN   TestREQ001_Foo"; echo "--- FAIL: TestREQ001_Foo"; exit 1`}
	status, output, err := r.Run("REQ-001")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if status != TestFailed {
		t.Errorf("status = %q, want %q (output=%q)", status, TestFailed, output)
	}
}

func TestCommandRunner_NotFound_ZeroExit(t *testing.T) {
	// This mirrors `go test -run <pattern>` when the pattern matches nothing:
	// exit code 0, but no "=== RUN" markers at all.
	r := CommandRunner{Template: `echo "testing: warning: no tests to run"; exit 0`}
	status, _, err := r.Run("REQ-999")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if status != TestNotFound {
		t.Errorf("status = %q, want %q", status, TestNotFound)
	}
}

func TestCommandRunner_ReqIDSubstitution(t *testing.T) {
	r := CommandRunner{Template: `echo "=== RUN {req_id}"; exit 0`}
	_, output, err := r.Run("REQ-042")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output, "REQ-042") {
		t.Errorf("output = %q, want it to contain substituted req_id REQ-042", output)
	}
}

func TestCommandRunner_CommandMissing(t *testing.T) {
	// sh itself will run fine but fail to exec a nonexistent binary; since no
	// "=== RUN" markers are produced either way, this is classified the same
	// as "no matching test" rather than a hard Go-level exec error.
	r := CommandRunner{Template: `this-binary-does-not-exist-anywhere-12345`}
	status, _, err := r.Run("REQ-001")
	if err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}
	if status != TestNotFound {
		t.Errorf("status = %q, want %q", status, TestNotFound)
	}
}
