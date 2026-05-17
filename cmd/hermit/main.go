package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/mcp"
	"github.com/ytnobody/hermit/internal/permissions"
)

//go:embed templates/*
var templateFS embed.FS

type Config struct {
	GitHub struct {
		Owner string `toml:"owner"`
		Repo  string `toml:"repo"`
	} `toml:"github"`
	Agent struct {
		MaxEngineers int    `toml:"max_engineers"`
		Language     string `toml:"language"`
	} `toml:"agent"`
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
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hermit <serve|install|init|pause|resume|status>")
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

func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		fatal("GITHUB_TOKEN が未設定で、gh auth token の実行にも失敗しました。\n" +
			"  - 環境変数 GITHUB_TOKEN を設定するか\n" +
			"  - gh auth login で認証してください")
	}
	return strings.TrimSpace(string(out))
}

func cmdServe() {
	cfg := loadConfig()
	token := githubToken()
	client := gh.NewClient(token, cfg.GitHub.Owner, cfg.GitHub.Repo)
	if err := mcp.Serve(client); err != nil {
		fatal(err.Error())
	}
}

func cmdInstall() {
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
		"command": "hermit",
		"args":    []string{"serve"},
		"env": map[string]string{
			"GITHUB_TOKEN": "${GITHUB_TOKEN}",
		},
	}
	settings["mcpServers"] = mcpServers

	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(settingsPath, append(b, '\n'), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Println("✓ HERMIT MCP server registered in", settingsPath)
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

	type tmplData struct {
		Owner        string
		Repo         string
		Language     string
		MaxEngineers int
	}
	data := tmplData{Owner: owner, Repo: repo, Language: lang, MaxEngineers: maxEng}

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
	t, err := template.New("").Parse(string(content))
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

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
