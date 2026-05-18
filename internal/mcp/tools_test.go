package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
)

type mockGithubClient struct {
	issues      []gh.Issue
	issuesErr   error
	comments    []gh.IssueComment
	commentsErr error
	assignErr   error
	prStatus    *gh.PRStatus
	prStatusErr error
	mergePRErr  error
}

func (m *mockGithubClient) CheckRateLimit(_ int) error { return nil }

func (m *mockGithubClient) ListOpenIssues(_ string) ([]gh.Issue, error) {
	return m.issues, m.issuesErr
}

func (m *mockGithubClient) ListAllIssues(_ []gh.RepoConfig) ([]gh.Issue, error) {
	return m.issues, m.issuesErr
}

func (m *mockGithubClient) AssignIssue(_ int, _ string) error {
	return m.assignErr
}

func (m *mockGithubClient) AssignIssueInRepo(_ int, _, _, _ string) error {
	return m.assignErr
}

func (m *mockGithubClient) GetPRStatus(_ int) (*gh.PRStatus, error) {
	return m.prStatus, m.prStatusErr
}

func (m *mockGithubClient) GetPRStatusInRepo(_ int, _, _ string) (*gh.PRStatus, error) {
	return m.prStatus, m.prStatusErr
}

func (m *mockGithubClient) PostComment(_ int, _ string) error {
	return nil
}

func (m *mockGithubClient) PostCommentInRepo(_ int, _, _, _ string) error {
	return nil
}

func (m *mockGithubClient) MergePR(_ int) error {
	return m.mergePRErr
}

func (m *mockGithubClient) MergePRInRepo(_ int, _, _ string) error {
	return m.mergePRErr
}

func (m *mockGithubClient) CloseIssue(_ int, _ string) error {
	return nil
}

func (m *mockGithubClient) ListOpenPRs(_ int) ([]gh.PRInfo, error) {
	return nil, nil
}

func (m *mockGithubClient) ReviewPR(_ int) (string, error) {
	return "", nil
}

func (m *mockGithubClient) GetIssueComments(_ int) ([]gh.IssueComment, error) {
	return m.comments, m.commentsErr
}

func newTestServer(t *testing.T, client githubClient) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil)
	return s
}

func callTool(t *testing.T, s *server.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	st := s.GetTool(name)
	if st == nil {
		t.Fatalf("tool %q not registered", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := st.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler(%s) error: %v", name, err)
	}
	return result
}

func TestGetIssueComments_success(t *testing.T) {
	comments := []gh.IssueComment{
		{ID: 1, Author: "alice", Body: "hello", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: 2, Author: "bob", Body: "world", CreatedAt: "2024-01-02T00:00:00Z"},
	}
	s := newTestServer(t, &mockGithubClient{comments: comments})

	result := callTool(t, s, "get_issue_comments", map[string]any{"issue_number": float64(42)})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	text := tc.Text
	var got []gh.IssueComment
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 comments, got %d", len(got))
	}
	if got[0].Author != "alice" || got[1].Author != "bob" {
		t.Errorf("unexpected authors: %+v", got)
	}
}

func TestGetIssueComments_empty(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{comments: []gh.IssueComment{}})

	result := callTool(t, s, "get_issue_comments", map[string]any{"issue_number": float64(1)})

	if result.IsError {
		t.Fatalf("expected success, got error")
	}
	tc2, ok2 := result.Content[0].(mcp.TextContent)
	if !ok2 {
		t.Fatalf("expected TextContent")
	}
	text := tc2.Text
	var got []gh.IssueComment
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 comments, got %d", len(got))
	}
}

func TestGetIssueComments_apiError(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{commentsErr: errors.New("api failure")})

	result := callTool(t, s, "get_issue_comments", map[string]any{"issue_number": float64(1)})

	if !result.IsError {
		t.Fatal("expected error result")
	}
}
