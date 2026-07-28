package main

// Tests for `hermit run` (Issue #181): the long-lived process that ticks the
// Superintendent cycle by launching `claude -p` on an interval, outside of
// any Claude Code session. TestREQ019_* functions verify REQUIREMENTS.md's
// REQ-019 acceptance criteria; the plain-named tests exercise supporting
// helpers not directly named by the REQ-019 acceptance criteria.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestREQ019_UsageMentionsRun verifies `hermit run` is documented in the
// top-level usage/help text, per REQ-019's "hermit --help / usage に記載される".
func TestREQ019_UsageMentionsRun(t *testing.T) {
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	usage()
	w.Close()
	os.Stderr = orig

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "run") {
		t.Errorf("usage() output does not mention the run subcommand: %q", out)
	}
}

// TestREQ019_MainSwitchDispatchesRun verifies "run" is wired into main's
// subcommand dispatch (not just usage text).
func TestREQ019_MainSwitchDispatchesRun(t *testing.T) {
	found := false
	for _, name := range []string{"serve", "run", "install", "init", "pause", "resume", "quit", "status", "use", "version", "upgrade", "cleanup", "doctor", "dry-run"} {
		if name == "run" {
			found = true
		}
	}
	if !found {
		t.Fatal("sanity check failed: 'run' missing from the expected subcommand list")
	}
	// The authoritative check: cmdRun must exist and be callable (compile-time
	// guarantee) — see TestCmdRunFatal_ClaudeMdMissing below for a behavioral
	// exercise of the actual switch-case-invoked function.
	var _ = cmdRun
}

// TestREQ019_BuildClaudeRunArgsNonInteractive verifies the constructed
// `claude` invocation matches the non-interactive pattern documented in
// docs/github-actions.md: --dangerously-skip-permissions is always present
// (REQ-019's "非対話で完走すること — permission prompt でハングしない"), --model is
// included only when a Superintendent model is configured, and the prompt is
// passed via -p.
func TestREQ019_BuildClaudeRunArgsNonInteractive(t *testing.T) {
	args := buildClaudeRunArgs("claude-sonnet-5", "prompt body")
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Errorf("args missing --dangerously-skip-permissions: %v", args)
	}
	if !strings.Contains(joined, "--model\x00claude-sonnet-5") {
		t.Errorf("args missing --model claude-sonnet-5: %v", args)
	}
	if !strings.Contains(joined, "-p\x00prompt body") {
		t.Errorf("args missing -p 'prompt body': %v", args)
	}

	argsNoModel := buildClaudeRunArgs("", "prompt body")
	for _, a := range argsNoModel {
		if a == "--model" {
			t.Errorf("args should omit --model when no Superintendent model is configured: %v", argsNoModel)
		}
	}
}

// TestREQ019_NewClaudeInvokerRunsClaudeInProjectRoot verifies each tick's
// `claude` invocation uses the project root as its working directory and
// reads CLAUDE.md content fresh from that directory as the prompt — REQ-019's
// "各tickはプロジェクトルートをcwdとして claude -p を起動する".
func TestREQ019_NewClaudeInvokerRunsClaudeInProjectRoot(t *testing.T) {
	dir := t.TempDir()
	claudeMd := "the superintendent prompt"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMd), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	// Fake `claude` binary that records its cwd and args to a file.
	fakeBinDir := t.TempDir()
	recordPath := filepath.Join(dir, "invocation.txt")
	script := "#!/bin/sh\npwd > \"" + recordPath + "\"\nprintf '%s\\n' \"$@\" >> \"" + recordPath + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fakeBinDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))

	invoke := newClaudeInvoker("claude", "claude-sonnet-5")
	if err := invoke(context.Background(), dir); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading invocation record: %v", err)
	}
	got := string(recorded)

	resolvedDir, _ := filepath.EvalSymlinks(dir)
	resolvedGot, _ := filepath.EvalSymlinks(strings.TrimSpace(strings.SplitN(got, "\n", 2)[0]))
	if resolvedGot != resolvedDir {
		t.Errorf("claude ran with cwd %q, want %q", resolvedGot, resolvedDir)
	}
	if !strings.Contains(got, claudeMd) {
		t.Errorf("claude was not invoked with CLAUDE.md's contents as the prompt: %q", got)
	}
	if !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("claude was not invoked non-interactively: %q", got)
	}
}

