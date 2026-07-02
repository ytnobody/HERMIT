package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- loadConfig ---

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
[agent]
max_engineers = 2
language = "en"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.GitHub.Owner != "owner" || cfg.GitHub.Repo != "repo" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.Agent.MaxEngineers != 2 || cfg.Agent.Language != "en" {
		t.Errorf("unexpected agent config: %+v", cfg.Agent)
	}
}

func TestLoadConfig_MultiRepos(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "myorg"
repo  = "primary"

[[repos]]
owner = "myorg"
repo  = "frontend"
label = "hermit"

[[repos]]
owner = "myorg"
repo  = "backend"
label = "hermit"

[agent]
max_engineers = 4
language = "en"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.GitHub.Owner != "myorg" || cfg.GitHub.Repo != "primary" {
		t.Errorf("unexpected primary github config: %+v", cfg.GitHub)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].Owner != "myorg" || cfg.Repos[0].Repo != "frontend" || cfg.Repos[0].Label != "hermit" {
		t.Errorf("unexpected repos[0]: %+v", cfg.Repos[0])
	}
	if cfg.Repos[1].Owner != "myorg" || cfg.Repos[1].Repo != "backend" || cfg.Repos[1].Label != "hermit" {
		t.Errorf("unexpected repos[1]: %+v", cfg.Repos[1])
	}
}

func TestLoadConfig_SingleRepo_NoRepos(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "solo"
repo  = "single"
[agent]
max_engineers = 1
language = "en"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if len(cfg.Repos) != 0 {
		t.Errorf("expected no repos in single-repo mode, got %d", len(cfg.Repos))
	}
}

func TestLoadConfig_LoopIntervalDefault(t *testing.T) {
	dir := t.TempDir()
	// harness.toml without loop_interval → should default to 270
	content := `[github]
owner = "owner"
repo  = "repo"
[agent]
max_engineers = 2
language = "en"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Agent.LoopInterval != 270 {
		t.Errorf("expected LoopInterval default 270, got %d", cfg.Agent.LoopInterval)
	}
}

func TestLoadConfig_LoopIntervalCustom(t *testing.T) {
	dir := t.TempDir()
	// harness.toml with explicit loop_interval
	content := `[github]
owner = "owner"
repo  = "repo"
[agent]
max_engineers = 2
language = "en"
loop_interval = 60
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Agent.LoopInterval != 60 {
		t.Errorf("expected LoopInterval 60, got %d", cfg.Agent.LoopInterval)
	}
}

func TestLoadConfig_Missing(t *testing.T) {
	if os.Getenv("TEST_LOADCONFIG_MISSING") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		loadConfig() // should call fatal → os.Exit(1)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLoadConfig_Missing", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_LOADCONFIG_MISSING=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return // expected: fatal exits with non-zero
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

func TestLoadConfig_BadTOML(t *testing.T) {
	if os.Getenv("TEST_LOADCONFIG_BAD") != "" {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "harness.toml"), []byte("not valid toml :::"), 0o644)
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		loadConfig()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLoadConfig_BadTOML", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_LOADCONFIG_BAD=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// --- githubToken ---

func TestGithubToken_EnvSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "mytoken")
	tok := githubToken()
	if tok != "mytoken" {
		t.Errorf("expected 'mytoken', got %q", tok)
	}
}

func TestGithubToken_FallbackWarns(t *testing.T) {
	// Remove GITHUB_TOKEN; use a PATH with no 'gh' so exec.Command fails.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // empty PATH — 'gh' not found

	var buf bytes.Buffer
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	tok := githubToken()

	w.Close()
	os.Stderr = origStderr
	buf.ReadFrom(r)

	if tok != "" {
		t.Errorf("expected empty token fallback, got %q", tok)
	}
	if !strings.Contains(buf.String(), "warn:") {
		t.Errorf("expected warn message on stderr, got %q", buf.String())
	}
}

// --- pause / resume / status ---

func TestCmdPauseResumeStatus(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Initially running
	var buf bytes.Buffer
	origOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmdStatus()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "running") {
		t.Errorf("expected 'running', got %q", buf.String())
	}

	// Pause
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdPause()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if _, err := os.Stat(pauseFile); os.IsNotExist(err) {
		t.Error(".hermit-paused should exist after pause")
	}

	// Status should be paused
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdStatus()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "paused") {
		t.Errorf("expected 'paused', got %q", buf.String())
	}

	// Resume
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdResume()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if _, err := os.Stat(pauseFile); !os.IsNotExist(err) {
		t.Error(".hermit-paused should be removed after resume")
	}

	// Resume again (file already gone) — should not crash
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdResume()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "already running") {
		t.Errorf("expected already-running message, got %q", buf.String())
	}
}

func TestCmdQuitStatus(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Initially running
	var buf bytes.Buffer
	origOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmdStatus()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "running") {
		t.Errorf("expected 'running', got %q", buf.String())
	}

	// Quit
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdQuit()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if _, err := os.Stat(quitFile); os.IsNotExist(err) {
		t.Error(".hermit-quit should exist after quit")
	}
	if !strings.Contains(buf.String(), "quit") {
		t.Errorf("expected quit confirmation message, got %q", buf.String())
	}

	// Status should report quit requested, taking priority over pause
	if err := os.WriteFile(pauseFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	r, w, _ = os.Pipe()
	os.Stdout = w
	cmdStatus()
	w.Close()
	os.Stdout = origOut
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "quit requested") {
		t.Errorf("expected 'quit requested' status even with pause file present, got %q", buf.String())
	}
}

