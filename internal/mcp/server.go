package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
)

func Serve(client *gh.Client, rateLimitThreshold int) error {
	s := server.NewMCPServer("hermit", "1.0.0")
	registerTools(s, client, rateLimitThreshold)
	return server.ServeStdio(s)
}
