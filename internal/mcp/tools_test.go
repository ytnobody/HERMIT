package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/risk"
)

// wellSpecifiedBody is a body long enough, and containing an acceptance
// criteria section, to satisfy the default readiness config — so that tests
// unrelated to readiness are not accidentally affected by it.
const wellSpecifiedBody = `## Background
This describes a well-specified piece of work with enough detail to start.

## Acceptance Criteria
- Thing one works
- Thing two works
`

type postedComment struct {
	number int
	body   string
	owner  string
	repo   string
}

type addedLabel struct {
	number int
	label  string
	owner  string
	repo   string
}

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

	// hearingMatchResult, keyed by issue number, controls the result of
	// HasCommentMatchingInRepo (used to check whether a readiness hearing
	// comment was already posted on a given issue).
	hearingMatchResult map[int]bool
	hearingMatchErr    error

	postedComments []postedComment
	addedLabels    []addedLabel
	addLabelErr    error
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

func (m *mockGithubClient) PostCommentInRepo(number int, body, owner, repo string) error {
	m.postedComments = append(m.postedComments, postedComment{number: number, body: body, owner: owner, repo: repo})
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

func (m *mockGithubClient) HasCommentMatchingInRepo(number int, _ string, _, _ string) (bool, error) {
	if m.hearingMatchErr != nil {
		return false, m.hearingMatchErr
	}
	return m.hearingMatchResult[number], nil
}

func (m *mockGithubClient) AddLabelInRepo(number int, label, owner, repo string) error {
	if m.addLabelErr != nil {
		return m.addLabelErr
	}
	m.addedLabels = append(m.addedLabels, addedLabel{number: number, label: label, owner: owner, repo: repo})
	return nil
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
	return newTestServerWithReadiness(t, client, "", readiness.DefaultConfig())
}

func newTestServerWithTrigger(t *testing.T, client githubClient, trigger string) *server.MCPServer {
	t.Helper()
	return newTestServerWithReadiness(t, client, trigger, readiness.DefaultConfig())
}

func newTestServerWithReadiness(t *testing.T, client githubClient, trigger string, readinessCfg readiness.Config) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, trigger, readinessCfg, risk.DefaultConfig(), nil, ModelConfig{})
	return s
}

// newTestServerWithRisk is like newTestServer but allows tests to supply a
// custom default risk.Config and per-repo overrides, exercising the
// configurable risk policy end-to-end through the MCP tool layer.
func newTestServerWithRisk(t *testing.T, client githubClient, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, "", readiness.DefaultConfig(), defaultRiskConfig, repoRiskConfigs, ModelConfig{})
	return s
}

func newTestServerWithModel(t *testing.T, client githubClient, model ModelConfig) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, "", readiness.DefaultConfig(), risk.DefaultConfig(), nil, model)
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
		{Number: 1, Title: "issue one", Body: wellSpecifiedBody},
		{Number: 2, Title: "issue two", Body: wellSpecifiedBody},
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
		{Number: 1, Title: "issue one", Body: wellSpecifiedBody},
		{Number: 2, Title: "issue two", Body: wellSpecifiedBody},
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
		{Number: 1, Title: "issue one", Body: wellSpecifiedBody},
		{Number: 2, Title: "issue two", Body: wellSpecifiedBody},
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
		{Number: 1, Title: "issue one", Body: wellSpecifiedBody},
	}
	mock := &mockGithubClient{issues: issues, commentMatchErr: errors.New("api error")}
	s := newTestServerWithTrigger(t, mock, "/hermit")

	result := callTool(t, s, "list_issues", map[string]any{})

	if !result.IsError {
		t.Fatal("expected error result when comment check fails")
	}
}


// --- get_config / risk policy exposure ---

func TestGetConfig_IncludesDefaultRiskConfig(t *testing.T) {
	s := newTestServer(t, &mockGithubClient{})

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

	loopInterval, ok := got["loop_interval"].(float64)
	if !ok || loopInterval != 120 {
		t.Errorf("expected loop_interval=120, got %v", got["loop_interval"])
	}

	riskVal, ok := got["risk"].(map[string]any)
	if !ok {
		t.Fatalf("expected risk object in get_config response, got %v", got["risk"])
	}
	if hf, ok := riskVal["high_file_threshold"].(float64); !ok || hf != 20 {
		t.Errorf("expected risk.high_file_threshold=20 (legacy default), got %v", riskVal["high_file_threshold"])
	}
	if hl, ok := riskVal["high_line_threshold"].(float64); !ok || hl != 500 {
		t.Errorf("expected risk.high_line_threshold=500 (legacy default), got %v", riskVal["high_line_threshold"])
	}
	if _, present := got["risk_overrides"]; present {
		t.Errorf("did not expect risk_overrides when no per-repo overrides are configured, got %v", got["risk_overrides"])
	}
}

// --- readiness behavior ---

