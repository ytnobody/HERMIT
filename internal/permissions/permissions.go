// Package permissions provides utilities for verifying Claude Code permission
// settings, ensuring hermit can operate autonomously without confirmation prompts.
package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Settings represents the structure of a Claude Code settings.json file.
type Settings struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
}

// LoadSettings reads and parses a Claude Code settings.json file.
func LoadSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read settings file: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse settings file: %w", err)
	}
	return &s, nil
}

// IsBashAllowed reports whether the given bash command is covered by the allow
// list in s. It uses the same matching logic as Claude Code:
//
//   - "Bash(*)"          – allows any bash command
//   - "Bash(foo *)"      – allows commands starting with "foo " or equal to "foo"
//   - "Bash(foo)"        – allows the exact command "foo"
//
// The wildcard "*" follows simple glob semantics (matched by filepath.Match).
func (s *Settings) IsBashAllowed(command string) bool {
	for _, pattern := range s.Permissions.Allow {
		if matchPermission(pattern, command) {
			return true
		}
	}
	return false
}

// matchPermission checks whether a single permission pattern covers the given
// bash command string.
func matchPermission(pattern, command string) bool {
	// Non-Bash permission entries (Write, Edit, Agent, mcp__hermit__*…) are
	// irrelevant for bash-command matching.
	if !strings.HasPrefix(pattern, "Bash(") || !strings.HasSuffix(pattern, ")") {
		return false
	}

	// Extract the glob inside Bash(…).
	glob := pattern[len("Bash(") : len(pattern)-1]

	// Bash(*) – allow everything.
	if glob == "*" {
		return true
	}

	matched, err := filepath.Match(glob, command)
	if err != nil {
		// Malformed pattern – treat as no match.
		return false
	}
	return matched
}

// UncoveredCommands returns the subset of commands that are NOT covered by any
// allow-list entry in s.
func (s *Settings) UncoveredCommands(commands []string) []string {
	var uncovered []string
	for _, cmd := range commands {
		if !s.IsBashAllowed(cmd) {
			uncovered = append(uncovered, cmd)
		}
	}
	return uncovered
}

// DefaultSettingsJSON returns the canonical .claude/settings.json content that
// hermit projects should use for autonomous (prompt-free) operation.
func DefaultSettingsJSON() []byte {
	s := Settings{}
	s.Permissions.Allow = []string{
		"Bash(*)",
		"Write",
		"Edit",
		"Agent",
		"ScheduleWakeup",
		"EnterWorktree",
		"ExitWorktree",
		"TaskUpdate",
		"TaskCreate",
		"TaskList",
		"TaskGet",
		"mcp__hermit__list_issues",
		"mcp__hermit__assign_issue",
		"mcp__hermit__create_worktree",
		"mcp__hermit__close_worktree",
		"mcp__hermit__evaluate_risk",
		"mcp__hermit__merge_pr",
		"mcp__hermit__post_comment",
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return append(b, '\n')
}
