package main

import (
	"bufio"
	"embed"
	"encoding/json"
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

//go:embed templates/*
var templateFS embed.FS

type Config struct {
	GitHub struct {
		Owner              string `toml:"owner"`
		Repo               string `toml:"repo"`
		RateLimitThreshold int    `toml:"rate_limit_threshold"`
	} `toml:"github"`
	Agent struct {
		MaxEngineers int    `toml:"max_engineers"`
		Language     string `toml:"language"`
		BranchPrefix string `toml:"branch_prefix"`
	} `toml:"agent"`
	Model struct {
		Superintendent string `toml:"superintendent"`
		Engineer       string `toml:"engineer"`
	} `toml:"model"`
}

// ModelPreset defines superintendent/engineer model combinations.
type ModelPreset struct {
	Superintendent string
	Engineer       string
	Description    string
}

var modelPresets = map[string]ModelPreset{
	"claude": {
		Superintendent: "claude-sonnet-4-5",
		Engineer:       "claude-sonnet-4-5",
		Description:    "Sonnet for both Superintendent and Engineer (balanced)",
	},
	"claude-cheap": {
		Superintendent: "claude-sonnet-4-5",
		Engineer:       "claude-haiku-4-5",
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
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hermit <serve|install|init|pause|resume|status|use|version|upgrade|cleanup>")
}

const pauseFile = ".hermit-paused"

func cmdPause() {
	f, err := os.Create(pauseFile)
	if err != nil {
		fatal(err.Error())
	}
	f.Close()
	fmt.Println("⏸  自動運転を一時停止しました。再開するには: hermit resume")
}

func cmdResume() {
	if err := os.Remove(pauseFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("自動運転はすでに動作中です。")
			return
		}
		fatal(err.Error())
	}
	fmt.Println("▶  自動運転を再開しました。")
}

func cmdStatus() {
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
	cfg["model"] = modelSection

	f, err := os.Create(harnessFile)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		fatal("failed to write harness.toml: " + err.Error())
	}

	fmt.Printf("✓ preset %q を適用しました\n", presetName)
	fmt.Printf("  superintendent: %s\n", preset.Superintendent)
	fmt.Printf("  engineer:       %s\n", preset.Engineer)
}

func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: GITHUB_TOKEN が未設定で gh auth token も失敗しました。GitHub API 呼び出しは認証エラーになります。")
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
		fmt.Fprintln(os.Stderr, "warn: GitHub ログイン名を取得できませんでした。フォールバックのブランチプレフィックス \"hermit\" を使用します:", err)
		return "hermit"
	}
	return "hermit/" + login
}

func cmdServe() {
	// Claude Code may not honour the cwd setting when spawning the MCP server.
	// Resolve the binary's real location and chdir there so harness.toml is
	// always reachable via a relative path regardless of the OS working dir.
	if execPath, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
		_ = os.Chdir(filepath.Dir(execPath))
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
	if err := mcp.Serve(client, cfg.GitHub.RateLimitThreshold, rootDir, prefix); err != nil {
		fatal(err.Error())
	}
}

func cmdCleanup() {
	fmt.Println("=== HERMIT Cleanup ===")

	branches := git.DetectLegacyBranches()
	if len(branches) == 0 {
		fmt.Println("レガシーブランチ: なし")
	} else {
		fmt.Printf("レガシーブランチを削除します: %v\n", branches)
		if err := git.CleanupLegacyBranches(branches); err != nil {
			fmt.Fprintf(os.Stderr, "警告: %v\n", err)
		} else {
			fmt.Printf("  %d 件のレガシーブランチを削除しました。\n", len(branches))
		}
	}

	zombies := git.DetectZombieWorktrees()
	if len(zombies) == 0 {
		fmt.Println("ゾンビワークツリー: なし")
	} else {
		fmt.Printf("ゾンビワークツリーを削除します: %v\n", zombies)
		cleaned := 0
		for _, path := range zombies {
			out, err := exec.Command("git", "worktree", "remove", "--force", path).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  警告: worktree %s の削除に失敗: %s\n", path, strings.TrimSpace(string(out)))
			} else {
				cleaned++
			}
		}
		fmt.Printf("  %d 件のゾンビワークツリーを削除しました。\n", cleaned)
	}

	fmt.Println("クリーンアップ完了。")
}

func cmdInstall() {
	execPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	// cwd is where harness.toml lives, not the binary's directory.
	cwd, _ := os.Getwd()
	if _, err := os.Stat("harness.toml"); os.IsNotExist(err) {
		fatal("harness.toml が見つかりません。プロジェクトルートで `hermit install` を実行してください。")
	}

	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		fatal(err.Error())
	}

	var settings map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			fatal("failed to parse settings.json: " + err.Error())
		}
	} else {
		settings = make(map[string]any)
	}

	mcpServers, _ := settings["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = make(map[string]any)
	}
	mcpServers["hermit"] = map[string]any{
		"command": execPath,
		"args":    []string{"serve"},
		"cwd":     cwd,
		"env": map[string]string{
			"GITHUB_TOKEN": "${GITHUB_TOKEN}",
		},
	}
	settings["mcpServers"] = mcpServers

	b, _ := json.MarshalIndent(settings, "", "  ") // map[string]any is always serialisable
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(settingsPath, append(b, '\n'), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Println("✓ HERMIT MCP server registered in", settingsPath)

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
	fmt.Println("⚠  Claude Code を再起動すると MCP ツールが有効になります。")
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

	type tmplData struct {
		Owner               string
		Repo                string
		Language            string
		MaxEngineers        int
		SuperintendentModel string
		EngineerModel       string
	}
	data := tmplData{
		Owner:               owner,
		Repo:                repo,
		Language:            lang,
		MaxEngineers:        maxEng,
		SuperintendentModel: preset.Superintendent,
		EngineerModel:       preset.Engineer,
	}

	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data)
	writeTemplate("templates/CLAUDE.md.tmpl", "CLAUDE.md", struct {
		MaxEngineers       int
		ProjectCodingRules string
	}{MaxEngineers: maxEng, ProjectCodingRules: "プロジェクト固有のコーディング規約をここに記述してください。"})

	// Generate .claude/settings.json so Claude Code runs autonomously without
	// confirmation prompts during the hermit loop.
	if err := writeClaudeSettings(); err != nil {
		fatal("failed to write .claude/settings.json: " + err.Error())
	}

	fmt.Println("\n✓ harness.toml と CLAUDE.md を生成しました。")
	fmt.Println("✓ .claude/settings.json を生成しました（自律実行モード）。")
	fmt.Println("次のステップ:")
	fmt.Println("  1. CLAUDE.md の「コーディング規約」セクションを編集する")
	fmt.Println("  2. GITHUB_TOKEN を設定して `hermit serve` を起動する")
}

// writeClaudeSettings creates .claude/settings.json in the current directory
// with the comprehensive permission set required for autonomous hermit operation.
func writeClaudeSettings() error {
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".claude", "settings.json"), permissions.DefaultSettingsJSON(), 0o644)
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
			fatal("harness.toml が見つかりません。プロジェクトルートで `hermit init` を実行してください。")
		}
		fatal("failed to load harness.toml: " + err.Error())
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
