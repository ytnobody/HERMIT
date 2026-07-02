package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ytnobody/hermit/internal/risk"
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

// TestLoadConfig_ModelEffort verifies that superintendent_effort/engineer_effort
// under [model] are parsed into Config.Model.
func TestLoadConfig_ModelEffort(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
[model]
superintendent = "claude-sonnet-4-5"
engineer       = "claude-haiku-4-5"
superintendent_effort = "high"
engineer_effort       = "medium"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Model.Superintendent != "claude-sonnet-4-5" || cfg.Model.Engineer != "claude-haiku-4-5" {
		t.Errorf("unexpected model config: %+v", cfg.Model)
	}
	if cfg.Model.SuperintendentEffort != "high" {
		t.Errorf("expected superintendent_effort=high, got %q", cfg.Model.SuperintendentEffort)
	}
	if cfg.Model.EngineerEffort != "medium" {
		t.Errorf("expected engineer_effort=medium, got %q", cfg.Model.EngineerEffort)
	}
}

// TestLoadConfig_ModelEffort_OmittedDefaultsEmpty verifies that omitting the
// effort keys leaves them as the empty string (no effort specified).
func TestLoadConfig_ModelEffort_OmittedDefaultsEmpty(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
[model]
superintendent = "claude-sonnet-4-5"
engineer       = "claude-sonnet-4-5"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Model.SuperintendentEffort != "" || cfg.Model.EngineerEffort != "" {
		t.Errorf("expected empty effort fields, got: %+v", cfg.Model)
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

// --- [risk] section / resolveRiskConfig ---

// TestLoadConfig_RiskSection_Custom verifies that harness.toml's [risk]
// section is parsed into cfg.Risk and that resolveRiskConfig applies it on
// top of the hardcoded default policy.
func TestLoadConfig_RiskSection_Custom(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"

[risk]
high_paths            = ["src/", "Cargo.toml"]
medium_paths          = ["lib/"]
high_file_threshold   = 15
high_line_threshold   = 300
medium_file_threshold = 5
medium_line_threshold = 100
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if len(cfg.Risk.HighPaths) != 2 || cfg.Risk.HighPaths[0] != "src/" || cfg.Risk.HighPaths[1] != "Cargo.toml" {
		t.Errorf("unexpected Risk.HighPaths: %+v", cfg.Risk.HighPaths)
	}
	if len(cfg.Risk.MediumPaths) != 1 || cfg.Risk.MediumPaths[0] != "lib/" {
		t.Errorf("unexpected Risk.MediumPaths: %+v", cfg.Risk.MediumPaths)
	}
	if cfg.Risk.HighFileThreshold != 15 || cfg.Risk.HighLineThreshold != 300 {
		t.Errorf("unexpected Risk high thresholds: %+v", cfg.Risk)
	}
	if cfg.Risk.MediumFileThreshold != 5 || cfg.Risk.MediumLineThreshold != 100 {
		t.Errorf("unexpected Risk medium thresholds: %+v", cfg.Risk)
	}

	def, repoCfgs := resolveRiskConfig(cfg)
	if repoCfgs != nil {
		t.Errorf("expected no per-repo overrides in single-repo mode, got %+v", repoCfgs)
	}
	if def.HighFileThreshold != 15 || def.HighLineThreshold != 300 {
		t.Errorf("resolveRiskConfig() default high thresholds = %+v, want overridden values", def)
	}
	if def.MediumFileThreshold != 5 || def.MediumLineThreshold != 100 {
		t.Errorf("resolveRiskConfig() default medium thresholds = %+v, want overridden values", def)
	}
	if len(def.HighPaths) != 2 || def.HighPaths[0] != "src/" {
		t.Errorf("resolveRiskConfig() default HighPaths = %+v, want overridden value", def.HighPaths)
	}
}

// TestLoadConfig_RiskSection_Omitted verifies that when [risk] is entirely
// absent from harness.toml, resolveRiskConfig falls back to the exact
// hardcoded legacy defaults (risk.DefaultConfig()) — i.e. omitting the
// section is fully backward compatible.
func TestLoadConfig_RiskSection_Omitted(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	def, repoCfgs := resolveRiskConfig(cfg)
	if repoCfgs != nil {
		t.Errorf("expected no per-repo overrides, got %+v", repoCfgs)
	}
	want := risk.DefaultConfig()
	if def.HighFileThreshold != want.HighFileThreshold || def.HighLineThreshold != want.HighLineThreshold {
		t.Errorf("default high thresholds = %+v, want legacy defaults %+v", def, want)
	}
	if def.MediumFileThreshold != want.MediumFileThreshold || def.MediumLineThreshold != want.MediumLineThreshold {
		t.Errorf("default medium thresholds = %+v, want legacy defaults %+v", def, want)
	}
	if len(def.HighPaths) != len(want.HighPaths) {
		t.Fatalf("HighPaths = %+v, want %+v", def.HighPaths, want.HighPaths)
	}
	for i := range want.HighPaths {
		if def.HighPaths[i] != want.HighPaths[i] {
			t.Errorf("HighPaths[%d] = %q, want %q", i, def.HighPaths[i], want.HighPaths[i])
		}
	}
	if len(def.MediumPaths) != len(want.MediumPaths) || def.MediumPaths[0] != want.MediumPaths[0] {
		t.Errorf("MediumPaths = %+v, want %+v", def.MediumPaths, want.MediumPaths)
	}
}

// TestLoadConfig_RiskSection_PartialOverride verifies that specifying only
// some [risk] fields leaves the rest at the hardcoded default (partial
// override / backward-compatible merge behavior).
func TestLoadConfig_RiskSection_PartialOverride(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"

[risk]
high_file_threshold = 3
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	def, _ := resolveRiskConfig(cfg)
	want := risk.DefaultConfig()
	if def.HighFileThreshold != 3 {
		t.Errorf("HighFileThreshold = %d, want 3", def.HighFileThreshold)
	}
	if def.HighLineThreshold != want.HighLineThreshold {
		t.Errorf("HighLineThreshold = %d, want default %d", def.HighLineThreshold, want.HighLineThreshold)
	}
	if def.MediumFileThreshold != want.MediumFileThreshold {
		t.Errorf("MediumFileThreshold = %d, want default %d", def.MediumFileThreshold, want.MediumFileThreshold)
	}
	if len(def.HighPaths) != len(want.HighPaths) {
		t.Errorf("HighPaths = %+v, want default %+v", def.HighPaths, want.HighPaths)
	}
}

// TestLoadConfig_RiskSection_PerRepoOverride verifies that in multi-repo
// mode, a [[repos]] entry's own [repos.risk] sub-table overrides the
// top-level [risk] section for that repo only, while other repos and the
// unqualified default fall back to the global config.
func TestLoadConfig_RiskSection_PerRepoOverride(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "myorg"
repo  = "primary"

[risk]
high_file_threshold = 15

[[repos]]
owner = "myorg"
repo  = "frontend"
label = "hermit"

[repos.risk]
high_file_threshold = 3
high_paths          = ["src/"]

[[repos]]
owner = "myorg"
repo  = "backend"
label = "hermit"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	def, repoCfgs := resolveRiskConfig(cfg)

	if def.HighFileThreshold != 15 {
		t.Errorf("global default HighFileThreshold = %d, want 15", def.HighFileThreshold)
	}

	frontend, ok := repoCfgs["myorg/frontend"]
	if !ok {
		t.Fatalf("expected an override for myorg/frontend, got %+v", repoCfgs)
	}
	if frontend.HighFileThreshold != 3 {
		t.Errorf("frontend HighFileThreshold = %d, want 3 (repo override)", frontend.HighFileThreshold)
	}
	if len(frontend.HighPaths) != 1 || frontend.HighPaths[0] != "src/" {
		t.Errorf("frontend HighPaths = %+v, want [src/]", frontend.HighPaths)
	}
	// Fields not overridden at the repo level should fall back to the
	// resolved global config, not the hardcoded legacy default.
	if frontend.HighLineThreshold != def.HighLineThreshold {
		t.Errorf("frontend HighLineThreshold = %d, want inherited global value %d", frontend.HighLineThreshold, def.HighLineThreshold)
	}

	backend, ok := repoCfgs["myorg/backend"]
	if !ok {
		t.Fatalf("expected an entry for myorg/backend, got %+v", repoCfgs)
	}
	if backend.HighFileThreshold != 15 {
		t.Errorf("backend HighFileThreshold = %d, want inherited global value 15 (no repo override)", backend.HighFileThreshold)
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
		Owner                string
		Repo                 string
		Language             string
		MaxEngineers         int
		SuperintendentModel  string
		EngineerModel        string
		SuperintendentEffort string
		EngineerEffort       string
	}
	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data{
		Owner: "owner", Repo: "repo", Language: "ja", MaxEngineers: 4,
		SuperintendentModel: "claude-sonnet-4-5", EngineerModel: "claude-haiku-4-5",
		SuperintendentEffort: "high", EngineerEffort: "medium",
	})
	content, err := os.ReadFile(filepath.Join(dir, "harness.toml"))
	if err != nil {
		t.Fatalf("harness.toml not created: %v", err)
	}
	if !strings.Contains(string(content), "owner") {
		t.Errorf("harness.toml missing owner: %s", content)
	}
	if !strings.Contains(string(content), `superintendent_effort = "high"`) {
		t.Errorf("harness.toml missing superintendent_effort: %s", content)
	}
	if !strings.Contains(string(content), `engineer_effort       = "medium"`) {
		t.Errorf("harness.toml missing engineer_effort: %s", content)
	}
}

// TestWriteTemplate_HarnessToml_EffortOmittedWhenEmpty verifies that the
// superintendent_effort/engineer_effort keys are omitted entirely (rather
// than written as empty strings) when no effort is configured.
func TestWriteTemplate_HarnessToml_EffortOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	type data struct {
		Owner                string
		Repo                 string
		Language             string
		MaxEngineers         int
		SuperintendentModel  string
		EngineerModel        string
		SuperintendentEffort string
		EngineerEffort       string
	}
	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data{
		Owner: "owner", Repo: "repo", Language: "ja", MaxEngineers: 4,
		SuperintendentModel: "claude-sonnet-4-5", EngineerModel: "claude-haiku-4-5",
	})
	content, err := os.ReadFile(filepath.Join(dir, "harness.toml"))
	if err != nil {
		t.Fatalf("harness.toml not created: %v", err)
	}
	if strings.Contains(string(content), "_effort") {
		t.Errorf("harness.toml should omit effort keys when unset: %s", content)
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

// --- modelPresets ---

// TestModelPresets_NoStaleModelIDs guards against regressing to retired
// Claude model IDs (e.g. "claude-sonnet-4-5") in the built-in presets. See
// https://github.com/ytnobody/HERMIT/issues/94.
func TestModelPresets_NoStaleModelIDs(t *testing.T) {
	staleIDs := []string{"claude-sonnet-4-5", "claude-haiku-4-5-latest"}

	for name, preset := range modelPresets {
		for _, stale := range staleIDs {
			if preset.Superintendent == stale {
				t.Errorf("preset %q: Superintendent uses stale model ID %q", name, stale)
			}
			if preset.Engineer == stale {
				t.Errorf("preset %q: Engineer uses stale model ID %q", name, stale)
			}
		}
	}
}

// TestModelPresets_ExpectedValues locks in the current model IDs for the
// built-in presets so future changes are intentional and reviewed.
func TestModelPresets_ExpectedValues(t *testing.T) {
	want := map[string]ModelPreset{
		"claude": {
			Superintendent: "claude-sonnet-5",
			Engineer:       "claude-sonnet-5",
		},
		"claude-cheap": {
			Superintendent: "claude-sonnet-5",
			Engineer:       "claude-haiku-4-5-20251001",
		},
	}

	for name, wantPreset := range want {
		gotPreset, ok := modelPresets[name]
		if !ok {
			t.Fatalf("preset %q not found in modelPresets", name)
		}
		if gotPreset.Superintendent != wantPreset.Superintendent {
			t.Errorf("preset %q: Superintendent = %q, want %q", name, gotPreset.Superintendent, wantPreset.Superintendent)
		}
		if gotPreset.Engineer != wantPreset.Engineer {
			t.Errorf("preset %q: Engineer = %q, want %q", name, gotPreset.Engineer, wantPreset.Engineer)
		}
	}
}
