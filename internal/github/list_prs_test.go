package github

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestExtractIssueNumber(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		wantN int
	}{
		{name: "closes #31", text: "Closes #31", wantN: 31},
		{name: "fixes #12", text: "Fixes #12", wantN: 12},
		{name: "fix #5", text: "fix #5", wantN: 5},
		{name: "resolves #100", text: "resolves #100", wantN: 100},
		{name: "resolved #7", text: "resolved #7", wantN: 7},
		{name: "bare ref #42", text: "Related to #42 change", wantN: 42},
		{name: "close #9", text: "close #9", wantN: 9},
		{name: "closed #3", text: "closed #3", wantN: 3},
		{name: "no ref", text: "No issue reference here", wantN: 0},
		{name: "empty string", text: "", wantN: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIssueNumber(tt.text)
			if got != tt.wantN {
				t.Errorf("extractIssueNumber(%q) = %d, want %d", tt.text, got, tt.wantN)
			}
		})
	}
}


func TestListOpenPRs_All(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			http.Error(w, "unexpected state param", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 10,
				"title":  "Fix bug in parser",
				"body":   "Closes #5",
				"head":   map[string]any{"ref": "hermit/issue-5"},
			},
			{
				"number": 11,
				"title":  "Add feature #20",
				"body":   "Implements new feature",
				"head":   map[string]any{"ref": "hermit/issue-20"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	prs, err := client.ListOpenPRs(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}

	// first PR: issue detected from body
	if prs[0].PRNumber != 10 {
		t.Errorf("prs[0].PRNumber = %d, want 10", prs[0].PRNumber)
	}
	if prs[0].IssueNumber != 5 {
		t.Errorf("prs[0].IssueNumber = %d, want 5", prs[0].IssueNumber)
	}
	if prs[0].HeadBranch != "hermit/issue-5" {
		t.Errorf("prs[0].HeadBranch = %q, want %q", prs[0].HeadBranch, "hermit/issue-5")
	}

	// second PR: issue detected from title
	if prs[1].PRNumber != 11 {
		t.Errorf("prs[1].PRNumber = %d, want 11", prs[1].PRNumber)
	}
	if prs[1].IssueNumber != 20 {
		t.Errorf("prs[1].IssueNumber = %d, want 20", prs[1].IssueNumber)
	}
}

func TestListOpenPRs_FilterByIssue(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 10,
				"title":  "Fix bug",
				"body":   "Closes #5",
				"head":   map[string]any{"ref": "hermit/issue-5"},
			},
			{
				"number": 11,
				"title":  "Another fix",
				"body":   "Closes #9",
				"head":   map[string]any{"ref": "hermit/issue-9"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	prs, err := client.ListOpenPRs(9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR after filter, got %d", len(prs))
	}
	if prs[0].PRNumber != 11 {
		t.Errorf("prs[0].PRNumber = %d, want 11", prs[0].PRNumber)
	}
	if prs[0].IssueNumber != 9 {
		t.Errorf("prs[0].IssueNumber = %d, want 9", prs[0].IssueNumber)
	}
}

func TestListOpenPRs_FilterByIssue_NoMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 10,
				"title":  "Fix bug",
				"body":   "Closes #5",
				"head":   map[string]any{"ref": "hermit/issue-5"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	prs, err := client.ListOpenPRs(99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 PRs for unmatched issue, got %d", len(prs))
	}
}

func TestListOpenPRs_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	prs, err := client.ListOpenPRs(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 PRs, got %d", len(prs))
	}
}

func TestListOpenPRs_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.ListOpenPRs(0)
	if err == nil {
		t.Error("expected error from API failure, got nil")
	}
}

func TestListOpenPRs_NoIssueRef(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 42,
				"title":  "Unrelated refactor",
				"body":   "No issue reference in this PR",
				"head":   map[string]any{"ref": "feature/refactor"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	prs, err := client.ListOpenPRs(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].IssueNumber != 0 {
		t.Errorf("expected IssueNumber=0 for PR with no issue ref, got %d", prs[0].IssueNumber)
	}
}
