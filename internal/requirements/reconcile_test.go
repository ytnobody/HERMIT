package requirements

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunReconcileSweep_NoDocFound_Skips(t *testing.T) {
	root := t.TempDir()
	summary, err := RunReconcileSweep(root, "REQUIREMENTS.md", "echo test", &fakeIssueClient{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summary.Skipped {
		t.Fatalf("expected Skipped=true, got %+v", summary)
	}
	if !summary.Quiet {
		t.Errorf("expected a missing doc to be a Quiet skip, got %+v", summary)
	}
}

func TestRunReconcileSweep_NoTestCommand_Skips(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "REQUIREMENTS.md")
	if err := os.WriteFile(docPath, []byte("## REQ-001: Thing\n"), 0o644); err != nil {
		t.Fatalf("writing doc: %v", err)
	}
	summary, err := RunReconcileSweep(root, "REQUIREMENTS.md", "", &fakeIssueClient{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summary.Skipped {
		t.Fatalf("expected Skipped=true, got %+v", summary)
	}
	if summary.Quiet {
		t.Errorf("expected a missing test_command to be a non-Quiet skip (worth logging), got %+v", summary)
	}
}

// TestRunReconcileSweep_RunsAndOpensIssues exercises RunReconcileSweep
// end-to-end (real doc parse, real CommandRunner, real FileHashStore, fake
// IssueClient) with a shell test_command that passes for REQ-001 and fails
// for REQ-002, so a single call covers both the "satisfied" and
// "regressed"-with-issue-opened code paths.
func TestRunReconcileSweep_RunsAndOpensIssues(t *testing.T) {
	root := t.TempDir()
	doc := "## REQ-001: Passes\n- verify: test\n\n## REQ-002: Fails\n- verify: test\n"
	if err := os.WriteFile(filepath.Join(root, "REQUIREMENTS.md"), []byte(doc), 0o644); err != nil {
		t.Fatalf("writing doc: %v", err)
	}

	issues := &fakeIssueClient{}
	summary, err := RunReconcileSweep(root, "REQUIREMENTS.md", `echo "=== RUN {req_id}"; test "{req_id}" = "REQ-001"`, issues)
	if err != nil {
		t.Fatalf("RunReconcileSweep: %v", err)
	}
	if summary.Skipped {
		t.Fatalf("expected Skipped=false, got %+v", summary)
	}
	if summary.Satisfied != 1 {
		t.Errorf("expected Satisfied=1, got %d", summary.Satisfied)
	}
	if summary.Regressed != 1 {
		t.Errorf("expected Regressed=1, got %d", summary.Regressed)
	}
	if summary.IssuesOpened != 1 {
		t.Errorf("expected IssuesOpened=1, got %d", summary.IssuesOpened)
	}
	if len(issues.created) != 1 || issues.created[0].Kind != KindRegression {
		t.Errorf("expected exactly one regression issue to be created, got %+v", issues.created)
	}
}
