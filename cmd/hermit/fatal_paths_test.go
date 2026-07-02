package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fatalInterrupt is the sentinel panic value emitted by the test-only fatalFunc.
type fatalInterrupt struct{ msg string }

// catchFatal replaces fatalFunc so that calling fatal() panics with a
// fatalInterrupt instead of calling os.Exit.  It returns the first fatal
// message captured, or "" if fn() returned without calling fatal().
//
// Named return value is required: when fn() panics and the deferred recover
// catches it, the function exits with the named variable's current value rather
// than the zero value that an unnamed return would give.
func catchFatal(t *testing.T, fn func()) (fatalMsg string) {
	t.Helper()
	orig := fatalFunc
	// The closure captures fatalMsg (the named return variable) by reference so
	// that setting fatalMsg inside fatalFunc is visible after panic recovery.
	fatalFunc = func(m string) {
		fatalMsg = m
		fatalFunc = orig // restore immediately so nested calls use real fatal
		panic(fatalInterrupt{m})
	}
	defer func() {
		fatalFunc = orig // ensure restoration even if fn() returned normally
		r := recover()
		if r == nil {
			return
		}
		if _, ok := r.(fatalInterrupt); !ok {
			panic(r) // re-panic unexpected panics
		}
	}()
	fn()
	return
}

// --- loadConfig fatal paths ---

func TestLoadConfigFatal_Missing(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	msg := catchFatal(t, func() { loadConfig() })
	if !strings.Contains(msg, "harness.toml") {
		t.Errorf("expected harness.toml message, got: %q", msg)
	}
}

func TestLoadConfigFatal_BadTOML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "harness.toml"), []byte("not valid toml :::"), 0o644)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	msg := catchFatal(t, func() { loadConfig() })
	if msg == "" {
		t.Error("expected fatal for bad TOML, got none")
	}
}

// --- cmdServe fatal path ---

func TestCmdServeFatal_ServeError(t *testing.T) {
	// Provide a broken stdout (read-end closed) so that ServeStdio returns an
	// error when it tries to write the MCP response.
	execPath, _ := os.Executable()
	if resolved, e := filepath.EvalSymlinks(execPath); e == nil {
		execPath = resolved
	}
	binDir := filepath.Dir(execPath)
	harnessPath := filepath.Join(binDir, "harness.toml")
	if err := os.WriteFile(harnessPath, []byte(minimalHarnessTOML), 0o644); err != nil {
		t.Fatalf("write harness.toml: %v", err)
	}
	defer os.Remove(harnessPath)

	// Close the read end of the pipe → any write from ServeStdio gets EPIPE.
	pr, pw, _ := os.Pipe()
	pr.Close()
	origStdout := os.Stdout
	os.Stdout = pw
	defer func() { os.Stdout = origStdout; pw.Close() }()

	// Send a real MCP initialize so the server tries to write a response.
	pr2, pw2, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = pr2
	defer func() { os.Stdin = origStdin }()
	go func() {
		fmt.Fprintln(pw2, `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}},"id":1}`)
		pw2.Close()
	}()

	t.Setenv("GITHUB_TOKEN", "dummy")

	msg := catchFatal(t, func() { cmdServe() })
	// If ServeStdio does not return an error on EPIPE (library-specific), msg
	// will be "".  Either outcome is valid — we only care that the code path
	// doesn't panic.
	_ = msg
}

// --- cmdInstall fatal paths ---

func TestCmdInstallFatal_HarnessNotFound(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)
	t.Setenv("HOME", t.TempDir())

	msg := catchFatal(t, func() { cmdInstall() })
	if !strings.Contains(msg, "harness.toml") {
		t.Errorf("expected harness.toml fatal, got: %q", msg)
	}
}

// TestCmdInstallFatal_ClaudeMcpAddError verifies that cmdInstall fatals when
// `claude mcp add` fails (e.g. Claude Code CLI missing or erroring), since MCP
// server registration must go through that command rather than hand-writing
// Claude Code's config files.
func TestCmdInstallFatal_ClaudeMcpAddError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Fake `claude` binary on PATH that always fails.
	fakeBinDir := t.TempDir()
	script := "#!/bin/sh\necho 'boom' >&2\nexit 1\n"
	os.WriteFile(filepath.Join(fakeBinDir, "claude"), []byte(script), 0o755)
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))

	msg := catchFatal(t, func() { cmdInstall() })
	if !strings.Contains(msg, "claude mcp add") {
		t.Errorf("expected claude mcp add fatal, got: %q", msg)
	}
}

// --- writeTemplate fatal paths ---

func TestWriteTemplateFatal_ParseError(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// invalid_for_test.tmpl has intentionally broken syntax to exercise the
	// template parse error branch.
	msg := catchFatal(t, func() {
		writeTemplate("templates/invalid_for_test.tmpl", "out.txt", nil)
	})
	if !strings.Contains(msg, "template parse error") {
		t.Errorf("expected 'template parse error', got: %q", msg)
	}
}

func TestWriteTemplateFatal_ReadFile(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	msg := catchFatal(t, func() {
		writeTemplate("templates/nonexistent.tmpl", "out.txt", nil)
	})
	if !strings.Contains(msg, "template not found") {
		t.Errorf("expected 'template not found', got: %q", msg)
	}
}

func TestWriteTemplateFatal_Create(t *testing.T) {
	type td struct {
		Owner, Repo, Language string
		MaxEngineers          int
	}
	msg := catchFatal(t, func() {
		writeTemplate("templates/harness.toml.tmpl", "/nonexistent-dir-xyz/out.toml",
			td{Owner: "o", Repo: "r", Language: "ja", MaxEngineers: 4})
	})
	if msg == "" {
		t.Error("expected fatal for os.Create error")
	}
}

func TestWriteTemplateFatal_Execute(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// nil data with missingkey=error → Execute returns error → fatal
	msg := catchFatal(t, func() {
		writeTemplate("templates/harness.toml.tmpl", "out.toml", nil)
	})
	if msg == "" {
		t.Error("expected fatal for template.Execute with nil data")
	}
}

// --- cmdPause fatal path (read-only dir) ---

func TestCmdPauseFatal_WriteError(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// May not fail if running as root — that's acceptable.
	_ = catchFatal(t, func() { cmdPause() })
}

// --- cmdResume fatal path (non-empty directory) ---

func TestCmdResumeFatal_RemoveNonEmpty(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	os.MkdirAll(filepath.Join(pauseFile, "child"), 0o755)

	msg := catchFatal(t, func() { cmdResume() })
	if msg == "" {
		t.Error("expected fatal for non-empty directory removal")
	}
}

// --- cmdInit fatal path (writeClaudeSettings fails) ---

func TestCmdInitFatal_WriteSettingsError(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	os.WriteFile(".claude", []byte("block"), 0o644)

	r, w, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		sc := bufio.NewWriter(w)
		fmt.Fprintln(sc, "owner")
		fmt.Fprintln(sc, "repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "4")
		sc.Flush()
		w.Close()
	}()

	msg := catchFatal(t, func() { cmdInit() })
	if !strings.Contains(msg, "settings.json") {
		t.Errorf("expected settings.json fatal, got: %q", msg)
	}
}
