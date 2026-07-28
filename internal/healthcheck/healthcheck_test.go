package healthcheck

import (
	"strings"
	"testing"
	"time"
)

func TestRunChecks_Passed(t *testing.T) {
	results := RunChecks([]Check{{Name: "ok-check", Command: "echo all-good; exit 0"}})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.Name != "ok-check" {
		t.Errorf("Name = %q, want %q", r.Name, "ok-check")
	}
	if !r.Ok {
		t.Errorf("Ok = false, want true (output=%q)", r.Output)
	}
	if !strings.Contains(r.Output, "all-good") {
		t.Errorf("Output = %q, want it to contain %q", r.Output, "all-good")
	}
}

func TestRunChecks_Failed(t *testing.T) {
	results := RunChecks([]Check{{Name: "bad-check", Command: "echo something-broke; exit 1"}})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.Ok {
		t.Errorf("Ok = true, want false")
	}
	if !strings.Contains(r.Output, "something-broke") {
		t.Errorf("Output = %q, want it to contain %q", r.Output, "something-broke")
	}
}

func TestRunChecks_Timeout(t *testing.T) {
	origTimeout, origWaitDelay := Timeout, WaitDelay
	// Shrink the timeout (and its WaitDelay grace period) for the test so
	// it runs fast; restore both after.
	Timeout = 50 * time.Millisecond
	WaitDelay = 50 * time.Millisecond
	defer func() { Timeout, WaitDelay = origTimeout, origWaitDelay }()

	results := RunChecks([]Check{{Name: "slow-check", Command: "sleep 5"}})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.Ok {
		t.Errorf("Ok = true, want false (command should have timed out)")
	}
	if !strings.Contains(r.Output, "timed out") {
		t.Errorf("Output = %q, want it to mention the timeout", r.Output)
	}
}

func TestRunChecks_MultipleInOrder(t *testing.T) {
	results := RunChecks([]Check{
		{Name: "first", Command: "exit 0"},
		{Name: "second", Command: "exit 1"},
	})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Name != "first" || !results[0].Ok {
		t.Errorf("results[0] = %+v, want ok first", results[0])
	}
	if results[1].Name != "second" || results[1].Ok {
		t.Errorf("results[1] = %+v, want failing second", results[1])
	}
}

func TestRunChecks_SkipsUnsupportedType(t *testing.T) {
	results := RunChecks([]Check{
		{Name: "command-check", Command: "exit 0", Type: "command"},
		{Name: "http-check", Command: "irrelevant", Type: "http"},
		{Name: "default-type-check", Command: "exit 0"},
	})
	var names []string
	for _, r := range results {
		names = append(names, r.Name)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v, want only the two supported (command-type) checks", names)
	}
	for _, n := range names {
		if n == "http-check" {
			t.Errorf("unsupported type=http check %q should have been skipped, got results = %v", n, names)
		}
	}
}
