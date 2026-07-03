package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// mockCheckRuns registers a handler for the GitHub Actions check-runs
// endpoint for the given owner/repo/ref, returning the provided check-run
// fixtures. Pass nil/empty runs to simulate "no check-runs configured".
func mockCheckRuns(mux *http.ServeMux, owner, repo, ref string, runs []map[string]any) {
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, ref), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": len(runs),
			"check_runs":  runs,
		})
	})
}

func TestGetCIDetails_Passing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 10,
			"head":   map[string]any{"sha": "abc123"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/abc123/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "success",
			"total_count": 2,
			"statuses": []map[string]any{
				{
					"context":     "ci/test",
					"state":       "success",
					"description": "All tests passed",
					"target_url":  "https://ci.example.com/builds/1",
				},
				{
					"context":     "ci/lint",
					"state":       "success",
					"description": "No lint errors",
					"target_url":  "https://ci.example.com/builds/2",
				},
			},
		})
	})
	mockCheckRuns(mux, "owner", "repo", "abc123", nil)

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(10)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if details.PRNumber != 10 {
		t.Errorf("PRNumber = %d, want 10", details.PRNumber)
	}
	if details.SHA != "abc123" {
		t.Errorf("SHA = %q, want %q", details.SHA, "abc123")
	}
	if !details.Passing {
		t.Error("expected Passing=true for success state")
	}
	if details.State != "success" {
		t.Errorf("State = %q, want %q", details.State, "success")
	}
	if len(details.Checks) != 2 {
		t.Errorf("len(Checks) = %d, want 2", len(details.Checks))
	}
	if len(details.FailedOnly) != 0 {
		t.Errorf("len(FailedOnly) = %d, want 0 for passing CI", len(details.FailedOnly))
	}
}

func TestGetCIDetails_Failing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/20", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 20,
			"head":   map[string]any{"sha": "def456"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/def456/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "failure",
			"total_count": 3,
			"statuses": []map[string]any{
				{
					"context":     "ci/test",
					"state":       "failure",
					"description": "2 tests failed",
					"target_url":  "https://ci.example.com/builds/10",
				},
				{
					"context":     "ci/lint",
					"state":       "success",
					"description": "No lint errors",
					"target_url":  "https://ci.example.com/builds/11",
				},
				{
					"context":     "ci/coverage",
					"state":       "error",
					"description": "Coverage check error",
					"target_url":  "https://ci.example.com/builds/12",
				},
			},
		})
	})

	mockCheckRuns(mux, "owner", "repo", "def456", nil)

	// Expect a comment to be posted when CI is failing
	commentPosted := false
	mux.HandleFunc("/repos/owner/repo/issues/20/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			commentPosted = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(20)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if details.Passing {
		t.Error("expected Passing=false for failure state")
	}
	if details.State != "failure" {
		t.Errorf("State = %q, want %q", details.State, "failure")
	}
	if len(details.Checks) != 3 {
		t.Errorf("len(Checks) = %d, want 3", len(details.Checks))
	}
	// Two checks failed: "ci/test" (failure) and "ci/coverage" (error)
	if len(details.FailedOnly) != 2 {
		t.Errorf("len(FailedOnly) = %d, want 2", len(details.FailedOnly))
	}

	_ = commentPosted // comment posting is best-effort; tested separately in tools tests
}

func TestGetCIDetails_EmptyState(t *testing.T) {
	// When no statuses are set, state is "" which means no CI configured: treated as passing.
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/30", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 30,
			"head":   map[string]any{"sha": "ghi789"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/ghi789/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":    "",
			"statuses": []map[string]any{},
		})
	})
	mockCheckRuns(mux, "owner", "repo", "ghi789", nil)

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(30)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if !details.Passing {
		t.Error("expected Passing=true when state is empty (no CI configured)")
	}
	if len(details.Checks) != 0 {
		t.Errorf("expected no checks, got %d", len(details.Checks))
	}
	if len(details.FailedOnly) != 0 {
		t.Errorf("expected no failed checks, got %d", len(details.FailedOnly))
	}
}

func TestGetCIDetails_GetPRError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/99", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetCIDetails(99)
	if err == nil {
		t.Error("expected error when PR not found")
	}
}

func TestGetCIDetails_StatusAPIError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/40", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 40,
			"head":   map[string]any{"sha": "jkl012"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/jkl012/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetCIDetails(40)
	if err == nil {
		t.Error("expected error when status API fails")
	}
}

