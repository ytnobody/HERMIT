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
	issues             []gh.Issue
	issuesErr          error
	comments           []gh.IssueComment
	commentsErr        error
	prComments         []gh.PRComment
	prCommentsErr      error
	assignErr          error
	prStatus           *gh.PRStatus
	prStatusErr        error
	mergePRErr         error
	commentMatchResult bool
	commentMatchErr    error
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

func (m *mockGithubClient) GetIssueComments(_ int, _ string) ([]gh.IssueComment, error) {
	return m.comments, m.commentsErr
}

func (m *mockGithubClient) HasCommentMatching(_ int, _ string) (bool, error) {
	return m.commentMatchResult, m.commentMatchErr
}

func (m *mockGithubClient) GetDefaultBranch() (string, error) {
	return "main", nil
}

func (m *mockGithubClient) GetCIDetailsInRepo(_ int, _, _ string) (*gh.CIDetails, error) {
	return &gh.CIDetails{}, nil
}

func (m *mockGithubClient) GetRecentPRComments(_ int, _ string) ([]gh.PRComment, error) {
	return m.prComments, m.prCommentsErr
}

func newTestServer(t *testing.T, client githubClient) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, "", ModelConfig{})
	return s
}

func newTestServerWithTrigger(t *testing.T, client githubClient, trigger string) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, trigger, ModelConfig{})
	return s
}

func newTestServerWithModel(t *testing.T, client githubClient, model ModelConfig) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, "", model)
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
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	gotComments, ok2 := got["comments"].([]any)
	if !ok2 {
		t.Fatalf("expected comments array in response")
	}
	if len(gotComments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(gotComments))
	}
	count, ok3 := got["count"].(float64)
	if !ok3 || count != 2 {
		t.Errorf("expected count 2, got %v", got["count"])
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
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	count, ok3 := got["count"].(float64)
	if !ok3 || count != 0 {
		t.Errorf("expected count 0, got %v", got["count"])
	}
}

func TestGetIssueComments_apiError(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{commentsErr: errors.New("api failure")})

	result := callTool(t, s, "get_issue_comments", map[string]any{"issue_number": float64(1)})

	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestGetPRComments_success(t *testing.T) {
	prComments := []gh.PRComment{
		{ID: 10, Author: "carol", Body: "looks good", Path: "main.go", CreatedAt: "2024-03-01T00:00:00Z", UpdatedAt: "2024-03-01T01:00:00Z"},
		{ID: 11, Author: "dave", Body: "nit: typo", Path: "README.md", CreatedAt: "2024-03-02T00:00:00Z", UpdatedAt: "2024-03-02T00:00:00Z"},
	}
	s := newTestServer(t, &mockGithubClient{prComments: prComments})

	result := callTool(t, s, "get_recent_pr_comments", map[string]any{"pr_number": float64(99)})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc3, ok3 := result.Content[0].(mcp.TextContent)
	if !ok3 {
		t.Fatalf("expected TextContent")
	}
	text := tc3.Text
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	comments, ok := got["comments"].([]any)
	if !ok {
		t.Fatalf("expected comments array in response")
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
	count2, ok4 := got["count"].(float64)
	if !ok4 || count2 != 2 {
		t.Errorf("expected count 2, got %v", got["count"])
	}
}

func TestGetPRComments_empty(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{prComments: []gh.PRComment{}})

	result := callTool(t, s, "get_recent_pr_comments", map[string]any{"pr_number": float64(1)})

	if result.IsError {
		t.Fatalf("expected success, got error")
	}
	tc4, ok4 := result.Content[0].(mcp.TextContent)
	if !ok4 {
		t.Fatalf("expected TextContent")
	}
	text := tc4.Text
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	count3, ok5 := got["count"].(float64)
	if !ok5 || count3 != 0 {
		t.Errorf("expected count 0, got %v", got["count"])
	}
}

func TestGetPRComments_apiError(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{prCommentsErr: errors.New("github api down")})

	result := callTool(t, s, "get_recent_pr_comments", map[string]any{"pr_number": float64(1)})

	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestListIssues_noTrigger_returnsAll(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "issue one"},
		{Number: 2, Title: "issue two"},
	}
	s := newTestServer(t, &mockGithubClient{issues: issues})

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got []any
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 issues, got %d", len(got))
	}
}