func TestListIssues_readiness_unreadyIssueGetsHearingCommentAndLabel(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "well specified", Body: wellSpecifiedBody},
		{Number: 2, Title: "too thin", Body: "fix it"},
	}
	mock := &mockGithubClient{issues: issues, hearingMatchResult: map[int]bool{}}
	s := newTestServer(t, mock)

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got []gh.Issue
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("expected only issue #1 to be returned, got %+v", got)
	}

	if len(mock.postedComments) != 1 || mock.postedComments[0].number != 2 {
		t.Fatalf("expected exactly one hearing comment posted on issue #2, got %+v", mock.postedComments)
	}
	if !strings.Contains(mock.postedComments[0].body, readiness.HearingMarker) {
		t.Fatalf("expected hearing comment to contain marker, got: %s", mock.postedComments[0].body)
	}

	if len(mock.addedLabels) != 1 || mock.addedLabels[0].number != 2 || mock.addedLabels[0].label != readiness.DefaultLabel {
		t.Fatalf("expected needs-clarification label added to issue #2, got %+v", mock.addedLabels)
	}
}

func TestListIssues_readiness_idempotent_noDuplicateHearingComment(t *testing.T) {
	issues := []gh.Issue{
		{Number: 2, Title: "too thin", Body: "fix it"},
	}
	// Hearing comment was already posted on issue #2 in a previous cycle.
	mock := &mockGithubClient{issues: issues, hearingMatchResult: map[int]bool{2: true}}
	s := newTestServer(t, mock)

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	if len(mock.postedComments) != 0 {
		t.Fatalf("expected no duplicate hearing comment to be posted, got %+v", mock.postedComments)
	}
	// The label should still be (re-)applied — this is idempotent on GitHub's
	// side and ensures the issue stays excluded even if the label was
	// somehow removed without a human response.
	if len(mock.addedLabels) != 1 || mock.addedLabels[0].number != 2 {
		t.Fatalf("expected label re-applied to issue #2, got %+v", mock.addedLabels)
	}
}

func TestListIssues_readiness_excludesNeedsClarificationLabel(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "well specified", Body: wellSpecifiedBody},
		{Number: 3, Title: "already flagged", Body: "fix it", Labels: []string{readiness.DefaultLabel}},
	}
	mock := &mockGithubClient{issues: issues}
	s := newTestServer(t, mock)

	result := callTool(t, s, "list_issues", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent")
	}
	var got []gh.Issue
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("expected only issue #1 to be returned, labeled issue excluded, got %+v", got)
	}
	// The already-labeled issue should not trigger a fresh comment/label call.
	if len(mock.postedComments) != 0 {
		t.Fatalf("expected no hearing comment for already-labeled issue, got %+v", mock.postedComments)
	}
}

func TestListIssues_readiness_configurableThreshold(t *testing.T) {
	issues := []gh.Issue{
		{Number: 1, Title: "short but allowed", Body: "0123456789"},
	}

	t.Run("lenient threshold allows short body", func(t *testing.T) {
		mock := &mockGithubClient{issues: issues, hearingMatchResult: map[int]bool{}}
		lenient := readiness.Config{MinBodyLength: 5, RequireAcceptanceCriteria: false, Label: readiness.DefaultLabel}
		s := newTestServerWithReadiness(t, mock, "", lenient)

		result := callTool(t, s, "list_issues", map[string]any{})
		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.Content)
		}
		tc, ok := result.Content[0].(mcp.TextContent)
		if !ok {
			t.Fatalf("expected TextContent")
		}
		var got []gh.Issue
		if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected issue to pass under lenient threshold, got %+v", got)
		}
	})

	t.Run("strict threshold rejects the same body", func(t *testing.T) {
		mock := &mockGithubClient{issues: issues, hearingMatchResult: map[int]bool{}}
		strict := readiness.Config{MinBodyLength: 1000, RequireAcceptanceCriteria: false, Label: readiness.DefaultLabel}
		s := newTestServerWithReadiness(t, mock, "", strict)

		result := callTool(t, s, "list_issues", map[string]any{})
		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.Content)
		}
		tc, ok := result.Content[0].(mcp.TextContent)
		if !ok {
			t.Fatalf("expected TextContent")
		}
		if tc.Text != "null" {
			var got []gh.Issue
			if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("expected issue to be rejected under strict threshold, got %+v", got)
			}
		}
		if len(mock.postedComments) != 1 {
			t.Fatalf("expected hearing comment posted under strict threshold, got %+v", mock.postedComments)
		}
	})
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

