package main

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/git"
	"github.com/ytnobody/hermit/internal/mcp"
	"github.com/ytnobody/hermit/internal/permissions"
)

//go:embed templates/* templates/commands/*
var templateFS embed.FS

// RepoConfig holds the owner, repo, and optional label filter for a single
// repository entry in the [[repos]] array.
type RepoConfig struct {
	Owner string `toml:"owner"`
	Repo  string `toml:"repo"`
	Label string `toml:"label"`
}

type Config struct {
	GitHub struct {
		Owner              string `toml:"owner"`
		Repo               string `toml:"repo"`
		RateLimitThreshold int    `toml:"rate_limit_threshold"`
		DefaultBranch      string `toml:"default_branch"`
	} `toml:"github"`
	// Repos overrides the single [github] section when present.
	Repos []RepoConfig `toml:"repos"`
	Agent struct {
		MaxEngineers    int    `toml:"max_engineers"`
		Language        string `toml:"language"`
		BranchPrefix    string `toml:"branch_prefix"`
		LoopInterval    int    `toml:"loop_interval"`
		TriggerComment  string `toml:"trigger_comment"`
	} `toml:"agent"`
	Model struct {
		Superintendent string `toml:"superintendent"`
		Engineer       string `toml:"engineer"`
		// SuperintendentEffort/EngineerEffort configure the Claude Agent
		// tool's reasoning effort (e.g. low/medium/high/xhigh/max) for the
		// Superintendent and Engineer roles respectively. Both are optional;
		// when empty, no effort is specified and the caller's default applies.
		SuperintendentEffort string `toml:"superintendent_effort"`
		EngineerEffort       string `toml:"engineer_effort"`
	} `toml:"model"`
	Notification struct {
		WebhookURL string `toml:"webhook_url"`
		Type       string `toml:"type"`
	} `toml:"notification"`
}

// ModelPreset defines superintendent/engineer model combinations.
// SuperintendentEffort/EngineerEffort are optional reasoning-effort defaults
// (low/medium/high/xhigh/max) applied for each role; an empty value means no
// effort is specified and the caller's default applies.
type ModelPreset struct {
	Superintendent       string
	Engineer             string
	SuperintendentEffort string
	EngineerEffort       string
	Description          string
}

var modelPresets = map[string]ModelPreset{
	"claude": {
		Superintendent: "claude-sonnet-5",
		Engineer:       "claude-sonnet-5",
		Description:    "Sonnet for both Superintendent and Engineer (balanced)",
	},
	"claude-cheap": {
		Superintendent: "claude-sonnet-5",
		Engineer:       "claude-haiku-4-5-20251001",
		Description:    "Sonnet for Superintendent, Haiku for Engineers (cost-optimized)",
	},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "install":
		cmdInstall()
	case "init":
		cmdInit()
	case "pause":
		cmdPause()
	case "resume":
		cmdResume()
	case "quit":
		cmdQuit()
	case "status":
		cmdStatus()
	case "use":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: hermit use <preset>")
			fmt.Fprintln(os.Stderr, "Available presets:")
			for name, p := range modelPresets {
				fmt.Fprintf(os.Stderr, "  %-20s %s\n", name, p.Description)
			}
			os.Exit(1)
		}
		cmdUse(os.Args[2])
	case "version":
		cmdVersion()
	case "upgrade":
		cmdUpgrade()
	case "cleanup":
		cmdCleanup()
	case "doctor":
		cmdDoctor()
	case "dry-run":
		cmdDryRun()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hermit <serve|install|init|pause|resume|quit|status|use|version|upgrade|cleanup|doctor|dry-run>")
}

const pauseFile = ".hermit-paused"

// quitFile is a terminal flag file: unlike pauseFile (which is meant to be
// resumed via `hermit resume`), quitFile signals that the Superintendent's
// `/loop` should stop entirely — the loop must not call ScheduleWakeup again
// once this file is present. There is no `hermit unquit`; starting a fresh
// `/hermit` run again is the intended way to resume autonomous operation
// after a quit.
const quitFile = ".hermit-quit"

func cmdPause() {
	f, err := os.Create(pauseFile)
	if err != nil {
		fatal(err.Error())
	}
	f.Close()
	fmt.Println("⏸  Autonomous operation paused. To resume: hermit resume")
}

func cmdResume() {
	if err := os.Remove(pauseFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Autonomous operation is already running.")
			return
		}
		fatal(err.Error())
	}
	fmt.Println("▶  Autonomous operation resumed.")
}

func cmdQuit() {
	f, err := os.Create(quitFile)
	if err != nil {
		fatal(err.Error())
	}
	f.Close()
	fmt.Println("⏹  Autonomous operation quit requested. The Superintendent loop will stop at the start of its next cycle and will not reschedule itself.")
}

