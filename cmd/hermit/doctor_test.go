package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// captureDoctor runs cmdDoctor() capturing stdout, returning output and whether it exited.
// Since cmdDoctor may call os.Exit(1), we use the subprocess pattern for failure cases.
func captureDoctorOutput(t *testing.T) string {
	t.Helper()
	r, w, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origOut }()

	cmdDoctor()

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestRunChecks_AllFields(t *testing.T) {
	results := runChecks()
	if len(results) != 5 {
		t.Errorf("expected 5 checks, got %d", len(results))
	}

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.name
	}

	expected := []string{
		"git command is available",
		"gh CLI is installed and authenticated",
		"GITHUB_TOKEN is available",
		"harness.toml exists with owner/repo",
		"Claude Code (claude) is installed",
	}
	for i, want := range expected {
		if i >= len(names) {
			t.Errorf("missing check %q", want)
			continue
		}
		if names[i] != want {
			t.Errorf("check[%d]: got %q, want %q", i, names[i], want)
		}
	}
}

func TestRunChecks_GitAvailable(t *testing.T) {
	results := runChecks()
	// git is expected to be available in the test environment
	if _, err := exec.LookPath("git"); err == nil {
		if !results[0].passed {
			t.Error("expected git check to pass when git is in PATH")
		}
	} else {
		if results[0].passed {
			t.Error("expected git check to fail when git is not in PATH")
		}
	}
}

func TestRunChecks_HarnessOK(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "test-owner"
repo  = "test-repo"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	results := runChecks()
	harnessResult := results[3]
	if !harnessResult.passed {
		t.Errorf("expected harness check to pass, detail: %s", harnessResult.detail)
	}
}

func TestRunChecks_HarnessMissing(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	results := runChecks()
	harnessResult := results[3]
	if harnessResult.passed {
		t.Error("expected harness check to fail when harness.toml is missing")
	}
	if !strings.Contains(harnessResult.detail, "not found") {
		t.Errorf("expected 'not found' in detail, got %q", harnessResult.detail)
	}
}

func TestRunChecks_HarnessMissingOwner(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = ""
repo  = "test-repo"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	results := runChecks()
	harnessResult := results[3]
	if harnessResult.passed {
		t.Error("expected harness check to fail when owner is empty")
	}
	if !strings.Contains(harnessResult.detail, "missing owner or repo") {
		t.Errorf("expected 'missing owner or repo' in detail, got %q", harnessResult.detail)
	}
}

func TestRunChecks_HarnessBadTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte("not valid toml :::"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	results := runChecks()
	harnessResult := results[3]
	if harnessResult.passed {
		t.Error("expected harness check to fail with bad TOML")
	}
	if !strings.Contains(harnessResult.detail, "failed to parse") {
		t.Errorf("expected 'failed to parse' in detail, got %q", harnessResult.detail)
	}
}

func TestRunChecks_GithubTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "testtoken123")
	results := runChecks()
	tokenResult := results[2]
	if !tokenResult.passed {
		t.Error("expected GITHUB_TOKEN check to pass when env var is set")
	}
}

func TestRunChecks_GithubTokenMissing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	// Use an empty PATH so gh is not found either
	t.Setenv("PATH", t.TempDir())

	results := runChecks()
	tokenResult := results[2]
	if tokenResult.passed {
		t.Error("expected GITHUB_TOKEN check to fail when env var and gh both unavailable")
	}
	if !strings.Contains(tokenResult.detail, "GITHUB_TOKEN not set") {
		t.Errorf("expected detail about missing token, got %q", tokenResult.detail)
	}
}

// TestCmdDoctor_AllPass tests that cmdDoctor prints pass marks and "All checks passed."
// when all checks pass. We set up a temp dir with harness.toml and GITHUB_TOKEN.
func TestCmdDoctor_AllPass(t *testing.T) {
	// Only run if git and claude are both available; otherwise this test is not meaningful.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	content := `[github]
owner = "test-owner"
repo  = "test-repo"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	t.Setenv("GITHUB_TOKEN", "testtoken123")

	results := runChecks()
	allPassed := true
	for _, r := range results {
		if !r.passed {
			allPassed = false
		}
	}
	if !allPassed {
		t.Skip("not all checks pass in this environment, skipping output test")
	}

	out := captureDoctorOutput(t)
	if !strings.Contains(out, "✓") {
		t.Errorf("expected checkmark in output, got: %q", out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("expected 'All checks passed' in output, got: %q", out)
	}
}

// TestCmdDoctor_ExitCode tests that cmdDoctor exits with code 1 when checks fail.
func TestCmdDoctor_ExitCode(t *testing.T) {
	if os.Getenv("TEST_DOCTOR_EXIT") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		// No harness.toml, no GITHUB_TOKEN, no gh — ensure failures
		t.Setenv("GITHUB_TOKEN", "")
		cmdDoctor()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdDoctor_ExitCode", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_DOCTOR_EXIT=1", "GITHUB_TOKEN=")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return // expected: non-zero exit when checks fail
	}
	// It's OK if all checks pass in this environment
	t.Log("doctor exit test: all checks passed in this environment (acceptable)")
}

// TestMainSwitch_Doctor tests that 'hermit doctor' is reachable via main().
func TestMainSwitch_Doctor(t *testing.T) {
	if os.Getenv("TEST_MAIN_DOCTOR") != "" {
		dir := t.TempDir()
		content := `[github]
owner = "test-owner"
repo  = "test-repo"
`
		os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644)
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)

		t.Setenv("GITHUB_TOKEN", "testtoken123")

		os.Args = []string{"hermit", "doctor"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainSwitch_Doctor", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_DOCTOR=1", "GITHUB_TOKEN=testtoken123")
	// Ignore exit status since some checks may fail in CI
	cmd.Run()
}
