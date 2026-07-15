package main

// REQ-named test for the requirements reconcile sweep (Issue #152). See
// REQUIREMENTS.md, REQ-012.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// tokenKeyRe matches a TOML key assignment that would store a GitHub token
// (e.g. `token = "..."` or `github_token = "..."`), while not matching
// unrelated keys such as rate_limit_threshold.
var tokenKeyRe = regexp.MustCompile(`(?mi)^\s*[a-z_]*token\s*=`)

// TestREQ012_GithubTokenFromEnvOnly verifies REQ-012: the GitHub token is
// taken from the GITHUB_TOKEN environment variable, and neither the
// harness.toml template nor the repository's own harness.toml contains a
// token setting.
func TestREQ012_GithubTokenFromEnvOnly(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "req012-token-from-env")
	if got := githubToken(); got != "req012-token-from-env" {
		t.Errorf("githubToken() = %q, want the GITHUB_TOKEN env value", got)
	}

	// The harness.toml template must not offer any token key. Read it from
	// the embedded FS so the check is independent of the test's working
	// directory (some tests in this package chdir).
	tmpl, err := templateFS.ReadFile("templates/harness.toml.tmpl")
	if err != nil {
		t.Fatalf("reading embedded harness.toml.tmpl: %v", err)
	}
	if tokenKeyRe.Match(tmpl) {
		t.Errorf("harness.toml.tmpl must not contain a token key, found: %s",
			tokenKeyRe.Find(tmpl))
	}

	// Nor must the repository's own harness.toml (it is committed and
	// shared). Resolve it relative to this source file, not the cwd.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	toml, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "harness.toml"))
	if err != nil {
		t.Fatalf("reading harness.toml: %v", err)
	}
	if tokenKeyRe.Match(toml) {
		t.Errorf("harness.toml must not contain a token key, found: %s",
			tokenKeyRe.Find(toml))
	}
}
