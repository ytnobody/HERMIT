package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestServeFromWrongCwd verifies that hermit serve starts and lists all tools
// even when the OS working directory is NOT the project root — which is what
// happens when Claude Code launches the MCP server without honouring cwd.
func TestServeFromWrongCwd(t *testing.T) {
	// Build the binary into a temporary project directory that holds harness.toml.
	projDir := t.TempDir()
	binPath := filepath.Join(projDir, "hermit")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	// Build from the repo root so all internal packages are available.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/hermit")
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Write a minimal harness.toml next to the binary (projDir).
	harness := `[github]
owner = "test-owner"
repo  = "test-repo"

[agent]
max_engineers = 4
language      = "ja"
`
	if err := os.WriteFile(filepath.Join(projDir, "harness.toml"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}

	// Launch from a completely different temp dir that has NO harness.toml.
	wrongCwd := t.TempDir()
	srv := exec.Command(binPath, "serve")
	srv.Dir = wrongCwd
	srv.Env = append(os.Environ(), "GITHUB_TOKEN=dummy-token-for-test")

	stdin, err := srv.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := srv.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	srv.Stderr = os.Stderr

	if err := srv.Start(); err != nil {
		t.Fatalf("server failed to start: %v", err)
	}
	defer srv.Process.Kill()

	// Send MCP handshake.
	send := func(msg string) {
		fmt.Fprintln(stdin, msg)
	}
	send(`{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}`)
	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	send(`{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}`)

	// Collect the tools/list response within 5 s.
	type rpcMsg struct {
		ID     any            `json:"id"`
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}

	resultCh := make(chan rpcMsg, 4)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var m rpcMsg
			if err := json.Unmarshal(sc.Bytes(), &m); err == nil {
				resultCh <- m
			}
		}
		close(resultCh)
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg, ok := <-resultCh:
			if !ok {
				t.Fatal("server exited before returning tools/list")
			}
			if msg.Error != nil {
				t.Fatalf("server returned error: %v", msg.Error)
			}
			// id==2 is the tools/list response.
			idFloat, _ := msg.ID.(float64)
			if int(idFloat) != 2 {
				continue
			}
			tools, _ := msg.Result["tools"].([]any)
			if len(tools) != 10 {
				t.Errorf("expected 10 tools, got %d", len(tools))
			}
			return
		case <-deadline:
			t.Fatal("timeout: server did not return tools/list within 5 s (likely crashed on startup)")
		}
	}
}
