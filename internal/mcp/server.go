package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
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

func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, model ModelConfig) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval, webhookURL, webhookType, repos, triggerComment, model)
	return server.ServeStdio(s)
}
