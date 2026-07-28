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

// TestREQ018_DefaultSandboxSettings_AllowUnsandboxedCommandsFalse verifies
// the sandbox default explicitly disables the "allowUnsandboxedCommands"
// escape hatch. Claude Code defaults this field to true, so hermit must set
// it to false explicitly or the rest of the sandbox block is effectively
// optional.
func TestREQ018_DefaultSandboxSettings_AllowUnsandboxedCommandsFalse(t *testing.T) {
	sb := permissions.DefaultSandboxSettings()
	if !sb.Enabled {
		t.Error("expected sandbox.enabled = true")
	}
	if sb.AllowUnsandboxedCommands {
		t.Error("expected sandbox.allowUnsandboxedCommands = false")
	}
}

// TestREQ018_DefaultSandboxSettings_GithubTokenMaskedNotDenied verifies
// GITHUB_TOKEN is exposed via "mask" + injectHosts rather than "deny": gh CLI
// needs the real token to reach api.github.com, so "deny" would break `gh pr
// create` and friends.
func TestREQ018_DefaultSandboxSettings_GithubTokenMaskedNotDenied(t *testing.T) {
	sb := permissions.DefaultSandboxSettings()

	var tokenVar *permissions.SandboxCredentialEnvVar
	for i := range sb.Credentials.EnvVars {
		if sb.Credentials.EnvVars[i].Name == "GITHUB_TOKEN" {
			tokenVar = &sb.Credentials.EnvVars[i]
			break
		}
	}
	if tokenVar == nil {
		t.Fatal("expected GITHUB_TOKEN entry in sandbox.credentials.envVars")
	}
	if tokenVar.Mode == "deny" {
		t.Error("GITHUB_TOKEN must not be mode=deny (breaks gh CLI); expected mode=mask")
	}
	if tokenVar.Mode != "mask" {
		t.Errorf("expected GITHUB_TOKEN mode=mask, got %q", tokenVar.Mode)
	}
	found := false
	for _, h := range tokenVar.InjectHosts {
		if h == "api.github.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected injectHosts to include api.github.com, got %v", tokenVar.InjectHosts)
	}
	// mask+injectHosts requires tlsTerminate to be configured.
	if sb.Network.TLSTerminate == nil {
		t.Error("expected network.tlsTerminate to be set (required for envVar host-scoped injection)")
	}
}

// TestREQ018_DefaultSandboxSettings_GoToolchainDomainsAllowed verifies the Go
// module proxy/sum/storage hosts are present in allowedDomains, since `go
// build`/`go test` need network access to fetch dependencies. This is a
// structural check; go.mod/go.sum actually resolving through these hosts is
// verified by `go test ./...` succeeding in CI/dev, per the acceptance
// criteria on Issue #180.
func TestREQ018_DefaultSandboxSettings_GoToolchainDomainsAllowed(t *testing.T) {
	sb := permissions.DefaultSandboxSettings()
	want := []string{"proxy.golang.org", "sum.golang.org", "storage.googleapis.com", "*.github.com"}
	for _, w := range want {
		found := false
		for _, d := range sb.Network.AllowedDomains {
			if d == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in network.allowedDomains, got %v", w, sb.Network.AllowedDomains)
		}
	}
}

