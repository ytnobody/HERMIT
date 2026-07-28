package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
)

type checkResult struct {
	name   string
	passed bool
	warn   bool
	detail string
}

// isSnapGh reports whether the resolved gh executable path indicates a
// snap-confined install (e.g. /snap/bin/gh). Snap's confinement gives gh a
// private /tmp, so it cannot read files under the host's /tmp even though
// the shell (and other tools) can read them just fine.
func isSnapGh(path string) bool {
	return strings.Contains(path, "/snap/bin/")
}

func runChecks() []checkResult {
	var results []checkResult

	// Check: git is available
	_, err := exec.LookPath("git")
	results = append(results, checkResult{
		name:   "git command is available",
		passed: err == nil,
	})

	// Check: gh CLI is installed and authenticated
	ghAuthOut, ghAuthErr := exec.Command("gh", "auth", "status").CombinedOutput()
	ghPath, ghLookErr := exec.LookPath("gh")
	ghInstalled := ghLookErr == nil
	ghAuthed := ghInstalled && ghAuthErr == nil
	detail := ""
	if !ghInstalled {
		detail = "gh not found in PATH"
	} else if ghAuthErr != nil {
		detail = strings.TrimSpace(string(ghAuthOut))
	}
	results = append(results, checkResult{
		name:   "gh CLI is installed and authenticated",
		passed: ghAuthed,
		detail: detail,
	})

	// Check: gh CLI installation method. Snap-packaged gh (e.g. /snap/bin/gh)
	// runs under snap confinement with a private /tmp, so it cannot read
	// files under the host's /tmp even though the shell can. This trips up
	// patterns like `gh issue edit N --body-file /tmp/....md`. This is a
	// warning, not a failure: it does not affect overall doctor pass/fail.
	snapGh := ghInstalled && isSnapGh(ghPath)
	snapDetail := ""
	if snapGh {
		snapDetail = fmt.Sprintf(
			"gh is a snap install (%s); snap confinement means it cannot read files under the host's /tmp, "+
				"so passing a /tmp path via --body-file (etc.) can fail with \"open /tmp/...: no such file or directory\". "+
				"Use stdin instead (e.g. `gh issue edit N --body-file - < /path/to/file`), keep temp files under the project directory, "+
				"or switch to the apt/official binary build of gh: https://github.com/cli/cli/blob/trunk/docs/install_linux.md",
			ghPath,
		)
	}
	results = append(results, checkResult{
		name:   "gh CLI installation method (snap /tmp confinement)",
		passed: true,
		warn:   snapGh,
		detail: snapDetail,
	})

	// Check: GITHUB_TOKEN is set or obtainable via gh auth token
	token := os.Getenv("GITHUB_TOKEN")
	tokenSource := ""
	if token == "" && ghInstalled {
		out, err := exec.Command("gh", "auth", "token").Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
			if token != "" {
				tokenSource = "obtained via gh auth token"
			}
		}
	}
	tokenAvail := token != ""
	tokenDetail := ""
	if !tokenAvail {
		tokenDetail = "GITHUB_TOKEN not set and gh auth token failed"
	} else if tokenSource != "" {
		tokenDetail = tokenSource
	}
	results = append(results, checkResult{
		name:   "GITHUB_TOKEN is available",
		passed: tokenAvail,
		detail: tokenDetail,
	})

	// Check: harness.toml exists with required fields owner/repo
	harnessOK := false
	harnessDetail := ""
	data, err := os.ReadFile("harness.toml")
	if os.IsNotExist(err) {
		harnessDetail = "harness.toml not found"
	} else if err != nil {
		harnessDetail = "failed to read harness.toml: " + err.Error()
	} else {
		var cfg Config
		if _, decodeErr := toml.Decode(string(data), &cfg); decodeErr != nil {
			harnessDetail = "failed to parse harness.toml: " + decodeErr.Error()
		} else if cfg.GitHub.Owner == "" || cfg.GitHub.Repo == "" {
			harnessDetail = "harness.toml missing owner or repo"
		} else {
			harnessOK = true
		}
	}
	results = append(results, checkResult{
		name:   "harness.toml exists with owner/repo",
		passed: harnessOK,
		detail: harnessDetail,
	})

	// Check: Claude Code (claude) is installed
	_, err = exec.LookPath("claude")
	results = append(results, checkResult{
		name:   "Claude Code (claude) is installed",
		passed: err == nil,
	})

	// Checks: sandbox configuration in .claude/settings.json (REQ-018). These
	// are warnings, not hard failures, so `hermit doctor` keeps passing on
	// projects initialized before the sandbox recommendation existed.
	results = append(results, checkSandboxSettings(".claude/settings.json")...)

	return results
}