func TestGetCIDetailsInRepo_CustomRepo(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/myorg/myrepo/pulls/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 5,
			"head":   map[string]any{"sha": "sha-custom"},
		})
	})

	mux.HandleFunc("/repos/myorg/myrepo/commits/sha-custom/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":    "success",
			"statuses": []map[string]any{},
		})
	})
	mockCheckRuns(mux, "myorg", "myrepo", "sha-custom", nil)

	client, teardown := newTestClientFor(t, mux, "myorg", "myrepo")
	defer teardown()

	details, err := client.GetCIDetailsInRepo(5, "myorg", "myrepo")
	if err != nil {
		t.Fatalf("GetCIDetailsInRepo() unexpected error: %v", err)
	}

	if details.PRNumber != 5 {
		t.Errorf("PRNumber = %d, want 5", details.PRNumber)
	}
	if details.SHA != "sha-custom" {
		t.Errorf("SHA = %q, want %q", details.SHA, "sha-custom")
	}
}

func TestGetCIDetails_CheckFields(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/50", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 50,
			"head":   map[string]any{"sha": "mno345"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/mno345/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state": "failure",
			"statuses": []map[string]any{
				{
					"context":     "ci/build",
					"state":       "failure",
					"description": "Build failed",
					"target_url":  "https://ci.example.com/builds/99",
				},
			},
		})
	})
	mockCheckRuns(mux, "owner", "repo", "mno345", nil)

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(50)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if len(details.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(details.Checks))
	}
	ch := details.Checks[0]
	if ch.Name != "ci/build" {
		t.Errorf("Name = %q, want %q", ch.Name, "ci/build")
	}
	if ch.State != "failure" {
		t.Errorf("State = %q, want %q", ch.State, "failure")
	}
	if ch.Description != "Build failed" {
		t.Errorf("Description = %q, want %q", ch.Description, "Build failed")
	}
	if ch.TargetURL != "https://ci.example.com/builds/99" {
		t.Errorf("TargetURL = %q, want %q", ch.TargetURL, "https://ci.example.com/builds/99")
	}

	if len(details.FailedOnly) != 1 {
		t.Fatalf("expected 1 failed check, got %d", len(details.FailedOnly))
	}
}

// --- GitHub Actions check-runs (Checks API) ---
//
// These tests cover repos whose CI is reported via check-runs (as produced by
// .github/workflows/*.yml) rather than legacy commit statuses. Before the
// fix, GetCombinedStatus (legacy Status API) never sees these, so
// check_ci_status always looked "pending" and merge_pr's CI gate always
// silently treated CI as passing regardless of real check-run results.

func mockEmptyStatus(mux *http.ServeMux, owner, repo, ref string) {
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, ref), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "pending",
			"total_count": 0,
			"statuses":    []map[string]any{},
		})
	})
}

func TestGetCIDetails_CheckRunsOnly_Passing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/60", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 60,
			"head":   map[string]any{"sha": "pqr678"},
		})
	})
	mockEmptyStatus(mux, "owner", "repo", "pqr678")
	mockCheckRuns(mux, "owner", "repo", "pqr678", []map[string]any{
		{"name": "Lint", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
		{"name": "Security Scan", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/2"},
		{"name": "Test", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/3"},
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(60)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}
	if !details.Passing {
		t.Error("expected Passing=true when all check-runs succeeded")
	}
	if details.State != "success" {
		t.Errorf("State = %q, want %q", details.State, "success")
	}
	if len(details.Checks) != 3 {
		t.Fatalf("expected 3 checks, got %d", len(details.Checks))
	}
	names := map[string]bool{}
	for _, ch := range details.Checks {
		names[ch.Name] = true
		if ch.State != "success" {
			t.Errorf("check %q State = %q, want success", ch.Name, ch.State)
		}
	}
	for _, want := range []string{"Lint", "Security Scan", "Test"} {
		if !names[want] {
			t.Errorf("expected check named %q to be present", want)
		}
	}
	if len(details.FailedOnly) != 0 {
		t.Errorf("expected no failed checks, got %d", len(details.FailedOnly))
	}
}

func TestGetCIDetails_CheckRunsOnly_Failing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/61", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 61,
			"head":   map[string]any{"sha": "stu901"},
		})
	})
	mockEmptyStatus(mux, "owner", "repo", "stu901")
	mockCheckRuns(mux, "owner", "repo", "stu901", []map[string]any{
		{"name": "Lint", "status": "completed", "conclusion": "success"},
		{"name": "Test", "status": "completed", "conclusion": "failure", "output": map[string]any{"summary": "2 tests failed"}},
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(61)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}
	if details.Passing {
		t.Error("expected Passing=false when a check-run failed")
	}
	if details.State != "failure" {
		t.Errorf("State = %q, want %q", details.State, "failure")
	}
	if len(details.FailedOnly) != 1 {
		t.Fatalf("expected 1 failed check, got %d", len(details.FailedOnly))
	}
	if details.FailedOnly[0].Name != "Test" {
		t.Errorf("failed check Name = %q, want %q", details.FailedOnly[0].Name, "Test")
	}
	if details.FailedOnly[0].Description != "2 tests failed" {
		t.Errorf("failed check Description = %q, want %q", details.FailedOnly[0].Description, "2 tests failed")
	}
}