func TestCmdPause_WriteError(t *testing.T) {
	if os.Getenv("TEST_PAUSE_ERR") != "" {
		// Change to a read-only dir to force write failure
		dir := t.TempDir()
		os.Chmod(dir, 0o555)
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		cmdPause()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdPause_WriteError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_PAUSE_ERR=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	// On some systems (e.g. running as root) the write may succeed — acceptable.
	t.Log("pause write error test inconclusive (may run as root)")
}

// --- prompt / promptDefault ---

func TestPrompt(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("  hello world  \n"))
	got := prompt(sc, "enter: ")
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestPromptDefault_NonEmpty(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("custom\n"))
	got := promptDefault(sc, "enter: ", "default")
	if got != "custom" {
		t.Errorf("expected 'custom', got %q", got)
	}
}

func TestPromptDefault_Empty(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("\n"))
	got := promptDefault(sc, "enter: ", "default")
	if got != "default" {
		t.Errorf("expected 'default', got %q", got)
	}
}

// --- writeClaudeSettings ---

func TestWriteClaudeSettings(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	if err := writeClaudeSettings(); err != nil {
		t.Fatalf("writeClaudeSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	if !strings.Contains(string(data), "Bash(*)") {
		t.Errorf("settings.json missing Bash(*): %s", data)
	}
}

// --- writeTemplate ---

func TestWriteTemplate_Valid(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	type data struct {
		Owner               string
		Repo                string
		Language            string
		MaxEngineers        int
		SuperintendentModel string
		EngineerModel       string
	}
	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data{
		Owner: "owner", Repo: "repo", Language: "ja", MaxEngineers: 4,
		SuperintendentModel: "claude-sonnet-4-5", EngineerModel: "claude-haiku-4-5",
	})
	content, err := os.ReadFile(filepath.Join(dir, "harness.toml"))
	if err != nil {
		t.Fatalf("harness.toml not created: %v", err)
	}
	if !strings.Contains(string(content), "owner") {
		t.Errorf("harness.toml missing owner: %s", content)
	}
}

func TestWriteTemplate_MissingTemplate(t *testing.T) {
	if os.Getenv("TEST_TEMPLATE_MISSING") != "" {
		writeTemplate("templates/nonexistent.tmpl", "out.txt", nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestWriteTemplate_MissingTemplate", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_TEMPLATE_MISSING=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

// --- writeIssueTemplate ---

func TestWriteIssueTemplate_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	if err := writeIssueTemplate(); err != nil {
		t.Fatalf("writeIssueTemplate: %v", err)
	}

	outPath := filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "hermit-task.md")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("hermit-task.md not created: %v", err)
	}
	content := string(data)
	for _, section := range []string{"Summary", "Background", "Acceptance Criteria", "Technical Notes", "Out of Scope"} {
		if !strings.Contains(content, section) {
			t.Errorf("hermit-task.md missing section %q", section)
		}
	}
	if !strings.Contains(content, "HERMIT Task") {
		t.Errorf("hermit-task.md missing HERMIT Task front matter")
	}
}

func TestWriteIssueTemplate_DirectoryAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Pre-create the directory to ensure MkdirAll handles existing dirs gracefully.
	if err := os.MkdirAll(filepath.Join(dir, ".github", "ISSUE_TEMPLATE"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := writeIssueTemplate(); err != nil {
		t.Fatalf("writeIssueTemplate with existing dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "hermit-task.md")); err != nil {
		t.Error("hermit-task.md not created when directory pre-exists")
	}
}

// --- fatal ---

func TestFatal(t *testing.T) {
	if os.Getenv("TEST_FATAL") != "" {
		fatal("test error message")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestFatal", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_FATAL=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected fatal to exit with non-zero status, got: %v", err)
}

// --- usage ---

func TestUsage(t *testing.T) {
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	usage()
	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "Usage") {
		t.Errorf("usage output missing 'Usage': %q", buf.String())
	}
}

// --- main ---

func TestMain_NoArgs(t *testing.T) {
	if os.Getenv("TEST_MAIN_NOARGS") != "" {
		os.Args = []string{"hermit"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_NoArgs", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_NOARGS=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit for no-args main, got: %v", err)
}

func TestMain_UnknownSubcmd(t *testing.T) {
	if os.Getenv("TEST_MAIN_UNKNOWN") != "" {
		os.Args = []string{"hermit", "unknown-subcommand"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_UnknownSubcmd", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_UNKNOWN=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit for unknown subcommand, got: %v", err)
}

func TestMain_Pause(t *testing.T) {
	if os.Getenv("TEST_MAIN_PAUSE") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		os.Args = []string{"hermit", "pause"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Pause", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_PAUSE=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for pause: %v", err)
	}
}

func TestMain_Resume(t *testing.T) {
	if os.Getenv("TEST_MAIN_RESUME") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		os.Args = []string{"hermit", "resume"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Resume", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_RESUME=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for resume: %v", err)
	}
}

func TestMain_Status(t *testing.T) {
	if os.Getenv("TEST_MAIN_STATUS") != "" {
		dir := t.TempDir()
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		os.Args = []string{"hermit", "status"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Status", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_STATUS=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for status: %v", err)
	}
}
