package github

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestGetIssueComments_All(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/10/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         101,
				"body":       "First comment",
				"created_at": "2024-01-01T00:00:00Z",
				"updated_at": "2024-01-01T01:00:00Z",
				"user":       map[string]any{"login": "alice"},
			},
			{
				"id":         102,
				"body":       "Second comment",
				"created_at": "2024-01-02T00:00:00Z",
				"updated_at": "2024-01-02T01:00:00Z",
				"user":       map[string]any{"login": "bob"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetIssueComments(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != 101 {
		t.Errorf("comments[0].ID = %d, want 101", comments[0].ID)
	}
	if comments[0].Body != "First comment" {
		t.Errorf("comments[0].Body = %q, want %q", comments[0].Body, "First comment")
	}
	if comments[0].Author != "alice" {
		t.Errorf("comments[0].Author = %q, want %q", comments[0].Author, "alice")
	}
	if comments[1].ID != 102 {
		t.Errorf("comments[1].ID = %d, want 102", comments[1].ID)
	}
	if comments[1].Author != "bob" {
		t.Errorf("comments[1].Author = %q, want %q", comments[1].Author, "bob")
	}
}

func TestGetIssueComments_WithSince(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/5/comments", func(w http.ResponseWriter, r *http.Request) {
		// Verify the since query parameter is forwarded
		since := r.URL.Query().Get("since")
		if since == "" {
			t.Error("expected since query parameter to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         200,
				"body":       "New comment",
				"created_at": "2024-06-01T12:00:00Z",
				"updated_at": "2024-06-01T12:00:00Z",
				"user":       map[string]any{"login": "carol"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	since := "2024-06-01T00:00:00Z"
	comments, err := client.GetIssueComments(5, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Author != "carol" {
		t.Errorf("comments[0].Author = %q, want %q", comments[0].Author, "carol")
	}
}

func TestGetIssueComments_InvalidSince(t *testing.T) {
	mux := http.NewServeMux()
	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetIssueComments(1, "not-a-timestamp")
	if err == nil {
		t.Error("expected error for invalid since timestamp")
	}
}

func TestGetIssueComments_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/99/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetIssueComments(99, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestGetIssueComments_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetIssueComments(3, "")
	if err == nil {
		t.Error("expected error from API failure, got nil")
	}
}

func TestGetIssueComments_TimestampFormat(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         300,
				"body":       "Check timestamps",
				"created_at": "2024-03-15T10:30:00Z",
				"updated_at": "2024-03-16T11:45:00Z",
				"user":       map[string]any{"login": "dave"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetIssueComments(7, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	// Verify timestamps are valid RFC3339
	if _, err := time.Parse(time.RFC3339, comments[0].CreatedAt); err != nil {
		t.Errorf("CreatedAt is not valid RFC3339: %q", comments[0].CreatedAt)
	}
	if _, err := time.Parse(time.RFC3339, comments[0].UpdatedAt); err != nil {
		t.Errorf("UpdatedAt is not valid RFC3339: %q", comments[0].UpdatedAt)
	}
}
