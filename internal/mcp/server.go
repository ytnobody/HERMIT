package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
)

func Serve(client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir, branchPrefix, loopInterval)
	return server.ServeStdio(s)
}