// TestREQ018_DefaultSettingsJSON_IncludesSandbox verifies the JSON hermit
// init writes for a fresh project contains the sandbox block described in
// Issue #180, in addition to the existing permissions allow-list.
func TestREQ018_DefaultSettingsJSON_IncludesSandbox(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	if err := os.WriteFile(path, permissions.DefaultSettingsJSON(), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := permissions.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Sandbox == nil {
		t.Fatal("expected non-nil Sandbox block in DefaultSettingsJSON output")
	}
	if !s.Sandbox.Enabled || s.Sandbox.AllowUnsandboxedCommands {
		t.Errorf("expected enabled=true, allowUnsandboxedCommands=false, got %+v", s.Sandbox)
	}
}

// TestREQ018_MergeDefaultSettings_FreshFile verifies MergeDefaultSettings
// behaves like DefaultSettingsJSON when no settings.json exists yet.
func TestREQ018_MergeDefaultSettings_FreshFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")

	merged, err := permissions.MergeDefaultSettings(path)
	if err != nil {
		t.Fatalf("MergeDefaultSettings: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(merged, &raw); err != nil {
		t.Fatalf("merged output is not valid JSON: %v", err)
	}
	if _, ok := raw["sandbox"]; !ok {
		t.Error("expected sandbox key in merged output for a fresh file")
	}
	if _, ok := raw["permissions"]; !ok {
		t.Error("expected permissions key in merged output for a fresh file")
	}
}

// TestREQ018_MergeDefaultSettings_PreservesExistingPermissions is the direct
// regression test for the Issue #180 acceptance criterion: re-running
// `hermit init` (which calls MergeDefaultSettings) against a project that
// already has a hand-tuned .claude/settings.json must not destroy the
// existing "permissions" block, even though it lacks Bash(*) and looks
// nothing like hermit's own default.
func TestREQ018_MergeDefaultSettings_PreservesExistingPermissions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	existing := `{
  "permissions": {
    "allow": ["Bash(git *)", "Bash(go *)"]
  },
  "someOtherProjectKey": {"foo": "bar"}
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := permissions.MergeDefaultSettings(path)
	if err != nil {
		t.Fatalf("MergeDefaultSettings: %v", err)
	}

	var s permissions.Settings
	if err := json.Unmarshal(merged, &s); err != nil {
		t.Fatalf("unmarshal merged settings: %v", err)
	}
	if len(s.Permissions.Allow) != 2 || s.Permissions.Allow[0] != "Bash(git *)" || s.Permissions.Allow[1] != "Bash(go *)" {
		t.Errorf("expected existing custom permissions.allow to survive unchanged, got %v", s.Permissions.Allow)
	}
	if (&s).IsBashAllowed("gh pr create") {
		t.Error("merge must not silently widen the existing (narrower) allow-list")
	}

	// The sandbox block should have been added since it was absent.
	if s.Sandbox == nil || !s.Sandbox.Enabled || s.Sandbox.AllowUnsandboxedCommands {
		t.Errorf("expected sandbox block to be filled in with hermit defaults, got %+v", s.Sandbox)
	}

	// Unrelated top-level keys must survive too.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(merged, &raw); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if _, ok := raw["someOtherProjectKey"]; !ok {
		t.Error("expected unrelated top-level key 'someOtherProjectKey' to survive the merge")
	}
}

// TestREQ018_MergeDefaultSettings_PreservesExistingSandbox verifies a
// project that has already customized its sandbox block (e.g. added an
// extra allowed domain) keeps that customization on a re-run of `hermit
// init`, rather than being clobbered back to hermit's defaults.
func TestREQ018_MergeDefaultSettings_PreservesExistingSandbox(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	existing := `{
  "permissions": {"allow": ["Bash(*)"]},
  "sandbox": {
    "enabled": true,
    "allowUnsandboxedCommands": false,
    "network": {"tlsTerminate": {}, "allowedDomains": ["*.github.com", "registry.npmjs.org"]}
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := permissions.MergeDefaultSettings(path)
	if err != nil {
		t.Fatalf("MergeDefaultSettings: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(merged, &raw); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	sandbox, ok := raw["sandbox"].(map[string]any)
	if !ok {
		t.Fatal("expected sandbox object in merged output")
	}
	network, ok := sandbox["network"].(map[string]any)
	if !ok {
		t.Fatal("expected sandbox.network object in merged output")
	}
	domains, ok := network["allowedDomains"].([]any)
	if !ok {
		t.Fatal("expected sandbox.network.allowedDomains array in merged output")
	}
	found := false
	for _, d := range domains {
		if d == "registry.npmjs.org" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pre-existing custom domain 'registry.npmjs.org' to survive the merge, got %v", domains)
	}
}

// TestREQ018_MergeDefaultSettings_MissingFileError verifies the underlying
// os.ReadFile error path other than "not exist" is surfaced rather than
// swallowed (e.g. a permission-denied directory component). We simulate this
// by pointing at a path whose parent is a file, not a directory, which
// produces an ENOTDIR rather than ENOENT.
func TestREQ018_MergeDefaultSettings_MissingFileError(t *testing.T) {
	tmp := t.TempDir()
	notADir := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(notADir, "settings.json")

	_, err := permissions.MergeDefaultSettings(path)
	if err == nil {
		t.Fatal("expected an error when the parent path is not a directory")
	}
}

// TestREQ018_MergeDefaultSettings_InvalidJSONError verifies a malformed
// existing settings.json produces an error rather than silently discarding
// the file's content.
func TestREQ018_MergeDefaultSettings_InvalidJSONError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	if err := os.WriteFile(path, []byte("not valid json {{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := permissions.MergeDefaultSettings(path)
	if err == nil {
		t.Fatal("expected an error for malformed existing settings.json")
	}
}