func TestGetCIDetails_CheckRunsOnly_Pending(t *testing.T) {
	// A check-run still in progress must count as not-passing, but must not
	// be reported as a failure either.
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/62", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 62,
			"head":   map[string]any{"sha": "vwx234"},
		})
	})
	mockEmptyStatus(mux, "owner", "repo", "vwx234")
	mockCheckRuns(mux, "owner", "repo", "vwx234", []map[string]any{
		{"name": "Lint", "status": "completed", "conclusion": "success"},
		{"name": "Test", "status": "in_progress"},
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(62)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}
	if details.Passing {
		t.Error("expected Passing=false while a check-run is still in_progress")
	}
	if len(details.FailedOnly) != 0 {
		t.Errorf("expected 0 failed checks for a pending (not failed) check-run, got %d", len(details.FailedOnly))
	}
	for _, ch := range details.Checks {
		if ch.Name == "Test" && ch.State != "pending" {
			t.Errorf("Test check State = %q, want %q", ch.State, "pending")
		}
	}
}

func TestGetCIDetails_NoStatusesNoCheckRuns(t *testing.T) {
	// Neither legacy statuses nor check-runs present at all: fall back to
	// "passing" so repos genuinely without any CI aren't permanently blocked.
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/63", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 63,
			"head":   map[string]any{"sha": "yz1234"},
		})
	})
	mockEmptyStatus(mux, "owner", "repo", "yz1234")
	mockCheckRuns(mux, "owner", "repo", "yz1234", nil)

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(63)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}
	if !details.Passing {
		t.Error("expected Passing=true when neither statuses nor check-runs exist")
	}
	if details.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", details.TotalCount)
	}
}

// TestIsCIPassingInRepo_CheckRunFailing_LegacyStatusSuccess reproduces the
// core bug from issue #127: merge_pr's CI gate (IsCIPassingInRepo) must not
// treat CI as passing just because there are no legacy commit statuses (or
// they happen to be green) while a real GitHub Actions check-run is failing.
func TestIsCIPassingInRepo_CheckRunFailing_LegacyStatusSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/commits/sha8/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// total_count 0: this repo has no legacy commit statuses at all,
		// which is the common case for Actions-based CI.
		json.NewEncoder(w).Encode(map[string]any{"state": "pending", "total_count": 0})
	})
	mockCheckRuns(mux, "owner", "repo", "sha8", []map[string]any{
		{"name": "Test", "status": "completed", "conclusion": "failure"},
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	passing, err := client.IsCIPassingInRepo(1, "sha8", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if passing {
		t.Error("expected IsCIPassing=false when a GitHub Actions check-run failed, even with zero legacy statuses")
	}
}

// TestIsCIPassingInRepo_CheckRunsAllPassing verifies merge_pr's CI gate
// allows merging once all check-runs (with no legacy statuses present) have
// succeeded.
func TestIsCIPassingInRepo_CheckRunsAllPassing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/commits/sha9/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"state": "pending", "total_count": 0})
	})
	mockCheckRuns(mux, "owner", "repo", "sha9", []map[string]any{
		{"name": "Lint", "status": "completed", "conclusion": "success"},
		{"name": "Security Scan", "status": "completed", "conclusion": "neutral"},
		{"name": "Test", "status": "completed", "conclusion": "success"},
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	passing, err := client.IsCIPassingInRepo(1, "sha9", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !passing {
		t.Error("expected IsCIPassing=true when all check-runs succeeded (neutral counts as non-blocking)")
	}
}

func TestIsCIPassingInRepo_CheckRunsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/commits/sha10/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"state": "success", "total_count": 1})
	})
	mux.HandleFunc("/repos/owner/repo/commits/sha10/check-runs", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	passing, err := client.IsCIPassingInRepo(1, "sha10", "", "")
	if err == nil {
		t.Fatal("expected error from check-runs API failure, got nil")
	}
	if passing {
		t.Error("expected IsCIPassing=false on check-runs API error")
	}
}
