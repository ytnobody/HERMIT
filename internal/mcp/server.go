package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
)

func Serve(client *gh.Client, rateLimitThreshold int, rootDir string) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold, rootDir)
	return server.ServeStdio(s)
}
