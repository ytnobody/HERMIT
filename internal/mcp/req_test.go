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
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/risk"
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

// toolIOSpec describes the documented input/output shape of one MCP tool as
// recorded in HERMIT.md's "4. MCP Tool Specifications" section.
type toolIOSpec struct {
	// inputRequired/inputOptional list the documented input field names by
	// required-ness. Implementations may register additional fields beyond
	// these (e.g. multi-repo owner/repo, or fields added after the original
	// 12-tool design) — that is treated as a compatible extension, not a
	// schema violation, consistent with how REQUIREMENTS.md already
	// describes HERMIT.md as trailing (not gating) the implementation.
	inputRequired []string
	inputOptional []string
	// outputKeys lists top-level keys that must appear in a successful
	// response, when checkOutput is non-nil.
	outputKeys []string
	// checkOutput, if set, invokes the tool (via callTool with the given
	// args) and returns the decoded top-level JSON object to check
	// outputKeys against. Left nil for tools whose output is already
	// exercised by a dedicated test elsewhere in this package (e.g. merge_pr
	// by TestREQ007/TestREQ008) or that require infra unrelated to schema
	// shape (see create_worktree below).
	checkOutput func(t *testing.T, s *server.MCPServer) map[string]any
}

// TestREQ002_ToolSchemasMatchHERMITDoc verifies the second half of REQ-002's
// acceptance criteria, which TestREQ002_RequiredMCPToolsRegistered above does
// not cover: "each tool follows the input/output schema documented in
// HERMIT.md". For every required tool it checks that the registered MCP
// input schema contains (at least) the documented fields with matching
// required/optional-ness, and, where practical, that a successful call
// returns the documented top-level output keys.
//
// Issue #163: REQUIREMENTS.md's REQ-002 block was re-hashed after an
// unrelated edit, and reviewing this test against the current HERMIT.md
// surfaced four tools (list_prs, notify, review_pr, list_issues) whose
// documented schema had silently drifted from the implementation; HERMIT.md
// was corrected to match the implementation for those four. get_config's
// long-known owner/repo gap remains out of scope here (tracked by REQ-011).
func TestREQ002_ToolSchemasMatchHERMITDoc(t *testing.T) {
	specs := map[string]toolIOSpec{
		"list_issues": {
			inputOptional: []string{"label"},
		},
		"assign_issue": {
			inputRequired: []string{"issue_number", "assignee"},
			outputKeys:    []string{"success"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "assign_issue", map[string]any{
					"issue_number": float64(1),
					"assignee":     "someone",
				}))
			},
		},
		"create_worktree": {
			inputRequired: []string{"issue_number", "base_branch"},
			outputKeys:    []string{"worktree_path", "branch"},
			checkOutput:   checkCreateWorktreeOutput,
		},
		"evaluate_risk": {
			inputRequired: []string{"pr_number"},
			outputKeys:    []string{"level", "reasons"},
			checkOutput: func(t *testing.T, _ *server.MCPServer) map[string]any {
				mock := &mockGithubClient{prStatus: &gh.PRStatus{Number: 1}}
				s2 := newTestServer(t, mock)
				return mustToolJSON(t, callTool(t, s2, "evaluate_risk", map[string]any{"pr_number": float64(1)}))
			},
		},
		"merge_pr": {
			inputRequired: []string{"pr_number"},
			inputOptional: []string{"worktree_path", "branch"},
			// Output (merged/reason) is already exercised by
			// TestREQ007_MergePR_CIGatingAndHighRiskRejection and
			// TestREQ008_MergePR_WorktreeCleanup.
		},
		"add_issue_comment": {
			inputRequired: []string{"issue_number", "body"},
			outputKeys:    []string{"success"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "add_issue_comment", map[string]any{
					"issue_number": float64(1),
					"body":         "hi",
				}))
			},
		},
		"close_issue": {
			inputRequired: []string{"issue_number"},
			outputKeys:    []string{"success"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "close_issue", map[string]any{
					"issue_number": float64(1),
				}))
			},
		},
		"list_prs": {
			inputOptional: []string{"issue_number"},
		},
		"get_lessons": {
			outputKeys: []string{"lessons"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "get_lessons", map[string]any{}))
			},
		},
		"get_config": {
			// owner/repo are documented but intentionally not returned; see
			// REQ-011 and the 現状把握サマリ gap table in REQUIREMENTS.md.
			outputKeys: []string{"max_engineers", "loop_interval"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "get_config", map[string]any{}))
			},
		},
		"review_pr": {
			inputRequired: []string{"pr_number"},
			outputKeys:    []string{"pr_number", "comment_posted"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "review_pr", map[string]any{"pr_number": float64(1)}))
			},
		},
		"notify": {
			inputRequired: []string{"event", "message"},
			outputKeys:    []string{"sent", "event"},
			checkOutput: func(t *testing.T, s *server.MCPServer) map[string]any {
				return mustToolJSON(t, callTool(t, s, "notify", map[string]any{
					"event":   "issue_assigned",
					"message": "hello",
				}))
			},
		},
	}

	s := newTestServer(t, &mockGithubClient{})
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			st := s.GetTool(name)
			if st == nil {
				t.Fatalf("tool %q is not registered", name)
			}
			props := st.Tool.InputSchema.Properties
			required := map[string]bool{}
			for _, r := range st.Tool.InputSchema.Required {
				required[r] = true
			}
			for _, field := range spec.inputRequired {
				if _, ok := props[field]; !ok {
					t.Errorf("documented required input %q is not registered", field)
				} else if !required[field] {
					t.Errorf("documented required input %q is registered but not marked required", field)
				}
			}
			for _, field := range spec.inputOptional {
				if _, ok := props[field]; !ok {
					t.Errorf("documented optional input %q is not registered", field)
				} else if required[field] {
					t.Errorf("documented optional input %q is registered as required", field)
				}
			}

			if spec.checkOutput == nil {
				return
			}
			got := spec.checkOutput(t, s)
			for _, key := range spec.outputKeys {
				if _, ok := got[key]; !ok {
					t.Errorf("documented output key %q missing from response %v", key, got)
				}
			}
		})
	}
}

