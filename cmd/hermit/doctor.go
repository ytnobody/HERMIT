package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
)

type checkResult struct {
	name   string
	passed bool
	detail string
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
	ghInstalled := true
	if _, lookErr := exec.LookPath("gh"); lookErr != nil {
		ghInstalled = false
	}
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

	return results
}

func cmdDoctor() {
	results := runChecks()

	allPassed := true
	for _, r := range results {
		mark := "✓"
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
