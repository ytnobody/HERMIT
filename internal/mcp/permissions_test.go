package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ytnobody/hermit/internal/permissions"
)

// toolNamePattern matches the tool-name string literal passed as the first
// argument to mcp.NewTool(...) calls in tools.go, e.g.:
//
//	mcp.NewTool("close_issue",
var toolNamePattern = regexp.MustCompile(`mcp\.NewTool\(\s*"([a-zA-Z0-9_]+)"`)

// registeredToolNames parses internal/mcp/tools.go and returns the set of
// tool names registered via mcp.NewTool(...), each prefixed with
// "mcp__hermit__" to match the form used in .claude/settings.json's
// permissions.allow list.
func registeredToolNames(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("reading tools.go: %v", err)
	}
	matches := toolNamePattern.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatalf("no mcp.NewTool(...) registrations found in tools.go — regexp may be stale")
	}
	names := make(map[string]bool, len(matches))
	for _, m := range matches {
		names["mcp__hermit__"+m[1]] = true
	}
	return names
}

// allowedHermitToolNames reads .claude/settings.json (at the repo root) and
// returns the subset of permissions.allow entries that are mcp__hermit__*
// tool grants.
func allowedHermitToolNames(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", ".claude", "settings.json")
	settings, err := permissions.LoadSettings(path)
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	allowed := make(map[string]bool)
	for _, entry := range settings.Permissions.Allow {
		if strings.HasPrefix(entry, "mcp__hermit__") {
			allowed[entry] = true
		}
	}
	return allowed
}

// TestAllRegisteredToolsAreAllowlisted guards against the recurrence of
// Issue #138: every tool registered in internal/mcp/tools.go via
// mcp.NewTool(...) must appear in .claude/settings.json's
// permissions.allow list, otherwise the Superintendent loop stalls on a
// confirmation prompt the first time the tool is invoked.
func TestAllRegisteredToolsAreAllowlisted(t *testing.T) {
	registered := registeredToolNames(t)
	allowed := allowedHermitToolNames(t)

	var missing []string
	for name := range registered {
		if !allowed[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf(".claude/settings.json permissions.allow is missing %d registered MCP tool(s): %v\n"+
			"Add these entries to permissions.allow so the Superintendent loop does not stall on a confirmation prompt.",
			len(missing), missing)
	}
}
