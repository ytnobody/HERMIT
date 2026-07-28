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
	Permissions PermissionsBlock `json:"permissions"`
	// Sandbox holds the recommended sandbox configuration written by
	// `hermit init` (see DefaultSandboxSettings). It is a pointer so that
	// settings.json files predating REQ-018 (no sandbox block at all) can be
	// distinguished from ones that explicitly configure it.
	Sandbox *SandboxSettings `json:"sandbox,omitempty"`
}

// PermissionsBlock is the `permissions` object of a Claude Code settings.json
// file.
type PermissionsBlock struct {
	Allow []string `json:"allow"`
}

// SandboxSettings represents the `sandbox` object hermit recommends in
// settings.json to narrow what the Engineer's Bash tool can reach on the host
// machine, without resorting to an allow-list of individual commands (see
// README "Security" section for the allow-list failure mode this replaces,
// and its scope/precedence caveats).
type SandboxSettings struct {
	// Enabled turns the sandbox on. Without this, the rest of the block has
	// no effect.
	Enabled bool `json:"enabled"`
	// AllowUnsandboxedCommands defaults to true in Claude Code itself, which
	// means a sandbox block with this field omitted is effectively inert —
	// hermit always writes it explicitly as false.
	AllowUnsandboxedCommands bool `json:"allowUnsandboxedCommands"`
	// ExcludedCommands lists Bash command patterns that bypass the sandbox
	// entirely. hermit's generated config leaves this empty; a non-empty
	// list here re-opens the hole the sandbox is meant to close, which is
	// why `hermit doctor` flags it.
	ExcludedCommands []string                  `json:"excludedCommands,omitempty"`
	Network          SandboxNetworkSettings    `json:"network"`
	Credentials      SandboxCredentialSettings `json:"credentials"`
}

// SandboxNetworkSettings restricts outbound network access from the sandbox.
type SandboxNetworkSettings struct {
	// TLSTerminate must be present (even empty) for envVars credential
	// masking/injection to work, since Claude Code needs to terminate TLS to
	// inject the token header only for allowed hosts.
	TLSTerminate   map[string]any `json:"tlsTerminate"`
	AllowedDomains []string       `json:"allowedDomains"`
}

// SandboxCredentialSettings hides host credentials from the sandboxed
// process, and controls how sensitive env vars are exposed to it.
type SandboxCredentialSettings struct {
	Files   []SandboxCredentialFile   `json:"files"`
	EnvVars []SandboxCredentialEnvVar `json:"envVars"`
}

// SandboxCredentialFile denies (or otherwise restricts) sandbox access to a
// host path likely to contain credentials.
type SandboxCredentialFile struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// SandboxCredentialEnvVar controls how an environment variable is exposed to
// the sandboxed process. Mode "deny" would break tools (like gh) that need
// the token; hermit uses "mask" + InjectHosts so the real value is only
// injected on requests to trusted hosts.
type SandboxCredentialEnvVar struct {
	Name        string   `json:"name"`
	Mode        string   `json:"mode"`
	InjectHosts []string `json:"injectHosts,omitempty"`
}

// DefaultSandboxSettings returns hermit's recommended sandbox configuration.
//
// AllowUnsandboxedCommands is explicitly false: Claude Code defaults this to
// true, and leaving it unset (or true) makes the rest of the sandbox block
// effectively optional for the model to honor.
//
// GITHUB_TOKEN is "mask" + InjectHosts, never "deny": gh CLI (and therefore
// `gh pr create`, `gh issue comment`, etc.) needs the real token to reach
// api.github.com. "deny" would break the Engineer's ability to open PRs.
//
// AllowedDomains includes the Go toolchain's module proxy/sum/storage hosts
// in addition to GitHub, since `go build`/`go test` fetch dependencies over
// the network. Verify this list actually covers a project's dependency graph
// by running `go test ./...` under the generated sandbox.
func DefaultSandboxSettings() SandboxSettings {
	return SandboxSettings{
		Enabled:                  true,
		AllowUnsandboxedCommands: false,
		Network: SandboxNetworkSettings{
			TLSTerminate: map[string]any{},
			AllowedDomains: []string{
				"*.github.com",
				"proxy.golang.org",
				"sum.golang.org",
				"storage.googleapis.com",
			},
		},
		Credentials: SandboxCredentialSettings{
			Files: []SandboxCredentialFile{
				{Path: "~/.ssh", Mode: "deny"},
				{Path: "~/.aws/credentials", Mode: "deny"},
			},
			EnvVars: []SandboxCredentialEnvVar{
				{Name: "GITHUB_TOKEN", Mode: "mask", InjectHosts: []string{"api.github.com"}},
			},
		},
	}
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
	s := defaultSettings()
	b, _ := json.MarshalIndent(s, "", "  ")
	return append(b, '\n')
}

// defaultSettings builds the full Settings value (permissions allow-list plus
// recommended sandbox block) that a fresh `hermit init` writes.
func defaultSettings() Settings {
	s := Settings{}
	s.Permissions.Allow = []string{
		"Bash(*)",
		"Write",
		"Edit",
		"Agent",
		"ScheduleWakeup",
		"WebFetch",
		"WebSearch",
		"EnterWorktree",
		"ExitWorktree",
		"TaskUpdate",
		"TaskCreate",
		"TaskList",
		"TaskGet",
		"TaskOutput",
		"TaskStop",
		"Monitor",
		"mcp__hermit__list_issues",
		"mcp__hermit__assign_issue",
		"mcp__hermit__create_worktree",
		"mcp__hermit__evaluate_risk",
		"mcp__hermit__merge_pr",
		"mcp__hermit__post_comment",
	}
	sandbox := DefaultSandboxSettings()
	s.Sandbox = &sandbox
	return s
}

// MergeDefaultSettings computes the settings.json content `hermit init`
// should write at path.
//
//   - If no file exists yet at path, it is a fresh project: return
//     DefaultSettingsJSON() unchanged.
//   - If a file already exists, preserve every top-level key it already has
//     (most importantly "permissions", which a project may have hand-tuned
//     after the first `hermit init`) and only fill in keys that are entirely
//     absent — "sandbox" first and foremost — with hermit's recommended
//     defaults. Re-running `hermit init` on an already-initialized project
//     must never destroy prior customization.
func MergeDefaultSettings(path string) ([]byte, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSettingsJSON(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing settings file: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(existing, &raw); err != nil {
		return nil, fmt.Errorf("parse existing settings file: %w", err)
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}

	def := defaultSettings()

	if _, ok := raw["permissions"]; !ok {
		permJSON, err := json.Marshal(def.Permissions)
		if err != nil {
			return nil, fmt.Errorf("marshal default permissions: %w", err)
		}
		raw["permissions"] = permJSON
	}

	if _, ok := raw["sandbox"]; !ok {
		sandboxJSON, err := json.Marshal(def.Sandbox)
		if err != nil {
			return nil, fmt.Errorf("marshal default sandbox settings: %w", err)
		}
		raw["sandbox"] = sandboxJSON
	}

	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged settings: %w", err)
	}
	return append(b, '\n'), nil
}
