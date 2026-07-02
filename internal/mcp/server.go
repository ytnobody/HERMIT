package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/risk"
)

// ModelConfig carries the resolved [model] section of harness.toml so that
// get_config can report the model/reasoning-effort configured for each
// HERMIT role (Superintendent, Engineer, Analyst). Callers are expected to
// have already applied any backward-compatibility fallback (e.g. Analyst
// falling back to Superintendent when [model].analyst is unset) before
// constructing this value.
type ModelConfig struct {
	Superintendent       string
	Engineer             string
	Analyst              string
	SuperintendentEffort string
	EngineerEffort       string
	AnalystEffort        string
}

// Serve starts the HERMIT MCP server. defaultRiskConfig is the effective
// risk policy applied when a tool call does not target a repo with its own
// override; repoRiskConfigs maps "owner/repo" to a per-repo override (used
// in multi-repo mode). Pass risk.DefaultConfig() and a nil map to use the
// built-in hardcoded policy.
func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, readinessCfg readiness.Config, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config, model ModelConfig) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval, webhookURL, webhookType, repos, triggerComment, readinessCfg, defaultRiskConfig, repoRiskConfigs, model)
	return server.ServeStdio(s)
}
