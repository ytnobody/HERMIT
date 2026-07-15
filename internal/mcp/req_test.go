package mcp

// REQ-named tests for the requirements reconcile sweep (Issue #152).
//
// Each TestREQxxx_* function verifies the acceptance criteria of the
// corresponding "## REQ-xxx:" block in REQUIREMENTS.md, as thin wrappers
// around the behavior already covered in more detail by the regular tests in
// this package. The sweep's [requirements].test_command in harness.toml
// selects them by the "TestREQ<id>" prefix (REQ-002 -> ^TestREQ002).

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	gh "github.com/ytnobody/hermit/internal/github"
)

// TestREQ002_RequiredMCPToolsRegistered verifies REQ-002: the MCP server
// registers at least the 12 tools required by HERMIT.md.
func TestREQ002_RequiredMCPToolsRegistered(t *testing.T) {
	required := []string{
		"list_issues",
		"assign_issue",
		"create_worktree",
		"evaluate_risk",
		"merge_pr",
		"add_issue_comment",
		"close_issue",
		"list_prs",
		"get_lessons",
		"get_config",
		"review_pr",
		"notify",
	}

	s := newTestServer(t, &mockGithubClient{})
	for _, name := range required {
		if s.GetTool(name) == nil {
			t.Errorf("required tool %q is not registered", name)
		}
	}
}

// TestREQ003_ListIssues_ExcludesNonQueueIssues verifies the exclusion half of
// REQ-003: Issues flagged as not workable (needs-clarification, hermit-hearing)
// are excluded from list_issues, while ready open Issues are returned. The
// label-filter half of REQ-003 is covered by TestREQ003_ListIssues_LabelFilter
// in internal/github.
func TestREQ003_ListIssues_ExcludesNonQueueIssues(t *testing.T) {
	mock := &mockGithubClient{
		issues: []gh.Issue{
			{Number: 1, Title: "ready", Body: wellSpecifiedBody},
			{Number: 2, Title: "flagged", Body: wellSpecifiedBody, Labels: []string{"needs-clarification"}},
			{Number: 3, Title: "hearing", Body: wellSpecifiedBody, Labels: []string{"hermit-hearing"}},
		},
	}
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
		t.Errorf("expected only issue #1 to be returned, got %v", got)
	}
}

// TestREQ004_AssignIssue_ReturnsSuccess verifies REQ-004: assign_issue marks
// the Issue as in-progress and returns {"success": true}.
func TestREQ004_AssignIssue_ReturnsSuccess(t *testing.T) {
	mock := &mockGithubClient{}
	s := newTestServer(t, mock)

	result := callTool(t, s, "assign_issue", map[string]any{
		"issue_number": float64(7),
		"assignee":     "someone",
	})
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
	if success, _ := got["success"].(bool); !success {
		t.Errorf("expected success=true, got %v", got)
	}
}

// TestREQ007_MergePR_CIGatingAndHighRiskRejection verifies REQ-007: merge_pr
// refuses to merge while CI is failing, and refuses HIGH risk PRs with a
// comment posted on the PR, returning merged=false with a reason in both
// cases.
func TestREQ007_MergePR_CIGatingAndHighRiskRejection(t *testing.T) {
	t.Run("CI failing", func(t *testing.T) {
		mock := &mockGithubClient{
			prStatus: &gh.PRStatus{Number: 1, CIPassing: false},
		}
		s := newTestServer(t, mock)

		got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(1)}))
		if merged, _ := got["merged"].(bool); merged {
			t.Errorf("expected merged=false while CI is failing, got %v", got)
		}
		if reason, _ := got["reason"].(string); reason != "CI failing" {
			t.Errorf("expected reason %q, got %v", "CI failing", got["reason"])
		}
		if mock.mergeCalled {
			t.Errorf("expected MergePRInRepo not to be called while CI is failing")
		}
	})

	t.Run("HIGH risk", func(t *testing.T) {
		mock := &mockGithubClient{
			prStatus: &gh.PRStatus{
				Number:    1,
				Additions: 10,
				CIPassing: true,
				Files:     []gh.PRFile{{Filename: "go.mod", Additions: 10}},
			},
		}
		s := newTestServer(t, mock)

		got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{"pr_number": float64(1)}))
		if merged, _ := got["merged"].(bool); merged {
			t.Errorf("expected merged=false for HIGH risk PR, got %v", got)
		}
		if reason, _ := got["reason"].(string); reason == "" {
			t.Errorf("expected a non-empty rejection reason, got %v", got)
		}
		if mock.mergeCalled {
			t.Errorf("expected MergePRInRepo not to be called for HIGH risk PR")
		}
		if len(mock.postedComments) == 0 {
			t.Errorf("expected a risk comment to be posted on the HIGH risk PR")
		}
	})
}

