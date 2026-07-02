package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ytnobody/hermit/internal/cihistory"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/lessons"
	"github.com/ytnobody/hermit/internal/readiness"
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
	issues              []gh.Issue
	issuesErr           error
	comments            []gh.IssueComment
	commentsErr         error
	prComments          []gh.PRComment
	prCommentsErr       error
	assignErr           error
	prStatus            *gh.PRStatus
	prStatusErr         error
	mergePRErr          error
	commentMatchResult  bool
	commentMatchErr     error
	commentMatchByIssue map[int]bool // optional per-number override for HasCommentMatching
	prCountForIssue     int
	prCountForIssueErr  error
	ciDetails           *gh.CIDetails

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

func (m *mockGithubClient) CountPRsForIssue(_ int) (int, error) {
	return m.prCountForIssue, m.prCountForIssueErr
}

func (m *mockGithubClient) ReviewPR(_ int) (string, error) {
	return "", nil
}

func (m *mockGithubClient) GetIssueComments(_ int, _ string) ([]gh.IssueComment, error) {
	return m.comments, m.commentsErr
}

func (m *mockGithubClient) HasCommentMatching(number int, _ string) (bool, error) {
	if m.commentMatchByIssue != nil {
		if v, ok := m.commentMatchByIssue[number]; ok {
			return v, m.commentMatchErr
		}
	}
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
	if m.ciDetails != nil {
		return m.ciDetails, nil
	}
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
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, trigger, readinessCfg, ModelConfig{})
	return s
}

func newTestServerWithModel(t *testing.T, client githubClient, model ModelConfig) *server.MCPServer {
	t.Helper()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, t.TempDir(), "hermit/issue-", 120, "", "", nil, "", readiness.DefaultConfig(), model)
	return s
}

// newTestServerWithRoot behaves like newTestServer but also returns the
// rootDir used, so tests can inspect files written under .hermit/.
func newTestServerWithRoot(t *testing.T, client githubClient) (*server.MCPServer, string) {
	t.Helper()
	root := t.TempDir()
	s := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(s, client, 0, root, "hermit/issue-", 120, "", "", nil, "", readiness.DefaultConfig(), ModelConfig{})
	return s, root
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


// --- merge_pr: real signal wiring (Issue #102) ---

func mergePRResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
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
	return got
}

func TestMergePR_AllSignalsFalse_PerfectScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus: &gh.PRStatus{Number: 1, CIPassing: true},
	}
	s, _ := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(1)}))

	if got["merged"] != true {
		t.Fatalf("expected merged=true, got %v", got)
	}
	if score, ok := got["score"].(float64); !ok || score != 100 {
		t.Errorf("expected score 100, got %v", got["score"])
	}
	if _, hasLesson := got["lesson"]; hasLesson {
		t.Errorf("expected no lesson for a perfect score, got %v", got["lesson"])
	}
}

func TestMergePR_CIWasFailing_DeductsScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus: &gh.PRStatus{Number: 5, CIPassing: true},
	}
	s, root := newTestServerWithRoot(t, mock)

	// Simulate an earlier check_ci_status call that observed CI failing.
	if err := cihistory.RecordFailure(root, 5); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(5)}))

	if score, ok := got["score"].(float64); !ok || score != 80 {
		t.Errorf("expected score 80 (100 - 20 for CI failing), got %v", got["score"])
	}
}

func TestMergePR_CIHistoryClearedAfterMerge(t *testing.T) {
	mock := &mockGithubClient{
		prStatus: &gh.PRStatus{Number: 6, CIPassing: true},
	}
	s, root := newTestServerWithRoot(t, mock)

	if err := cihistory.RecordFailure(root, 6); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}

	callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(6)})

	failing, err := cihistory.WasFailing(root, 6)
	if err != nil {
		t.Fatalf("WasFailing error: %v", err)
	}
	if failing {
		t.Error("expected CI failure history to be cleared after merge")
	}
}

func TestMergePR_HasMultiplePRs_DeductsScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus:        &gh.PRStatus{Number: 7, CIPassing: true, IssueNumber: 42},
		prCountForIssue: 2,
	}
	s, _ := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(7)}))

	if score, ok := got["score"].(float64); !ok || score != 85 {
		t.Errorf("expected score 85 (100 - 15 for multiple PRs), got %v", got["score"])
	}
}

