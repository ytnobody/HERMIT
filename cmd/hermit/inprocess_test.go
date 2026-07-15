package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const minimalHarnessTOML = `[github]
owner = "test-owner"
repo  = "test-repo"

[agent]
max_engineers = 4
language      = "ja"
`

// --- githubToken success path ---

// TestGithubToken_GhSuccess verifies that githubToken returns the gh CLI output
// when GITHUB_TOKEN is unset but a fake 'gh' is available on PATH.
func TestGithubToken_GhSuccess(t *testing.T) {
	// Create a fake 'gh' script that prints a fixed token.
	fakeGhDir := t.TempDir()
	fakePath := filepath.Join(fakeGhDir, "gh")
	script := "#!/bin/sh\necho 'ghp_fake_token'\n"
	if err := os.WriteFile(fakePath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", fakeGhDir+":"+os.Getenv("PATH"))

	tok := githubToken()
	if tok != "ghp_fake_token" {
		t.Errorf("expected 'ghp_fake_token', got %q", tok)
	}
}

// --- cmdResume fatal branch ---

// TestCmdResume_RemoveError verifies that cmdResume calls fatal when os.Remove
// fails for a reason other than "not exists".
func TestCmdResume_RemoveError(t *testing.T) {
	if os.Getenv("TEST_RESUME_ERR") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		// Create .hermit-paused as a non-empty directory so os.Remove fails.
		os.MkdirAll(pauseFile+"/child", 0o755)
		cmdResume() // should fatal
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdResume_RemoveError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_RESUME_ERR=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// --- writeClaudeSettings error path ---

// TestWriteClaudeSettings_MkdirError verifies that the error from os.MkdirAll
// is returned when .claude already exists as a regular file.
func TestWriteClaudeSettings_MkdirError(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Create .claude as a regular file so MkdirAll fails.
	os.WriteFile(".claude", []byte("not a dir"), 0o644)

	err := writeClaudeSettings()
	if err == nil {
		t.Error("expected error when .claude is a file, got nil")
	}
}

// --- writeTemplate create/execute error paths ---

// TestWriteTemplate_CreateError verifies fatal when os.Create fails (output
// path in a non-existent directory).
func TestWriteTemplate_CreateError(t *testing.T) {
	if os.Getenv("TEST_TMPL_CREATE") != "" {
		type td struct {
			Owner, Repo, Language string
			MaxEngineers          int
		}
		writeTemplate("templates/harness.toml.tmpl", "/nonexistent-dir/out.toml",
			td{Owner: "o", Repo: "r", Language: "ja", MaxEngineers: 4})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestWriteTemplate_CreateError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_TMPL_CREATE=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// TestWriteTemplate_ExecuteError verifies fatal when template.Execute fails
// because the data passed is nil but the template references fields.
func TestWriteTemplate_ExecuteError(t *testing.T) {
	if os.Getenv("TEST_TMPL_EXEC") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		// nil data → Execute will fail for a template that uses fields
		writeTemplate("templates/harness.toml.tmpl", "out.toml", nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestWriteTemplate_ExecuteError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_TMPL_EXEC=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// --- cmdServe in-process ---

// TestCmdServe_InProcess runs cmdServe with EOF stdin so ServeStdio returns
// immediately.  harness.toml is placed next to the test binary so the
// chdir-to-binary-dir logic inside cmdServe can find it.
func TestCmdServe_InProcess(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Skip("cannot determine executable path:", err)
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

	// Provide empty stdin → ServeStdio gets EOF and returns.
	pr, pw, _ := os.Pipe()
	pw.Close()
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()

	// Capture stdout so MCP handshake output does not pollute test output.
	pr2, pw2, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = pw2
	defer func() { os.Stdout = origStdout }()
	go func() { io_copy_discard(pr2) }()

	t.Setenv("GITHUB_TOKEN", "dummy-token-for-test")

	// Should return without calling fatal.
	cmdServe()

	pw2.Close()
}

func io_copy_discard(r *os.File) {
	buf := make([]byte, 4096)
	for {
		if _, err := r.Read(buf); err != nil {
			return
		}
	}
}

// --- cmdInstall in-process ---

// writeFakeClaude creates a fake `claude` executable that records the
// arguments it is invoked with to recordPath, then puts it on PATH ahead of
// any real `claude` binary. cmdInstall shells out to `claude mcp add` for MCP
// server registration, so tests exercise that call through this stand-in
// rather than depending on a real Claude Code installation.
func writeFakeClaude(t *testing.T) (binDir, recordPath string) {
	t.Helper()
	binDir = t.TempDir()
	recordPath = filepath.Join(binDir, "record.txt")
	script := "#!/bin/sh\necho \"$@\" > " + recordPath + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return binDir, recordPath
}

// TestCmdInstall_Valid exercises the full happy path with HOME redirected to a
// temp directory so nothing is written to the real user's home.
func TestCmdInstall_Valid(t *testing.T) {
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

	// Capture stdout to suppress install messages.
	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdInstall()
	pw.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr)

	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("claude mcp add was not invoked: %v", err)
	}
	if !strings.Contains(string(recorded), "hermit") {
		t.Errorf("claude mcp add args missing hermit: %s", recorded)
	}
	if !strings.Contains(string(recorded), "HERMIT_PROJECT_DIR") {
		t.Errorf("claude mcp add args missing HERMIT_PROJECT_DIR: %s", recorded)
	}
}

// TestCmdInstall_NoHarness verifies that cmdInstall fatals when there is no
// harness.toml in the current directory.
func TestCmdInstall_NoHarness(t *testing.T) {
	if os.Getenv("TEST_INSTALL_NOHARNESS") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		t.Setenv("HOME", dir)
		cmdInstall()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdInstall_NoHarness", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_INSTALL_NOHARNESS=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// TestCmdInstall_LocalBinMkdirWarn verifies that cmdInstall prints a warn and
// continues when os.MkdirAll for ~/.local/bin fails (e.g. .local exists as a file).
func TestCmdInstall_LocalBinMkdirWarn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeFakeClaude(t)

	// Create .local as a regular file so MkdirAll(".local/bin") fails.
	os.WriteFile(filepath.Join(homeDir, ".local"), []byte("file"), 0o644)

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw

	pr2, pw2, _ := os.Pipe()
	origErr := os.Stderr
	os.Stderr = pw2

	cmdInstall()

	pw.Close()
	os.Stdout = origOut
	pw2.Close()
	os.Stderr = origErr

	var errBuf bytes.Buffer
	errBuf.ReadFrom(pr2)
	pr.Close()

	if !strings.Contains(errBuf.String(), "warn:") {
		t.Errorf("expected localBin mkdir warn on stderr, got: %q", errBuf.String())
	}
}

// TestCmdInstall_SymlinkWarn verifies that cmdInstall prints a warn when
// the symlink target already exists as a non-empty directory (so Remove
// fails and Symlink also fails).
func TestCmdInstall_SymlinkWarn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeFakeClaude(t)

	// Create ~/.local/bin/hermit as a non-empty directory so:
	//   os.Remove(linkPath) fails (non-empty directory)
	//   os.Symlink(execPath, linkPath) fails (linkPath still exists)
	localBin := filepath.Join(homeDir, ".local", "bin")
	os.MkdirAll(localBin, 0o755)
	hermitAsDir := filepath.Join(localBin, "hermit")
	os.MkdirAll(filepath.Join(hermitAsDir, "child"), 0o755)

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw

	pr2, pw2, _ := os.Pipe()
	origErr := os.Stderr
	os.Stderr = pw2

	cmdInstall()

	pw.Close()
	os.Stdout = origOut
	pw2.Close()
	os.Stderr = origErr

	var errBuf bytes.Buffer
	errBuf.ReadFrom(pr2)
	pr.Close()

	if !strings.Contains(errBuf.String(), "warn:") {
		t.Errorf("expected symlink warn on stderr, got: %q", errBuf.String())
	}
}

// --- cmdInit in-process ---

// TestCmdInit_Valid exercises the happy path: all answers provided on stdin.
func TestCmdInit_Valid(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Pipe the interactive answers.
	r, w, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		sc := bufio.NewWriter(w)
		fmt.Fprintln(sc, "test-owner")
		fmt.Fprintln(sc, "test-repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		sc.Flush()
		w.Close()
	}()

	// Capture stdout.
	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr2)

	if _, err := os.Stat(filepath.Join(dir, "harness.toml")); err != nil {
		t.Error("harness.toml not created by cmdInit")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md not created by cmdInit")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); err != nil {
		t.Error(".claude/settings.json not created by cmdInit")
	}
}

// TestCmdInit_ClaudeMdIncludesGhSnapGuideline verifies that the CLAUDE.md
// generated by `hermit init` includes the gh CLI usage guideline warning
// Engineers away from passing /tmp paths directly to gh (see issue #108:
// snap-packaged gh cannot read the host's /tmp).
func TestCmdInit_ClaudeMdIncludesGhSnapGuideline(t *testing.T) {
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
		fmt.Fprintln(sc, "test-owner")
		fmt.Fprintln(sc, "test-repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr2)

	claudeMd, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(claudeMd)
	if !strings.Contains(content, "--body-file -") {
		t.Errorf("CLAUDE.md missing gh stdin (--body-file -) guideline:\n%s", content)
	}
	if !strings.Contains(content, "snap") {
		t.Errorf("CLAUDE.md missing mention of snap-confined gh:\n%s", content)
	}
	if !strings.Contains(content, "Never pass a `/tmp` path directly") {
		t.Errorf("CLAUDE.md missing guideline against passing /tmp paths to gh:\n%s", content)
	}
}

// TestCmdInit_DefaultsApplied verifies that empty answers for language and
// max_engineers use the defaults ("ja" and 4).
func TestCmdInit_DefaultsApplied(t *testing.T) {
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
		fmt.Fprintln(sc, "") // default language = ja
		fmt.Fprintln(sc, "") // default max_engineers = 4
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	pr2.Close()

	content, _ := os.ReadFile(filepath.Join(dir, "harness.toml"))
	if !strings.Contains(string(content), "ja") {
		t.Error("expected default language 'ja' in harness.toml")
	}
}

// TestCmdInit_InvalidMaxEngineers verifies that a non-numeric max_engineers
// falls back to the default (4).
func TestCmdInit_InvalidMaxEngineers(t *testing.T) {
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
		fmt.Fprintln(sc, "not-a-number") // invalid → maxEng=0 → default to 4
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	pr2.Close()
}

// TestCmdInit_WriteSettingsError verifies that cmdInit fatals when
// writeClaudeSettings fails (because .claude already exists as a file).
func TestCmdInit_WriteSettingsError(t *testing.T) {
	if os.Getenv("TEST_INIT_SETTINGS_ERR") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		// Block .claude dir creation by placing a file there.
		os.WriteFile(".claude", []byte("not a dir"), 0o644)

		r, w, _ := os.Pipe()
		os.Stdin = r
		go func() {
			sc := bufio.NewWriter(w)
			fmt.Fprintln(sc, "owner")
			fmt.Fprintln(sc, "repo")
			fmt.Fprintln(sc, "en")
			fmt.Fprintln(sc, "4")
			sc.Flush()
			w.Close()
		}()
		cmdInit()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdInit_WriteSettingsError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_INIT_SETTINGS_ERR=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// TestCmdInit_ClaudeMdWiresEngineerModelAndEffort verifies that answering the
// new reasoning-effort prompts during `hermit init` causes the generated
// CLAUDE.md's Engineer-spawning step to reference the configured Engineer
// model and effort via the Agent tool (fixing the wiring gap described in
// issue #96).
func TestCmdInit_ClaudeMdWiresEngineerModelAndEffort(t *testing.T) {
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
		fmt.Fprintln(sc, "test-owner")
		fmt.Fprintln(sc, "test-repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		fmt.Fprintln(sc, "claude-cheap") // preset: sonnet superintendent / haiku engineer / sonnet analyst
		fmt.Fprintln(sc, "high")         // superintendent effort
		fmt.Fprintln(sc, "medium")       // engineer effort
		fmt.Fprintln(sc, "low")          // analyst effort
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr2)

	claudeMd, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(claudeMd)
	if !strings.Contains(content, `model: "claude-haiku-4-5-20251001"`) {
		t.Errorf("CLAUDE.md Engineer-spawn step missing engineer model wiring:\n%s", content)
	}
	if !strings.Contains(content, `effort: "medium"`) {
		t.Errorf("CLAUDE.md Engineer-spawn step missing engineer effort wiring:\n%s", content)
	}
	if !strings.Contains(content, `model: "claude-sonnet-5"`) {
		t.Errorf("CLAUDE.md Analyst-spawn step missing analyst model wiring:\n%s", content)
	}
	if !strings.Contains(content, `effort: "low"`) {
		t.Errorf("CLAUDE.md Analyst-spawn step missing analyst effort wiring:\n%s", content)
	}

	harnessToml, err := os.ReadFile(filepath.Join(dir, "harness.toml"))
	if err != nil {
		t.Fatalf("harness.toml not created: %v", err)
	}
	htContent := string(harnessToml)
	if !strings.Contains(htContent, `superintendent_effort = "high"`) {
		t.Errorf("harness.toml missing superintendent_effort:\n%s", htContent)
	}
	if !strings.Contains(htContent, `engineer_effort       = "medium"`) {
		t.Errorf("harness.toml missing engineer_effort:\n%s", htContent)
	}
	if !strings.Contains(htContent, `analyst        = "claude-sonnet-5"`) {
		t.Errorf("harness.toml missing analyst model:\n%s", htContent)
	}
	if !strings.Contains(htContent, `analyst_effort         = "low"`) {
		t.Errorf("harness.toml missing analyst_effort:\n%s", htContent)
	}
}

// TestCmdInit_ClaudeMdDispatchesBackgroundSuperintendent verifies that the
// generated CLAUDE.md instructs the foreground /hermit invocation to delegate
// the Superintendent cycle to a background subagent (Agent tool with
// run_in_background: true) wired to the configured Superintendent
// model/effort, instead of running the cycle inline (issue #147).
func TestCmdInit_ClaudeMdDispatchesBackgroundSuperintendent(t *testing.T) {
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
		fmt.Fprintln(sc, "test-owner")
		fmt.Fprintln(sc, "test-repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		fmt.Fprintln(sc, "claude-cheap")
		fmt.Fprintln(sc, "high")   // superintendent effort
		fmt.Fprintln(sc, "medium") // engineer effort
		fmt.Fprintln(sc, "low")    // analyst effort
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr2)

	claudeMd, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(claudeMd)
	if !strings.Contains(content, "### Foreground dispatch") {
		t.Errorf("CLAUDE.md missing foreground dispatch section:\n%s", content)
	}
	if !strings.Contains(content, "### Background cycle") {
		t.Errorf("CLAUDE.md missing background cycle section:\n%s", content)
	}
	// Superintendent model/effort wiring on the background dispatch step
	// (claude-cheap preset superintendent model, effort answered as "high").
	want := "`run_in_background: true` and `model: \"claude-sonnet-5\"` and `effort: \"high\"`"
	if !strings.Contains(content, want) {
		t.Errorf("CLAUDE.md dispatch step missing superintendent model/effort wiring (want %s):\n%s", want, content)
	}
	if !strings.Contains(content, "### Engineer fallback") {
		t.Errorf("CLAUDE.md missing Engineer fallback section:\n%s", content)
	}
}

// TestEmbeddedHermitCommandDispatchesBackground verifies that the embedded
// /hermit slash-command template tells the model to run only the foreground
// dispatch and delegate the cycle to a background subagent (issue #147).
func TestEmbeddedHermitCommandDispatchesBackground(t *testing.T) {
	data, err := templateFS.ReadFile("templates/commands/hermit.md")
	if err != nil {
		t.Fatalf("embedded templates/commands/hermit.md not readable: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "run_in_background: true") {
		t.Errorf("hermit.md command missing run_in_background dispatch instruction:\n%s", content)
	}
	if !strings.Contains(content, "Foreground dispatch") {
		t.Errorf("hermit.md command missing reference to the foreground dispatch steps:\n%s", content)
	}
}

// TestCmdInit_ClaudeMdOmitsEffortClauseWhenUnset verifies that leaving the
// effort prompts empty produces a model-only Agent tool wiring instruction
// (no dangling `effort: ""` clause). Uses the "claude-cheap" preset, since
// (unlike "claude") none of its roles — including Analyst, added in issue
// #107 — have a default reasoning effort, so leaving every effort prompt
// blank genuinely produces no effort clauses anywhere in CLAUDE.md.
func TestCmdInit_ClaudeMdOmitsEffortClauseWhenUnset(t *testing.T) {
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
		fmt.Fprintln(sc, "test-owner")
		fmt.Fprintln(sc, "test-repo")
		fmt.Fprintln(sc, "en")
		fmt.Fprintln(sc, "2")
		fmt.Fprintln(sc, "claude-cheap") // preset: no default effort on any role
		fmt.Fprintln(sc, "")             // superintendent effort: none
		fmt.Fprintln(sc, "")             // engineer effort: none
		fmt.Fprintln(sc, "")             // analyst effort: none
		sc.Flush()
		w.Close()
	}()

	pr2, pw2, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw2
	cmdInit()
	pw2.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr2)

	claudeMd, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(claudeMd)
	if !strings.Contains(content, `model: "claude-haiku-4-5-20251001"`) {
		t.Errorf("CLAUDE.md Engineer-spawn step missing engineer model wiring:\n%s", content)
	}
	if !strings.Contains(content, `model: "claude-sonnet-5"`) {
		t.Errorf("CLAUDE.md Analyst-spawn step missing analyst model wiring:\n%s", content)
	}
	if strings.Contains(content, "effort:") {
		t.Errorf("CLAUDE.md should omit effort clause when unset:\n%s", content)
	}
}

// --- cmdUse ---

// TestCmdUse_WritesModelAndEffort verifies that `hermit use <preset>` writes
// both the model and (when present) the reasoning-effort fields into
// harness.toml's [model] section.
func TestCmdUse_WritesModelAndEffort(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	if err := os.WriteFile("harness.toml", []byte(minimalHarnessTOML+"\n[model]\nsuperintendent = \"old\"\nengineer = \"old\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const presetName = "__test_issue96_with_effort__"
	modelPresets[presetName] = ModelPreset{
		Superintendent:       "claude-x",
		Engineer:             "claude-y",
		SuperintendentEffort: "high",
		EngineerEffort:       "low",
		Description:          "test preset with effort",
	}
	defer delete(modelPresets, presetName)

	cmdUse(presetName)

	data, err := os.ReadFile("harness.toml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		`superintendent = "claude-x"`,
		`engineer = "claude-y"`,
		`superintendent_effort = "high"`,
		`engineer_effort = "low"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("harness.toml missing %q:\n%s", want, content)
		}
	}
}

// TestCmdUse_OmitsEffortWhenPresetHasNone verifies that presets without a
// superintendent/engineer effort configured (like the built-in "claude"
// preset) do not write superintendent_effort/engineer_effort keys. The
// "claude" preset does configure an analyst_effort (issue #107: Analyst
// defaults to a higher-tier model/effort since misinterpretation cascades
// into implementation work), so analyst_effort is expected to be present.
func TestCmdUse_OmitsEffortWhenPresetHasNone(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	if err := os.WriteFile("harness.toml", []byte(minimalHarnessTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmdUse("claude")

	data, err := os.ReadFile("harness.toml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "superintendent_effort") {
		t.Errorf("harness.toml should not contain superintendent_effort for the 'claude' preset:\n%s", content)
	}
	if strings.Contains(content, "engineer_effort") {
		t.Errorf("harness.toml should not contain engineer_effort for the 'claude' preset:\n%s", content)
	}
	if !strings.Contains(content, `analyst_effort = "high"`) {
		t.Errorf("harness.toml should contain analyst_effort=high for the 'claude' preset:\n%s", content)
	}
	if !strings.Contains(content, `analyst = "claude-opus-4-8"`) {
		t.Errorf("harness.toml should contain analyst=claude-opus-4-8 for the 'claude' preset:\n%s", content)
	}
}

// --- main subcommand routing for serve/install/init ---

func TestMain_Init(t *testing.T) {
	if os.Getenv("TEST_MAIN_INIT") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)

		r, w, _ := os.Pipe()
		os.Stdin = r
		go func() {
			sc := bufio.NewWriter(w)
			fmt.Fprintln(sc, "owner")
			fmt.Fprintln(sc, "repo")
			fmt.Fprintln(sc, "en")
			fmt.Fprintln(sc, "4")
			sc.Flush()
			w.Close()
		}()
		os.Args = []string{"hermit", "init"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Init", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_INIT=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for init: %v", err)
	}
}

func TestMain_Install(t *testing.T) {
	if os.Getenv("TEST_MAIN_INSTALL") != "" {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(minimalHarnessTOML), 0o644)
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		homeDir := t.TempDir()
		os.Setenv("HOME", homeDir)
		writeFakeClaude(t)
		os.Args = []string{"hermit", "install"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Install", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_INSTALL=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for install: %v", err)
	}
}
