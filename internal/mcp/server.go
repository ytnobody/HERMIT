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

// RequirementsConfig carries the resolved [requirements] section of
// harness.toml needed by the run_requirements_sweep MCP tool (Issue #128):
// Doc is the already-default-resolved requirements-document path (relative
// to rootDir) and TestCommand is the [requirements].test_command template.
// Callers are expected to have already applied any default-doc-path
// fallback (e.g. cmd/hermit/main.go's resolveRequirementsDoc) before
// constructing this value, mirroring how ModelConfig expects
// backward-compatibility fallbacks to already be applied.
type RequirementsConfig struct {
	Doc         string
	TestCommand string
}

// Serve starts the HERMIT MCP server. defaultRiskConfig is the effective
// risk policy applied when a tool call does not target a repo with its own
// override; repoRiskConfigs maps "owner/repo" to a per-repo override (used
// in multi-repo mode). Pass risk.DefaultConfig() and a nil map to use the
// built-in hardcoded policy. maxEngineers is the resolved (default-applied)
// [agent].max_engineers value from harness.toml (REQ-011): the maximum
// number of Engineers the Superintendent spawns in parallel per pass. It is
// surfaced read-only via get_config so the Superintendent can reference the
// configured value instead of a hardcoded number.
func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, readinessCfg readiness.Config, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config, model ModelConfig, requirementsCfg RequirementsConfig, maxEngineers int) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval, webhookURL, webhookType, repos, triggerComment, readinessCfg, defaultRiskConfig, repoRiskConfigs, model, requirementsCfg, maxEngineers)
	return server.ServeStdio(s)
}