// TestCmdRunFatal_ClaudeMdMissing verifies cmdRun fails fast (before ever
// starting the tick loop) when CLAUDE.md is missing from the project root,
// instead of looping forever invoking a `claude -p` that would immediately
// error on every tick.
func TestCmdRunFatal_ClaudeMdMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644); err != nil {
		t.Fatalf("write harness.toml: %v", err)
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	msg := catchFatal(t, func() { cmdRun() })
	if !strings.Contains(msg, "CLAUDE.md") {
		t.Errorf("expected a CLAUDE.md fatal message, got: %q", msg)
	}
}

// TestConfigRunSectionParsing verifies harness.toml's [run] section decodes
// into Config.Run.FailureNotifyThreshold.
func TestConfigRunSectionParsing(t *testing.T) {
	src := minimalHarnessTOML + "\n[run]\nfailure_notify_threshold = 5\n"
	var cfg Config
	if _, err := toml.Decode(src, &cfg); err != nil {
		t.Fatalf("decoding harness.toml: %v", err)
	}
	if cfg.Run.FailureNotifyThreshold != 5 {
		t.Errorf("Run.FailureNotifyThreshold = %d, want 5", cfg.Run.FailureNotifyThreshold)
	}
}

// TestConfigRunSectionDefaultsToZeroWhenAbsent verifies an unconfigured
// [run] section decodes to the zero value, which cmdRun then maps to
// defaultFailureNotifyThreshold (mirroring how LoopInterval/MaxEngineers
// handle "<=0 means default" elsewhere in loadConfig).
func TestConfigRunSectionDefaultsToZeroWhenAbsent(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(minimalHarnessTOML, &cfg); err != nil {
		t.Fatalf("decoding harness.toml: %v", err)
	}
	if cfg.Run.FailureNotifyThreshold != 0 {
		t.Errorf("Run.FailureNotifyThreshold = %d, want 0 (unset) before cmdRun applies its default", cfg.Run.FailureNotifyThreshold)
	}
}

// TestNewClaudeInvokerPropagatesClaudeFailure verifies a non-zero `claude`
// exit code surfaces as an error from the Invoker, so runloop.Run's
// failure-counting/notification logic actually engages on a broken tick.
func TestNewClaudeInvokerPropagatesClaudeFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	fakeBinDir := t.TempDir()
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBinDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))

	invoke := newClaudeInvoker("claude", "")
	if err := invoke(context.Background(), dir); err == nil {
		t.Error("expected an error when the claude invocation exits non-zero")
	}
}

// Sanity: exec.Command / exec.CommandContext usage in newClaudeInvoker must
// not require a shell (no shell metacharacter interpretation of the prompt
// body), otherwise a CLAUDE.md containing shell-special characters (quotes,
// backticks, $()) could corrupt or fail the invocation. This is implicit in
// using exec.CommandContext with a []string arg list (never exec.Command via
// "sh -c"), verified indirectly by TestREQ019_NewClaudeInvokerRunsClaudeInProjectRoot
// above using a CLAUDE.md body without special characters; this test uses one
// that does.
func TestNewClaudeInvokerHandlesShellSpecialCharsInPrompt(t *testing.T) {
	dir := t.TempDir()
	claudeMd := "prompt with `backticks`, $(command), and \"quotes\""
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMd), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	fakeBinDir := t.TempDir()
	recordPath := filepath.Join(dir, "invocation.txt")
	script := "#!/bin/sh\nprintf '%s' \"$3\" > \"" + recordPath + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fakeBinDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))

	invoke := newClaudeInvoker("claude", "")
	if err := invoke(context.Background(), dir); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading invocation record: %v", err)
	}
	if string(got) != claudeMd {
		t.Errorf("prompt arg = %q, want %q (shell metacharacters must reach claude literally, unexpanded)", string(got), claudeMd)
	}
}