// mustToolJSON decodes a successful tool result's text content into a
// top-level JSON object for output-key assertions.
func mustToolJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal error: %v (text: %s)", err, tc.Text)
	}
	return got
}

// checkCreateWorktreeOutput exercises create_worktree end-to-end against a
// throwaway git repository (git.CreateWorktree shells out to git against the
// process's current working directory), verifying the response contains the
// documented worktree_path/branch keys.
func checkCreateWorktreeOutput(t *testing.T, _ *server.MCPServer) map[string]any {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	gitIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn("init", "-b", "main")
	gitIn("config", "user.email", "hermit-test@example.com")
	gitIn("config", "user.name", "hermit-test")
	gitIn("commit", "--allow-empty", "-m", "init")

	// CreateWorktree runs git against the process working directory.
	t.Chdir(repo)

	srv := server.NewMCPServer("hermit-test", "0.0.0")
	registerTools(srv, &mockGithubClient{}, 0, t.TempDir(), "req002schema/gh-test", 120, "", "", nil, "", readiness.DefaultConfig(), risk.DefaultConfig(), nil, ModelConfig{}, RequirementsConfig{}, 4)

	got := mustToolJSON(t, callTool(t, srv, "create_worktree", map[string]any{
		"issue_number": float64(163),
		"base_branch":  "main",
	}))

	if wt, _ := got["worktree_path"].(string); wt != "" {
		t.Cleanup(func() {
			_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", wt).Run()
		})
	}
	return got
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

// TestREQ011_GetConfig_ReturnsMaxEngineers verifies REQ-011: get_config
// reports the [agent].max_engineers value from harness.toml so the
// Superintendent can look up the configured parallel-Engineer cap via MCP
// instead of relying on a hardcoded number (the CLAUDE.md template already
// references {{ .MaxEngineers }} at render time; this covers the runtime
// half of the acceptance criteria).
func TestREQ011_GetConfig_ReturnsMaxEngineers(t *testing.T) {
	s := newTestServerWithMaxEngineers(t, &mockGithubClient{}, 7)

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
	maxEngineers, ok := got["max_engineers"].(float64)
	if !ok {
		t.Fatalf("expected max_engineers in get_config response, got %v", got)
	}
	if maxEngineers != 7 {
		t.Errorf("max_engineers = %v, want 7", maxEngineers)
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