func cmdStatus() {
	if _, err := os.Stat(quitFile); err == nil {
		fmt.Println("⏹  quit requested (loop will stop)")
		return
	}
	if _, err := os.Stat(pauseFile); err == nil {
		fmt.Println("⏸  paused")
	} else {
		fmt.Println("▶  running")
	}
}

func cmdUse(presetName string) {
	preset, ok := modelPresets[presetName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown preset %q\n", presetName)
		fmt.Fprintln(os.Stderr, "Available presets:")
		for name, p := range modelPresets {
			fmt.Fprintf(os.Stderr, "  %-20s %s\n", name, p.Description)
		}
		os.Exit(1)
	}

	const harnessFile = "harness.toml"
	data, err := os.ReadFile(harnessFile)
	if err != nil {
		fatal("failed to read harness.toml: " + err.Error())
	}

	var cfg map[string]any
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		fatal("failed to parse harness.toml: " + err.Error())
	}

	modelSection, _ := cfg["model"].(map[string]any)
	if modelSection == nil {
		modelSection = make(map[string]any)
	}
	modelSection["superintendent"] = preset.Superintendent
	modelSection["engineer"] = preset.Engineer
	if preset.SuperintendentEffort != "" {
		modelSection["superintendent_effort"] = preset.SuperintendentEffort
	}
	if preset.EngineerEffort != "" {
		modelSection["engineer_effort"] = preset.EngineerEffort
	}
	cfg["model"] = modelSection

	f, err := os.Create(harnessFile)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		fatal("failed to write harness.toml: " + err.Error())
	}

	fmt.Printf("✓ preset %q applied\n", presetName)
	fmt.Printf("  superintendent: %s\n", preset.Superintendent)
	fmt.Printf("  engineer:       %s\n", preset.Engineer)
	if preset.SuperintendentEffort != "" {
		fmt.Printf("  superintendent_effort: %s\n", preset.SuperintendentEffort)
	}
	if preset.EngineerEffort != "" {
		fmt.Printf("  engineer_effort:       %s\n", preset.EngineerEffort)
	}
}

func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: GITHUB_TOKEN is not set and gh auth token also failed. GitHub API calls will result in authentication errors.")
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveBranchPrefix determines the branch prefix to use for worktrees.
// Priority:
//  1. cfg.Agent.BranchPrefix if set in harness.toml
//  2. "hermit/<gh_login>" if gh CLI is available
//  3. "hermit" as fallback (legacy format)
func resolveBranchPrefix(cfg Config) string {
	if cfg.Agent.BranchPrefix != "" {
		return cfg.Agent.BranchPrefix
	}
	login, err := gh.GetGitHubLogin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: could not retrieve GitHub login name. Using fallback branch prefix \"hermit\":", err)
		return "hermit"
	}
	return "hermit/" + login
}

func cmdServe() {
	// HERMIT_PROJECT_DIR explicitly pins the project root regardless of cwd.
	// cmdInstall writes this into the MCP server env so the correct harness.toml
	// is always used, even when Claude Code does not honour the cwd field.
	if projectDir := os.Getenv("HERMIT_PROJECT_DIR"); projectDir != "" {
		if err := os.Chdir(projectDir); err != nil {
			log.Printf("warn: HERMIT_PROJECT_DIR=%q: %v; falling back to cwd resolution", projectDir, err)
		}
	} else if _, err := os.Stat("harness.toml"); os.IsNotExist(err) {
		// No explicit project dir set and harness.toml not in cwd: fall back to
		// the binary's real location — Claude Code may not honour the cwd field
		// when spawning the MCP server.
		if execPath, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
				execPath = resolved
			}
			_ = os.Chdir(filepath.Dir(execPath))
		}
	}

	rootDir, _ := os.Getwd()

	// Detect legacy resources and warn before starting the MCP server.
	if branches := git.DetectLegacyBranches(); len(branches) > 0 {
		log.Printf("WARNING: %d legacy branch(es) detected: %v", len(branches), branches)
		log.Printf("         Run `hermit cleanup` to remove them.")
	}
	if zombies := git.DetectZombieWorktrees(); len(zombies) > 0 {
		log.Printf("WARNING: %d zombie worktree(s) detected: %v", len(zombies), zombies)
		log.Printf("         Run `hermit cleanup` to remove them.")
	}

	cfg := loadConfig()
	token := githubToken()
	client := gh.NewClient(token, cfg.GitHub.Owner, cfg.GitHub.Repo)
	prefix := resolveBranchPrefix(cfg)

	// Convert []RepoConfig → []gh.RepoConfig for the MCP layer.
	var repos []gh.RepoConfig
	for _, r := range cfg.Repos {
		repos = append(repos, gh.RepoConfig{Owner: r.Owner, Repo: r.Repo, Label: r.Label})
	}

	if err := mcp.Serve(client, cfg.GitHub.RateLimitThreshold, rootDir, prefix, cfg.Agent.LoopInterval, cfg.Notification.WebhookURL, cfg.Notification.Type, repos, cfg.Agent.TriggerComment); err != nil {
		fatal(err.Error())
	}
}

