package github

import (
	"os"
	"testing"
)

func TestGetGitHubLogin_Success(t *testing.T) {
	// Use a fake 'gh' binary that prints a fixed login name.
	dir := t.TempDir()
	fakeBin := dir + "/gh"
	script := "#!/bin/sh\necho testuser\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	login, err := GetGitHubLogin()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if login != "testuser" {
		t.Errorf("expected %q, got %q", "testuser", login)
	}
}

func TestGetGitHubLogin_EmptyOutput(t *testing.T) {
	// Fake 'gh' that prints nothing.
	dir := t.TempDir()
	fakeBin := dir + "/gh"
	script := "#!/bin/sh\necho\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	_, err := GetGitHubLogin()
	if err == nil {
		t.Error("expected error for empty login output")
	}
}

func TestGetGitHubLogin_CommandFails(t *testing.T) {
	// Use a PATH with no 'gh' binary.
	t.Setenv("PATH", t.TempDir())

	_, err := GetGitHubLogin()
	if err == nil {
		t.Error("expected error when gh is not found")
	}
}