func TestGetConfig_IncludesRiskOverrides(t *testing.T) {
	overrides := map[string]risk.Config{
		"myorg/frontend": {HighFileThreshold: 3, HighLineThreshold: 50, HighPaths: []string{"src/"}},
	}
	s := newTestServerWithRisk(t, &mockGithubClient{}, risk.DefaultConfig(), overrides)

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

	riskOverrides, ok := got["risk_overrides"].(map[string]any)
	if !ok {
		t.Fatalf("expected risk_overrides object in get_config response, got %v", got["risk_overrides"])
	}
	frontend, ok := riskOverrides["myorg/frontend"].(map[string]any)
	if !ok {
		t.Fatalf("expected myorg/frontend entry in risk_overrides, got %v", riskOverrides)
	}
	if hf, ok := frontend["high_file_threshold"].(float64); !ok || hf != 3 {
		t.Errorf("expected myorg/frontend high_file_threshold=3, got %v", frontend["high_file_threshold"])
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

// --- evaluate_risk / merge_pr: configurable policy + per-repo overrides ---

func TestEvaluateRisk_UsesConfiguredDefaultPolicy(t *testing.T) {
	// A custom default policy where "docs/" is high-risk (not so under the
	// hardcoded legacy default) and the file/line thresholds are lowered.
	customDefault := risk.Config{
		HighPaths:           []string{"docs/"},
		MediumPaths:         []string{},
		HighFileThreshold:   999,
		HighLineThreshold:   999,
		MediumFileThreshold: 999,
		MediumLineThreshold: 999,
	}
	status := &gh.PRStatus{
		Files:     []gh.PRFile{{Filename: "docs/readme.md"}},
		Additions: 1,
		Deletions: 0,
		CIPassing: true,
	}
	s := newTestServerWithRisk(t, &mockGithubClient{prStatus: status}, customDefault, nil)

	result := callTool(t, s, "evaluate_risk", map[string]any{"pr_number": float64(1)})
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
	if got["level"] != "HIGH" {
		t.Errorf("expected HIGH (docs/ configured as a high-risk path), got %v", got["level"])
	}
}

func TestEvaluateRisk_PerRepoOverrideAppliesOnlyToMatchingRepo(t *testing.T) {
	// Default policy: nothing is risky (thresholds effectively disabled).
	defaultCfg := risk.Config{HighFileThreshold: 999, HighLineThreshold: 999, MediumFileThreshold: 999, MediumLineThreshold: 999}
	// myorg/frontend gets a strict override: any file under "src/" is HIGH.
	overrides := map[string]risk.Config{
		"myorg/frontend": {HighPaths: []string{"src/"}, HighFileThreshold: 999, HighLineThreshold: 999, MediumFileThreshold: 999, MediumLineThreshold: 999},
	}
	status := &gh.PRStatus{
		Files:     []gh.PRFile{{Filename: "src/main.py"}},
		Additions: 1,
		Deletions: 0,
		CIPassing: true,
	}
	s := newTestServerWithRisk(t, &mockGithubClient{prStatus: status}, defaultCfg, overrides)

	// Matching repo: override applies -> HIGH.
	result := callTool(t, s, "evaluate_risk", map[string]any{
		"pr_number": float64(1), "owner": "myorg", "repo": "frontend",
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got["level"] != "HIGH" {
		t.Errorf("myorg/frontend: expected HIGH via repo override, got %v", got["level"])
	}

	// A different repo not present in overrides: falls back to the
	// (non-risky) default policy -> LOW.
	result2 := callTool(t, s, "evaluate_risk", map[string]any{
		"pr_number": float64(1), "owner": "myorg", "repo": "backend",
	})
	var got2 map[string]any
	if err := json.Unmarshal([]byte(result2.Content[0].(mcp.TextContent).Text), &got2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got2["level"] != "LOW" {
		t.Errorf("myorg/backend: expected LOW (no override, non-risky default), got %v", got2["level"])
	}

	// No owner/repo (primary repo): also falls back to the default policy.
	result3 := callTool(t, s, "evaluate_risk", map[string]any{"pr_number": float64(1)})
	var got3 map[string]any
	if err := json.Unmarshal([]byte(result3.Content[0].(mcp.TextContent).Text), &got3); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got3["level"] != "LOW" {
		t.Errorf("primary repo (no owner/repo): expected LOW, got %v", got3["level"])
	}
}

func TestMergePR_UsesPerRepoRiskOverrideForHighRiskComment(t *testing.T) {
	defaultCfg := risk.Config{HighFileThreshold: 999, HighLineThreshold: 999, MediumFileThreshold: 999, MediumLineThreshold: 999}
	overrides := map[string]risk.Config{
		"myorg/frontend": {HighPaths: []string{"src/"}, HighFileThreshold: 999, HighLineThreshold: 999, MediumFileThreshold: 999, MediumLineThreshold: 999},
	}
	status := &gh.PRStatus{
		Files:     []gh.PRFile{{Filename: "src/main.py"}},
		Additions: 1,
		Deletions: 0,
		CIPassing: true,
	}
	mock := &mockGithubClient{prStatus: status}
	s := newTestServerWithRisk(t, mock, defaultCfg, overrides)

	result := callTool(t, s, "merge_pr", map[string]any{
		"pr_number": float64(1), "owner": "myorg", "repo": "frontend",
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if merged, ok := got["merged"].(bool); !ok || !merged {
		t.Errorf("expected merged=true, got %v", got["merged"])
	}
}