func cmdCleanup() {
	fmt.Println("=== HERMIT Cleanup ===")

	branches := git.DetectLegacyBranches()
	if len(branches) == 0 {
		fmt.Println("Legacy branches: none")
	} else {
		fmt.Printf("Removing legacy branches: %v\n", branches)
		if err := git.CleanupLegacyBranches(branches); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			fmt.Printf("  Removed %d legacy branch(es).\n", len(branches))
		}
	}

	zombies := git.DetectZombieWorktrees()
	if len(zombies) == 0 {
		fmt.Println("Zombie worktrees: none")
	} else {
		fmt.Printf("Removing zombie worktrees: %v\n", zombies)
		cleaned := 0
		for _, path := range zombies {
			out, err := exec.Command("git", "worktree", "remove", "--force", path).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to remove worktree %s: %s\n", path, strings.TrimSpace(string(out)))
			} else {
				cleaned++
			}
		}
		fmt.Printf("  Removed %d zombie worktree(s).\n", cleaned)
	}

	fmt.Println("Cleanup complete.")
}

func cmdInstall() {
	execPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	// cwd is where harness.toml lives, not the binary's directory.
	cwd, _ := os.Getwd()
	if _, err := os.Stat("harness.toml"); os.IsNotExist(err) {
		fatal("harness.toml not found. Please run `hermit install` from the project root.")
	}

	// Register hermit as a project-local MCP server through the Claude Code
	// CLI itself. Claude Code does NOT read MCP server definitions from
	// ~/.claude/settings.json — only from ~/.claude.json (managed via
	// `claude mcp add`) or a project .mcp.json file, so registration must go
	// through the officially supported `claude mcp add` command rather than
	// hand-writing Claude Code's internal config format.
	addArgs := []string{
		"mcp", "add", "hermit",
		"-s", "local",
		"-e", "GITHUB_TOKEN=${GITHUB_TOKEN}",
		"-e", "HERMIT_PROJECT_DIR=" + cwd,
		"--", execPath, "serve",
	}
	if out, err := exec.Command("claude", addArgs...).CombinedOutput(); err != nil {
		fatal("failed to register MCP server via `claude mcp add`: " + err.Error() + "\n" + string(out))
	}
	fmt.Println("✓ HERMIT MCP server registered with Claude Code (run `claude mcp list` to verify)")

	// Install slash commands into the project's .claude/commands/ directory.
	commandsDir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not create %s: %v\n", commandsDir, err)
	} else {
		cmdFiles := []string{"hermit.md", "hermit-pause.md", "hermit-resume.md", "hermit-quit.md"}
		allOK := true
		for _, name := range cmdFiles {
			src := "templates/commands/" + name
			data, err := templateFS.ReadFile(src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not read embedded %s: %v\n", src, err)
				allOK = false
				continue
			}
			dst := filepath.Join(commandsDir, name)
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not write %s: %v\n", dst, err)
				allOK = false
			}
		}
		if allOK {
			fmt.Println("✓ Slash commands installed to", commandsDir)
			fmt.Println("  Tip: run `git add .claude/commands/` to commit hermit's slash commands to version control.")
		}
	}

	// Symlink binary to ~/.local/bin so `hermit` is available in PATH.
	localBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not create %s: %v\n", localBin, err)
	} else {
		linkPath := filepath.Join(localBin, "hermit")
		if linkPath != execPath {
			_ = os.Remove(linkPath)
			if err := os.Symlink(execPath, linkPath); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not symlink hermit to %s: %v\n", linkPath, err)
			} else {
				fmt.Println("✓ hermit symlinked to", linkPath)
			}
		}
	}

	fmt.Println("")
	fmt.Println("⚠  Restart Claude Code to enable the MCP tools.")
}

