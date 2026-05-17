package permissions_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ytnobody/hermit/internal/permissions"
)

// TestLoadSettings_FileNotFound covers the os.ReadFile error branch.
func TestLoadSettings_FileNotFound(t *testing.T) {
	_, err := permissions.LoadSettings("/nonexistent/path/settings.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestLoadSettings_InvalidJSON covers the json.Unmarshal error branch.
func TestLoadSettings_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	os.WriteFile(path, []byte("not valid json {{"), 0o644)
	_, err := permissions.LoadSettings(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestMatchPermission_MalformedGlob covers the filepath.Match error branch
// (a Bash() pattern containing an unterminated character class).
func TestMatchPermission_MalformedGlob(t *testing.T) {
	s := &permissions.Settings{}
	s.Permissions.Allow = []string{"Bash([invalid)"}
	// A malformed glob should NOT match — it must not panic or return true.
	if s.IsBashAllowed("git status") {
		t.Error("malformed glob should not match any command")
	}
}

// TestMatchPermission_NonBashPatternSkipped covers the early-return branch
// inside matchPermission when the allow entry is not a Bash() pattern (e.g.
// "Write", "Edit"). Such entries must be silently skipped so that a
// subsequent Bash() entry can still grant access.
func TestMatchPermission_NonBashPatternSkipped(t *testing.T) {
	s := &permissions.Settings{}
	// Non-Bash entries first, then a Bash prefix pattern.
	s.Permissions.Allow = []string{"Write", "Edit", "Bash(git *)"}

	if !s.IsBashAllowed("git status") {
		t.Error("Bash(git *) should match 'git status' even with non-Bash entries preceding it")
	}
	if s.IsBashAllowed("go build ./...") {
		t.Error("'go build ./...' should not be allowed by Bash(git *)")
	}
}

// TestUncoveredCommands_SomeUncovered covers the append branch in UncoveredCommands.
func TestUncoveredCommands_SomeUncovered(t *testing.T) {
	s := &permissions.Settings{}
	s.Permissions.Allow = []string{"Bash(git *)"}

	uncovered := s.UncoveredCommands([]string{"git status", "go build ./..."})
	if len(uncovered) != 1 || uncovered[0] != "go build ./..." {
		t.Errorf("expected [\"go build ./...\"], got %v", uncovered)
	}
}

// projectRoot returns the absolute path to the HERMIT project root by walking
// up from the test file's directory.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine source file path")
	}
	// filename → …/internal/permissions/permissions_test.go
	// project root is two levels up.
	root := filepath.Join(filepath.Dir(filename), "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("cannot resolve project root: %v", err)
	}
	return abs
}

// hermitCommands is the authoritative list of bash commands that the hermit
// Superintendent and Engineer agents may run during autonomous operation.
// Every entry MUST be covered by at least one allow-list pattern; the test
// will fail if any command is missing.
var hermitCommands = []string{
	// --- git operations ---
	"git add -A",
	"git add .",
	`git commit -m "fix: something"`,
	"git push origin hermit/issue-2",
	"git worktree add /tmp/hermit-2 -b hermit/issue-2",
	"git worktree remove /tmp/hermit-2",
	"git worktree list",
	"git checkout main",
	"git switch main",
	"git merge hermit/issue-2",
	"git rebase main",
	"git log --oneline",
	"git log --oneline -5",
	"git status",
	"git diff",
	"git diff HEAD",
	"git init",
	"git init .",
	"git fetch origin",
	"git pull origin main",

	// --- go toolchain ---
	"go build ./...",
	"go test ./...",
	"go test ./... -v",
	"go test ./internal/permissions/... -v",
	"go vet ./...",
	"go mod tidy",
	"go mod download",
	"go get github.com/some/pkg",
	"go generate ./...",
	"go version",
	"go env GOPATH",

	// --- file operations ---
	"ls /tmp",
	"ls /home/user/.claude",
	"ls /home/ytnobody/HERMIT/.claude",
	"cat /home/user/.claude/settings.json",
	"cat /home/user/project/.claude/settings.json 2>/dev/null || cat fallback",
	"mkdir /tmp/hermit-5",
	"mkdir -p /tmp/hermit-5/.claude",
	"cp file1 file2",
	"cp -r src/ dst/",
	"mv file1 file2",
	"rm /tmp/hermit-5",
	"rm -rf /tmp/hermit-5",
	"chmod +x hermit",
	"chmod 755 hermit",

	// --- gh CLI ---
	`gh pr create --title "fix" --body "body"`,
	"gh pr merge 42 --squash",
	`gh pr comment 42 --body "HIGH risk"`,
	"gh pr view 42",
	"gh pr list",
	"gh pr list --state open",
	"gh issue list",
	"gh issue list --state open",
	"gh issue edit 2 --add-assignee @me",
	"gh api repos/owner/repo/issues",
	"gh api repos/owner/repo/pulls/42/merge --method PUT",
	"gh auth token",

	// --- test/condition checks ---
	"test -f .hermit-paused",
	"test -d /tmp/hermit-2",
	"test -e /path",

	// --- hermit binary ---
	"hermit status",
	"hermit pause",
	"hermit resume",
	"hermit serve",
	"/home/user/HERMIT/hermit status",
	"/home/user/HERMIT/hermit serve",
	"/home/ytnobody/HERMIT/hermit status",

	// --- other build tools ---
	"npm install",
	"npm test",
	"npm run build",
	"cargo build",
	"cargo test",
	"python -m pytest",
	"make test",
	"make build",

	// --- process / system inspection ---
	"ps aux",
	"which hermit",
	`find . -name "*.go"`,
	`grep -r "TODO" .`,
}

// TestAllHermitCommandsAreCovered loads the ACTUAL .claude/settings.json from
// the project root and asserts that every command in hermitCommands is allowed
// without a confirmation prompt.
func TestAllHermitCommandsAreCovered(t *testing.T) {
	root := projectRoot(t)
	settingsPath := filepath.Join(root, ".claude", "settings.json")

	s, err := permissions.LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("failed to load %s: %v", settingsPath, err)
	}

	uncovered := s.UncoveredCommands(hermitCommands)
	if len(uncovered) == 0 {
		t.Logf("All %d commands are covered by the allow list in %s", len(hermitCommands), settingsPath)
		return
	}

	t.Errorf("%d command(s) are NOT covered by the allow list in %s:", len(uncovered), settingsPath)
	for _, cmd := range uncovered {
		t.Errorf("  NOT COVERED: %q", cmd)
	}
}

