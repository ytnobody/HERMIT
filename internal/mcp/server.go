package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/risk"
)

// Serve starts the HERMIT MCP server. defaultRiskConfig is the effective
// risk policy applied when a tool call does not target a repo with its own
// override; repoRiskConfigs maps "owner/repo" to a per-repo override (used
// in multi-repo mode). Pass risk.DefaultConfig() and a nil map to use the
// built-in hardcoded policy.
func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval, webhookURL, webhookType, repos, triggerComment, defaultRiskConfig, repoRiskConfigs)
	return server.ServeStdio(s)
}