func TestListIssues_withTrigger_filtersMatched(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "issue one"},
		{Number: 2, Title: "issue two"},
	}
	// Only issue 1 has a matching comment
	mock := &mockGithubClient{issues: issues, commentMatchResult: true}
	s := newTestServerWithTrigger(t, mock, "/hermit")

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got []any
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	// All issues matched because mock returns true for all
	if len(got) != 2 {
		t.Errorf("expected 2 issues (all matched), got %d", len(got))
	}
}

func TestListIssues_withTrigger_noMatch(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "issue one"},
		{Number: 2, Title: "issue two"},
	}
	mock := &mockGithubClient{issues: issues, commentMatchResult: false}
	s := newTestServerWithTrigger(t, mock, "/hermit")

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	// When no issues match, json.Marshal(nil) returns "null"
	text := tc.Text
	if text != "null" {
		var got []any
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 issues, got %d", len(got))
		}
	}
}

func TestListIssues_withTrigger_commentCheckError(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "issue one"},
	}
	mock := &mockGithubClient{issues: issues, commentMatchErr: errors.New("api error")}
	s := newTestServerWithTrigger(t, mock, "/hermit")

	result := callTool(t, s, "list_issues", map[string]any{})

	if !result.IsError {
		t.Fatal("expected error result when comment check fails")
	}
}

// TestGetConfig_IncludesModelConfig verifies that get_config reports the
// per-role model/reasoning-effort configuration (including the Analyst role
// added in issue #107) alongside loop_interval.
func TestGetConfig_IncludesModelConfig(t *testing.T) {
	model := ModelConfig{
		Superintendent:       "claude-sonnet-5",
		Engineer:             "claude-haiku-4-5-20251001",
		Analyst:              "claude-opus-4-8",
		SuperintendentEffort: "high",
		EngineerEffort:       "low",
		AnalystEffort:        "high",
	}
	s := newTestServerWithModel(t, &mockGithubClient{}, model)

	result := callTool(t, s, "get_config", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got struct {
		LoopInterval int `json:"loop_interval"`
		Model        struct {
			Superintendent       string `json:"superintendent"`
			Engineer             string `json:"engineer"`
			Analyst              string `json:"analyst"`
			SuperintendentEffort string `json:"superintendent_effort"`
			EngineerEffort       string `json:"engineer_effort"`
			AnalystEffort        string `json:"analyst_effort"`
		} `json:"model"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.LoopInterval != 120 {
		t.Errorf("expected loop_interval=120, got %d", got.LoopInterval)
	}
	if got.Model.Analyst != "claude-opus-4-8" {
		t.Errorf("expected model.analyst=claude-opus-4-8, got %q", got.Model.Analyst)
	}
	if got.Model.AnalystEffort != "high" {
		t.Errorf("expected model.analyst_effort=high, got %q", got.Model.AnalystEffort)
	}
	if got.Model.Superintendent != "claude-sonnet-5" || got.Model.Engineer != "claude-haiku-4-5-20251001" {
		t.Errorf("unexpected model config: %+v", got.Model)
	}
}

// TestGetConfig_AnalystFallback_EmptyWhenUnresolved verifies that get_config
// simply reflects whatever ModelConfig it was constructed with — callers
// (cmd/hermit's resolveAnalystModel) are responsible for backward-compat
// fallback before constructing ModelConfig, so an unset Analyst here is
// surfaced as an empty string rather than silently defaulted inside the MCP
// layer.
func TestGetConfig_AnalystFallback_EmptyWhenUnresolved(t *testing.T) {
	s := newTestServerWithModel(t, &mockGithubClient{}, ModelConfig{Superintendent: "claude-sonnet-5"})

	result := callTool(t, s, "get_config", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	modelMap, ok := got["model"].(map[string]any)
	if !ok {
		t.Fatalf("expected model object in response")
	}
	if modelMap["analyst"] != "" {
		t.Errorf("expected analyst to be empty string when ModelConfig.Analyst is unset, got %v", modelMap["analyst"])
	}
}