// TestBashWildcardMatchesAll verifies that Bash(*) covers any arbitrary command.
func TestBashWildcardMatchesAll(t *testing.T) {
	s := &permissions.Settings{}
	s.Permissions.Allow = []string{"Bash(*)"}

	cases := []string{
		"git status",
		"cat /etc/passwd",
		"rm -rf /",
		"some completely arbitrary command with spaces and --flags",
	}
	for _, cmd := range cases {
		if !s.IsBashAllowed(cmd) {
			t.Errorf("Bash(*) should allow %q but did not", cmd)
		}
	}
}

// TestPrefixPatternMatching verifies that Bash(foo *) only allows commands
// starting with "foo ".
func TestPrefixPatternMatching(t *testing.T) {
	s := &permissions.Settings{}
	s.Permissions.Allow = []string{"Bash(git *)"}

	allowed := []string{
		"git status",
		`git commit -m "msg"`,
		"git push origin main",
	}
	denied := []string{
		"gh pr create",
		"go build ./...",
		"gitk", // no space after "git"
	}

	for _, cmd := range allowed {
		if !s.IsBashAllowed(cmd) {
			t.Errorf("Bash(git *) should allow %q but did not", cmd)
		}
	}
	for _, cmd := range denied {
		if s.IsBashAllowed(cmd) {
			t.Errorf("Bash(git *) should NOT allow %q but it did", cmd)
		}
	}
}

// TestDefaultSettingsJSONIsValid checks that DefaultSettingsJSON produces valid
// JSON that, when loaded, contains Bash(*) in the allow list.
func TestDefaultSettingsJSONIsValid(t *testing.T) {
	data := permissions.DefaultSettingsJSON()
	if len(data) == 0 {
		t.Fatal("DefaultSettingsJSON returned empty bytes")
	}

	// Verify it is valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("DefaultSettingsJSON is not valid JSON: %v", err)
	}

	// Write to a temp file and load through the real loader.
	tmp := t.TempDir()
	dotClaude := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dotClaude, "settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp settings: %v", err)
	}

	s, err := permissions.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings on DefaultSettingsJSON output: %v", err)
	}

	// Must contain Bash(*).
	found := false
	for _, entry := range s.Permissions.Allow {
		if entry == "Bash(*)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DefaultSettingsJSON allow list does not contain Bash(*); got: %v", s.Permissions.Allow)
	}

	// Must cover all hermit commands.
	uncovered := s.UncoveredCommands(hermitCommands)
	if len(uncovered) > 0 {
		t.Errorf("DefaultSettingsJSON does not cover %d command(s): %s",
			len(uncovered), strings.Join(uncovered, ", "))
	}
}