func cmdInit() {
	sc := bufio.NewScanner(os.Stdin)

	owner := prompt(sc, "GitHub owner (org or user): ")
	repo := prompt(sc, "GitHub repo: ")
	lang := promptDefault(sc, "Language [ja/en] (default: ja): ", "ja")
	maxEngStr := promptDefault(sc, "Max parallel Engineers (default: 4): ", "4")
	maxEng, _ := strconv.Atoi(maxEngStr)
	if maxEng <= 0 {
		maxEng = 4
	}

	fmt.Println("Available model presets:")
	for name, p := range modelPresets {
		fmt.Printf("  %-20s %s\n", name, p.Description)
	}
	presetName := promptDefault(sc, "Model preset (default: claude): ", "claude")
	preset, ok := modelPresets[presetName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown preset %q, using 'claude'\n", presetName)
		preset = modelPresets["claude"]
	}

	supEffort := promptDefault(sc, "Superintendent reasoning effort [low/medium/high/xhigh/max] (optional, default: none): ", preset.SuperintendentEffort)
	engEffort := promptDefault(sc, "Engineer reasoning effort [low/medium/high/xhigh/max] (optional, default: none): ", preset.EngineerEffort)

	type tmplData struct {
		Owner                string
		Repo                 string
		Language             string
		MaxEngineers         int
		SuperintendentModel  string
		EngineerModel        string
		SuperintendentEffort string
		EngineerEffort       string
	}
	data := tmplData{
		Owner:                owner,
		Repo:                 repo,
		Language:             lang,
		MaxEngineers:         maxEng,
		SuperintendentModel:  preset.Superintendent,
		EngineerModel:        preset.Engineer,
		SuperintendentEffort: supEffort,
		EngineerEffort:       engEffort,
	}

	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data)
	writeTemplate("templates/CLAUDE.md.tmpl", "CLAUDE.md", struct {
		MaxEngineers       int
		ProjectCodingRules string
		EngineerModel      string
		EngineerEffort     string
	}{
		MaxEngineers:       maxEng,
		ProjectCodingRules: "Describe your project-specific coding guidelines here.",
		EngineerModel:      preset.Engineer,
		EngineerEffort:     engEffort,
	})

	// Generate .github/ISSUE_TEMPLATE/hermit-task.md for Issue creation guidance.
	if err := writeIssueTemplate(); err != nil {
		fatal("failed to write .github/ISSUE_TEMPLATE/hermit-task.md: " + err.Error())
	}

	// Generate .claude/settings.json so Claude Code runs autonomously without
	// confirmation prompts during the hermit loop.
	if err := writeClaudeSettings(); err != nil {
		fatal("failed to write .claude/settings.json: " + err.Error())
	}

	fmt.Println("\n✓ harness.toml and CLAUDE.md generated.")
	fmt.Println("✓ .github/ISSUE_TEMPLATE/hermit-task.md generated.")
	fmt.Println("✓ .claude/settings.json generated (autonomous operation mode).")
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit the 'Coding Guidelines' section in CLAUDE.md")
	fmt.Println("  2. Set GITHUB_TOKEN and start `hermit serve`")
}

// writeClaudeSettings creates .claude/settings.json in the current directory
// with the comprehensive permission set required for autonomous hermit operation.
func writeClaudeSettings() error {
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".claude", "settings.json"), permissions.DefaultSettingsJSON(), 0o644)
}

// writeIssueTemplate creates .github/ISSUE_TEMPLATE/hermit-task.md in the
// current directory so users can create well-structured Issues for HERMIT.
func writeIssueTemplate() error {
	dir := filepath.Join(".github", "ISSUE_TEMPLATE")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content, err := templateFS.ReadFile("templates/hermit-task.md.tmpl")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "hermit-task.md"), content, 0o644)
}

func writeTemplate(tmplPath, outPath string, data any) {
	content, err := templateFS.ReadFile(tmplPath)
	if err != nil {
		fatal("template not found: " + tmplPath + ": " + err.Error())
	}
	t, err := template.New("").Option("missingkey=error").Parse(string(content))
	if err != nil {
		fatal("template parse error: " + err.Error())
	}
	f, err := os.Create(outPath)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	if err := t.Execute(f, data); err != nil {
		fatal(err.Error())
	}
}

func loadConfig() Config {
	var cfg Config
	if _, err := toml.DecodeFile("harness.toml", &cfg); err != nil {
		if os.IsNotExist(err) {
			fatal("harness.toml not found. Please run `hermit init` from the project root.")
		}
		fatal("failed to load harness.toml: " + err.Error())
	}
	if cfg.Agent.LoopInterval <= 0 {
		cfg.Agent.LoopInterval = 270
	}
	return cfg
}

func prompt(sc *bufio.Scanner, msg string) string {
	fmt.Print(msg)
	sc.Scan()
	return strings.TrimSpace(sc.Text())
}

func promptDefault(sc *bufio.Scanner, msg, def string) string {
	v := prompt(sc, msg)
	if v == "" {
		return def
	}
	return v
}

// fatalFunc is the function invoked by fatal(). Tests may replace it so that
// error paths can be exercised in-process without calling os.Exit.
var fatalFunc = func(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

func fatal(msg string) {
	fatalFunc(msg)
}
