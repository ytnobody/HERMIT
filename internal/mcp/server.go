package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/readiness"
)

func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, readinessCfg readiness.Config) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval, webhookURL, webhookType, repos, triggerComment, readinessCfg)
	return server.ServeStdio(s)
}
