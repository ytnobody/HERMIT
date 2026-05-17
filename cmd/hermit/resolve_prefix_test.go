package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveBranchPrefix tests the resolveBranchPrefix function using
// table-driven tests with fake 'gh' binaries to control the login output.
func TestResolveBranchPrefix(t *testing.T) {
	tests := []struct {
		name         string
		configPrefix string // harness.toml branch_prefix value (empty = not set)
		fakGhLogin   string // what the fake gh binary prints ("" means gh fails)
		wantPrefix   string
		wantContains string // alternative: check Contains instead of exact match
	}{
		{
			name:         "explicit branch_prefix in config takes priority",
			configPrefix: "myorg/myteam",
			fakGhLogin:   "someuser",
			wantPrefix:   "myorg/myteam",
		},
		{
			name:       "gh login available, no explicit config",
			fakGhLogin: "gh-user",
			wantPrefix: "hermit/gh-user",
		},
		{
			name:       "gh login unavailable, falls back to hermit",
			fakGhLogin: "", // gh binary will fail
			wantPrefix: "hermit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set up fake gh binary.
			fakeGhDir := t.TempDir()
			var ghScript string
			if tc.fakGhLogin != "" {
				ghScript = "#!/bin/sh\necho " + tc.fakGhLogin + "\n"
			} else {
				ghScript = "#!/bin/sh\nexit 1\n"
			}
			if err := os.WriteFile(filepath.Join(fakeGhDir, "gh"), []byte(ghScript), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeGhDir+":"+os.Getenv("PATH"))

			// Build a config.
			cfg := Config{}
			cfg.Agent.BranchPrefix = tc.configPrefix

			var got string
			// Capture stderr to avoid test noise.
			origStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w
			got = resolveBranchPrefix(cfg)
			w.Close()
			os.Stderr = origStderr
			r.Close()

			if got != tc.wantPrefix {
				t.Errorf("resolveBranchPrefix = %q, want %q", got, tc.wantPrefix)
			}
		})
	}
}

// TestResolveBranchPrefix_WarnOnFallback verifies that a warning is printed to
// stderr when gh CLI is unavailable and the fallback prefix is used.
func TestResolveBranchPrefix_WarnOnFallback(t *testing.T) {
	// Ensure gh is not found.
	t.Setenv("PATH", t.TempDir())

	cfg := Config{}

	var stderrBuf strings.Builder
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	result := resolveBranchPrefix(cfg)

	w.Close()
	os.Stderr = origStderr
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderrBuf.Write(buf[:n])

	if result != "hermit" {
		t.Errorf("fallback prefix: got %q, want %q", result, "hermit")
	}
	if !strings.Contains(stderrBuf.String(), "warn:") {
		t.Errorf("expected warn on stderr, got: %q", stderrBuf.String())
	}
}

// TestLoadConfig_BranchPrefix verifies that branch_prefix is read from harness.toml.
func TestLoadConfig_BranchPrefix(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
[agent]
max_engineers = 2
language = "en"
branch_prefix = "custom/prefix"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Agent.BranchPrefix != "custom/prefix" {
		t.Errorf("BranchPrefix: got %q, want %q", cfg.Agent.BranchPrefix, "custom/prefix")
	}
}

// TestLoadConfig_BranchPrefix_Omitted verifies that branch_prefix defaults to
// empty string when omitted from harness.toml.
func TestLoadConfig_BranchPrefix_Omitted(t *testing.T) {
	dir := t.TempDir()
	content := `[github]
owner = "owner"
repo  = "repo"
[agent]
max_engineers = 2
language = "en"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	cfg := loadConfig()
	if cfg.Agent.BranchPrefix != "" {
		t.Errorf("BranchPrefix: got %q, want empty string", cfg.Agent.BranchPrefix)
	}
}
