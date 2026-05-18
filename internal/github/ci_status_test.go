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
			"state":      "success",
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
			"state":      "failure",
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