// sandboxSettingsRaw mirrors just the fields of the "sandbox" block in
// .claude/settings.json that checkSandboxSettings inspects. It is decoded
// independently of internal/permissions.Settings so a malformed or
// hand-edited settings.json (missing fields, extra keys) never breaks
// `hermit doctor` itself — unmarshal errors are treated as "not configured".
type sandboxSettingsRaw struct {
	Sandbox *struct {
		Enabled                  *bool    `json:"enabled"`
		AllowUnsandboxedCommands *bool    `json:"allowUnsandboxedCommands"`
		ExcludedCommands         []string `json:"excludedCommands"`
	} `json:"sandbox"`
}

// checkSandboxSettings inspects the "sandbox" block of the settings.json at
// path and warns about the three ways it can end up not actually restricting
// the Engineer's Bash tool (see Issue #180 / REQUIREMENTS.md REQ-018):
//
//   - sandbox.enabled is false or missing — the whole block is inert
//   - allowUnsandboxedCommands is true or missing — defaults to true in
//     Claude Code, which lets commands opt out of the sandbox entirely
//   - excludedCommands has entries — those commands bypass the sandbox
func checkSandboxSettings(path string) []checkResult {
	data, err := os.ReadFile(path)
	fileMissing := os.IsNotExist(err)

	var cfg sandboxSettingsRaw
	if err == nil {
		// Best-effort: malformed JSON is handled the same as "no sandbox
		// block configured" rather than failing doctor outright.
		_ = json.Unmarshal(data, &cfg)
	}

	missingFileDetail := path + " not found (run `hermit init`)"
	missingBlockDetail := "sandbox block missing from " + path

	enabled := cfg.Sandbox != nil && cfg.Sandbox.Enabled != nil && *cfg.Sandbox.Enabled
	enabledDetail := ""
	switch {
	case enabled:
		// no detail needed
	case fileMissing:
		enabledDetail = missingFileDetail
	case cfg.Sandbox == nil:
		enabledDetail = missingBlockDetail
	case cfg.Sandbox.Enabled == nil:
		enabledDetail = "sandbox.enabled not set"
	default:
		enabledDetail = "sandbox.enabled is false"
	}

	blocked := cfg.Sandbox != nil && cfg.Sandbox.AllowUnsandboxedCommands != nil && !*cfg.Sandbox.AllowUnsandboxedCommands
	blockedDetail := ""
	switch {
	case blocked:
		// no detail needed
	case fileMissing:
		blockedDetail = missingFileDetail
	case cfg.Sandbox == nil:
		blockedDetail = missingBlockDetail
	case cfg.Sandbox.AllowUnsandboxedCommands == nil:
		blockedDetail = "allowUnsandboxedCommands not set (defaults to true in Claude Code, which makes the sandbox block a no-op)"
	default:
		blockedDetail = "allowUnsandboxedCommands is true (defeats the sandbox; set it to false)"
	}

	var excluded []string
	if cfg.Sandbox != nil {
		excluded = cfg.Sandbox.ExcludedCommands
	}
	excludedDetail := ""
	if len(excluded) > 0 {
		excludedDetail = "sandbox.excludedCommands bypasses the sandbox for: " + strings.Join(excluded, ", ")
	}

	return []checkResult{
		{
			name:   "sandbox.enabled is true",
			passed: true,
			warn:   !enabled,
			detail: enabledDetail,
		},
		{
			name:   "allowUnsandboxedCommands is false",
			passed: true,
			warn:   !blocked,
			detail: blockedDetail,
		},
		{
			name:   "sandbox.excludedCommands is empty",
			passed: true,
			warn:   len(excluded) > 0,
			detail: excludedDetail,
		},
	}
}

func cmdDoctor() {
	results := runChecks()

	allPassed := true
	for _, r := range results {
		mark := "✓"
		if r.warn {
			mark = "⚠"
		}
		if !r.passed {
			mark = "✗"
			allPassed = false
		}
		line := fmt.Sprintf("  %s  %s", mark, r.name)
		if r.detail != "" {
			line += fmt.Sprintf(" (%s)", r.detail)
		}
		fmt.Println(line)
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All checks passed.")
	} else {
		fmt.Fprintln(os.Stderr, "One or more checks failed.")
		os.Exit(1)
	}
}