func TestMergePR_SinglePRForIssue_NoDeduction(t *testing.T) {
	mock := &mockGithubClient{
		prStatus:        &gh.PRStatus{Number: 8, CIPassing: true, IssueNumber: 42},
		prCountForIssue: 1,
	}
	s, _ := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(8)}))

	if score, ok := got["score"].(float64); !ok || score != 100 {
		t.Errorf("expected score 100 for a single PR on the issue, got %v", got["score"])
	}
}

func TestMergePR_HasClarification_ViaPRComment_DeductsScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus:           &gh.PRStatus{Number: 9, CIPassing: true},
		commentMatchResult: true,
	}
	s, _ := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(9)}))

	if score, ok := got["score"].(float64); !ok || score != 80 {
		t.Errorf("expected score 80 (100 - 20 for clarification), got %v", got["score"])
	}
}

func TestMergePR_HasClarification_ViaIssueComment_DeductsScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus:            &gh.PRStatus{Number: 10, CIPassing: true, IssueNumber: 99},
		commentMatchResult:  false,
		commentMatchByIssue: map[int]bool{99: true}, // PR (10) has no match, but the linked issue (99) does
	}
	s, _ := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(10)}))

	if score, ok := got["score"].(float64); !ok || score != 80 {
		t.Errorf("expected score 80 (100 - 20 for clarification found on linked issue), got %v", got["score"])
	}
}

func TestMergePR_MultipleDeductions_GeneratesLesson(t *testing.T) {
	mock := &mockGithubClient{
		prStatus:        &gh.PRStatus{Number: 11, CIPassing: true, IssueNumber: 42},
		prCountForIssue: 2,
	}
	s, root := newTestServerWithRoot(t, mock)

	if err := cihistory.RecordFailure(root, 11); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(11)}))

	// 100 - 20 (CI failing) - 15 (multiple PRs) = 65, which is below the
	// lessons.GenerateLesson threshold of 70, so a lesson must be recorded.
	if score, ok := got["score"].(float64); !ok || score != 65 {
		t.Errorf("expected score 65, got %v", got["score"])
	}
	lesson, ok := got["lesson"].(string)
	if !ok || lesson == "" {
		t.Errorf("expected a non-empty lesson for a low score, got %v", got["lesson"])
	}

	saved, err := lessons.ReadLessons(root)
	if err != nil {
		t.Fatalf("ReadLessons error: %v", err)
	}
	if len(saved) != 1 || saved[0] != lesson {
		t.Errorf("expected the lesson to be persisted under .hermit/, got %v", saved)
	}
}

func TestMergePR_CIFailing_DoesNotMergeOrScore(t *testing.T) {
	mock := &mockGithubClient{
		prStatus: &gh.PRStatus{Number: 12, CIPassing: false},
	}
	s, root := newTestServerWithRoot(t, mock)

	got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(12)}))

	if got["merged"] != false {
		t.Fatalf("expected merged=false when CI is failing, got %v", got)
	}
	if _, hasScore := got["score"]; hasScore {
		t.Errorf("expected no score when the PR was not merged, got %v", got["score"])
	}

	saved, err := lessons.ReadLessons(root)
	if err != nil {
		t.Fatalf("ReadLessons error: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("expected no lesson to be recorded when the PR was not merged, got %v", saved)
	}
}

// --- check_ci_status: records CI failure history for later scoring ---

func TestCheckCIStatus_RecordsFailureHistory(t *testing.T) {
	mock := &mockGithubClient{
		ciDetails: &gh.CIDetails{
			PRNumber: 20,
			Passing:  false,
			FailedOnly: []gh.CICheckResult{
				{Name: "test", State: "failure"},
			},
		},
	}
	s, root := newTestServerWithRoot(t, mock)

	callTool(t, s, "check_ci_status", map[string]any{"pr_number": float64(20)})

	failing, err := cihistory.WasFailing(root, 20)
	if err != nil {
		t.Fatalf("WasFailing error: %v", err)
	}
	if !failing {
		t.Error("expected check_ci_status to record CI failure history")
	}
}

func TestCheckCIStatus_Passing_DoesNotRecordFailure(t *testing.T) {
	mock := &mockGithubClient{
		ciDetails: &gh.CIDetails{PRNumber: 21, Passing: true},
	}
	s, root := newTestServerWithRoot(t, mock)

	callTool(t, s, "check_ci_status", map[string]any{"pr_number": float64(21)})

	failing, err := cihistory.WasFailing(root, 21)
	if err != nil {
		t.Fatalf("WasFailing error: %v", err)
	}
	if failing {
		t.Error("expected no CI failure history to be recorded when CI is passing")
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
