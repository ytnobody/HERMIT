package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// directMain calls main() with the given args and captures stdout.
// Restores os.Args and os.Stdout afterwards.
func directMain(t *testing.T, args []string) string {
	t.Helper()
	origArgs := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = origArgs })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	t.Cleanup(func() { os.Stdout = origOut })

	main()

	pw.Close()
	var buf bytes.Buffer
	buf.ReadFrom(pr)
	return buf.String()
}

func TestMainSwitch_Pause(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	directMain(t, []string{"hermit", "pause"})

	if _, err := os.Stat(pauseFile); os.IsNotExist(err) {
		t.Error(".hermit-paused not created by main pause")
	}
}

func TestMainSwitch_Resume(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	directMain(t, []string{"hermit", "pause"})
	out := directMain(t, []string{"hermit", "resume"})
	if !strings.Contains(out, "resumed") {
		t.Errorf("expected resume message, got %q", out)
	}
}

func TestMainSwitch_Status(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	out := directMain(t, []string{"hermit", "status"})
	if !strings.Contains(out, "running") {
		t.Errorf("expected running status, got %q", out)
	}
}

func TestMainSwitch_Install(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	_, recordPath := writeFakeClaude(t)

	directMain(t, []string{"hermit", "install"})

	if _, err := os.Stat(recordPath); err != nil {
		t.Error("claude mcp add was not invoked via main install")
	}
}

func TestMainSwitch_Init(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	r, w, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		sc := bufio.NewWriter(w)
		fmt.Fprintln(sc, "owner")
		fmt.Fprintln(sc, "repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		sc.Flush()
		w.Close()
	}()

	directMain(t, []string{"hermit", "init"})

	if _, err := os.Stat(filepath.Join(dir, "harness.toml")); err != nil {
		t.Error("harness.toml not created via main init")
	}
	if _, err := os.Stat(filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "hermit-task.md")); err != nil {
		t.Error(".github/ISSUE_TEMPLATE/hermit-task.md not created via main init")
	}
}

func TestMainSwitch_Serve(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Skip("cannot determine executable path")
	}
	if resolved, e := filepath.EvalSymlinks(execPath); e == nil {
		execPath = resolved
	}
	binDir := filepath.Dir(execPath)
	harnessPath := filepath.Join(binDir, "harness.toml")
	if err := os.WriteFile(harnessPath, []byte(minimalHarnessTOML), 0o644); err != nil {
		t.Fatalf("write harness.toml: %v", err)
	}
	defer os.Remove(harnessPath)

	pr, pw, _ := os.Pipe()
	pw.Close() // EOF → ServeStdio returns
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()

	t.Setenv("GITHUB_TOKEN", "dummy-token")

	directMain(t, []string{"hermit", "serve"})
}