// TestREQ008_MergePR_WorktreeCleanup verifies REQ-008: when merge_pr is given
// worktree_path and branch, the worktree and branch are removed after a
// successful merge, and are left untouched when the merge fails.
func TestREQ008_MergePR_WorktreeCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn(repo, "init", "-b", "main")
	gitIn(repo, "config", "user.email", "hermit-test@example.com")
	gitIn(repo, "config", "user.name", "hermit-test")
	gitIn(repo, "commit", "--allow-empty", "-m", "init")

	// git.CloseWorktree runs git in the process working directory, so run
	// this test from inside the main repository.
	t.Chdir(repo)

	lowRiskStatus := func() *gh.PRStatus {
		return &gh.PRStatus{
			Number:    1,
			Additions: 5,
			CIPassing: true,
			Files:     []gh.PRFile{{Filename: "README.md", Additions: 5}},
		}
	}

	t.Run("removed on successful merge", func(t *testing.T) {
		wtPath := filepath.Join(t.TempDir(), "wt-ok")
		branch := "req008/issue-ok"
		gitIn(repo, "worktree", "add", "-b", branch, wtPath, "main")

		mock := &mockGithubClient{prStatus: lowRiskStatus()}
		s := newTestServer(t, mock)

		got := mergePRResult(t, callTool(t, s, "merge_pr", map[string]any{
			"pr_number":     float64(1),
			"worktree_path": wtPath,
			"branch":        branch,
		}))
		if merged, _ := got["merged"].(bool); !merged {
			t.Fatalf("expected merged=true, got %v", got)
		}
		if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
			t.Errorf("expected worktree %s to be removed after merge, stat err=%v", wtPath, err)
		}
		out, err := exec.Command("git", "-C", repo, "branch", "--list", branch).Output()
		if err != nil {
			t.Fatalf("git branch --list: %v", err)
		}
		if len(out) != 0 {
			t.Errorf("expected branch %q to be deleted after merge, got %q", branch, out)
		}
	})

	t.Run("kept when merge fails", func(t *testing.T) {
		wtPath := filepath.Join(t.TempDir(), "wt-fail")
		branch := "req008/issue-fail"
		gitIn(repo, "worktree", "add", "-b", branch, wtPath, "main")
		t.Cleanup(func() {
			gitIn(repo, "worktree", "remove", "--force", wtPath)
			gitIn(repo, "branch", "-D", branch)
		})

		mock := &mockGithubClient{
			prStatus:   lowRiskStatus(),
			mergePRErr: errMergeFailed,
		}
		s := newTestServer(t, mock)

		result := callTool(t, s, "merge_pr", map[string]any{
			"pr_number":     float64(1),
			"worktree_path": wtPath,
			"branch":        branch,
		})
		if !result.IsError {
			t.Fatalf("expected an error result when the merge fails, got %v", result.Content)
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("expected worktree %s to remain after failed merge, stat err=%v", wtPath, err)
		}
	})
}

// errMergeFailed is a sentinel merge error for TestREQ008_MergePR_WorktreeCleanup.
var errMergeFailed = errors.New("merge failed (test)")
