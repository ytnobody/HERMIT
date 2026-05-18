package github

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestGetRecentPRComments_All(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/10/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         201,
				"body":       "Looks good",
				"path":       "main.go",
				"created_at": "2024-03-01T00:00:00Z",
				"updated_at": "2024-03-01T01:00:00Z",
				"user":       map[string]any{"login": "alice"},
			},
			{
				"id":         202,
				"body":       "Nit: rename this",
				"path":       "internal/foo.go",
				"created_at": "2024-03-02T00:00:00Z",
				"updated_at": "2024-03-02T02:00:00Z",
				"user":       map[string]any{"login": "bob"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetRecentPRComments(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != 201 {
		t.Errorf("comments[0].ID = %d, want 201", comments[0].ID)
	}
	if comments[0].Author != "alice" {
		t.Errorf("comments[0].Author = %q, want %q", comments[0].Author, "alice")
	}
	if comments[0].Path != "main.go" {
		t.Errorf("comments[0].Path = %q, want %q", comments[0].Path, "main.go")
	}
	if comments[1].ID != 202 {
		t.Errorf("comments[1].ID = %d, want 202", comments[1].ID)
	}
	if comments[1].Author != "bob" {
		t.Errorf("comments[1].Author = %q, want %q", comments[1].Author, "bob")
	}
}

func TestGetRecentPRComments_WithSince(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/5/comments", func(w http.ResponseWriter, r *http.Request) {
		since := r.URL.Query().Get("since")
		if since == "" {
			t.Error("expected since query parameter to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         300,
				"body":       "New review comment",
				"path":       "cmd/main.go",
				"created_at": "2024-06-01T12:00:00Z",
				"updated_at": "2024-06-01T12:00:00Z",
				"user":       map[string]any{"login": "carol"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	since := "2024-06-01T00:00:00Z"
	comments, err := client.GetRecentPRComments(5, since)
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

func TestGetRecentPRComments_InvalidSince(t *testing.T) {
	mux := http.NewServeMux()
	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetRecentPRComments(1, "not-a-timestamp")
	if err == nil {
		t.Error("expected error for invalid since timestamp")
	}
}

func TestGetRecentPRComments_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/99/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetRecentPRComments(99, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestGetRecentPRComments_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/3/comments", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.GetRecentPRComments(3, "")
	if err == nil {
		t.Error("expected error from API failure, got nil")
	}
}

func TestGetRecentPRComments_TimestampFormat(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/7/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         400,
				"body":       "Check timestamp formats",
				"path":       "README.md",
				"created_at": "2024-03-15T10:30:00Z",
				"updated_at": "2024-03-16T11:45:00Z",
				"user":       map[string]any{"login": "dave"},
			},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comments, err := client.GetRecentPRComments(7, "")
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
