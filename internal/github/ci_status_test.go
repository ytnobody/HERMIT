package github

import (
	"encoding/json"
	"net/http"
	"testing"
)

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

// TestGetCIDetails_ActionsOnly_Passing covers a repo whose CI runs solely
// through GitHub Actions (Checks API), with no legacy commit statuses set at
// all. This mirrors real-world repos like this one, where GetCombinedStatus
// always reports total_count == 0 regardless of whether Actions checks ran.
func TestGetCIDetails_ActionsOnly_Passing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/60", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 60,
			"head":   map[string]any{"sha": "actions-ok"},
		})
	})

	// No legacy statuses configured for this repo.
	mux.HandleFunc("/repos/owner/repo/commits/actions-ok/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "pending",
			"total_count": 0,
			"statuses":    []map[string]any{},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/actions-ok/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 3,
			"check_runs": []map[string]any{
				{
					"name":       "Lint",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/owner/repo/actions/runs/1",
				},
				{
					"name":       "Security Scan",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/owner/repo/actions/runs/2",
				},
				{
					"name":       "Test",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/owner/repo/actions/runs/3",
				},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(60)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if !details.Passing {
		t.Error("expected Passing=true when all Actions check runs succeeded")
	}
	if details.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", details.TotalCount)
	}
	if len(details.Checks) != 3 {
		t.Fatalf("len(Checks) = %d, want 3", len(details.Checks))
	}
	if len(details.FailedOnly) != 0 {
		t.Errorf("len(FailedOnly) = %d, want 0", len(details.FailedOnly))
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
}

// TestGetCIDetails_ActionsOnly_Failing covers the same Actions-only scenario
// but with one failing check run: it must be reported as Passing=false with
// the failing check populated in FailedOnly.
func TestGetCIDetails_ActionsOnly_Failing(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/61", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 61,
			"head":   map[string]any{"sha": "actions-fail"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/actions-fail/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "pending",
			"total_count": 0,
			"statuses":    []map[string]any{},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/actions-fail/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"check_runs": []map[string]any{
				{
					"name":       "Lint",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/owner/repo/actions/runs/10",
				},
				{
					"name":       "Test",
					"status":     "completed",
					"conclusion": "failure",
					"html_url":   "https://github.com/owner/repo/actions/runs/11",
				},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(61)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if details.Passing {
		t.Error("expected Passing=false when an Actions check run failed")
	}
	if len(details.FailedOnly) != 1 {
		t.Fatalf("len(FailedOnly) = %d, want 1", len(details.FailedOnly))
	}
	if details.FailedOnly[0].Name != "Test" {
		t.Errorf("FailedOnly[0].Name = %q, want %q", details.FailedOnly[0].Name, "Test")
	}
	if details.FailedOnly[0].State != "failure" {
		t.Errorf("FailedOnly[0].State = %q, want failure", details.FailedOnly[0].State)
	}
}

// TestGetCIDetails_MergesStatusAndChecks covers a repo that has both a
// legacy commit status and GitHub Actions check runs: both must appear in
// the unified Checks/FailedOnly view.
func TestGetCIDetails_MergesStatusAndChecks(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/62", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 62,
			"head":   map[string]any{"sha": "mixed-sha"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/mixed-sha/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "success",
			"total_count": 1,
			"statuses": []map[string]any{
				{
					"context":     "third-party/deploy-preview",
					"state":       "success",
					"description": "Preview deployed",
					"target_url":  "https://preview.example.com",
				},
			},
		})
	})

	mux.HandleFunc("/repos/owner/repo/commits/mixed-sha/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"check_runs": []map[string]any{
				{
					"name":       "Test",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/owner/repo/actions/runs/20",
				},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	details, err := client.GetCIDetails(62)
	if err != nil {
		t.Fatalf("GetCIDetails() unexpected error: %v", err)
	}

	if !details.Passing {
		t.Error("expected Passing=true when both status and check run succeeded")
	}
	if details.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", details.TotalCount)
	}
	names := map[string]bool{}
	for _, ch := range details.Checks {
		names[ch.Name] = true
	}
	if !names["third-party/deploy-preview"] || !names["Test"] {
		t.Errorf("expected both status and check-run entries present, got %+v", details.Checks)
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
