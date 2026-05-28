package github

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHasCommentMatching_found(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/10/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "body": "some text", "user": map[string]any{"login": "alice"}},
			{"id": 2, "body": "please /hermit this", "user": map[string]any{"login": "bob"}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	matched, err := client.HasCommentMatching(10, "/hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected match, got false")
	}
}

func TestHasCommentMatching_notFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/10/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "body": "just a regular comment", "user": map[string]any{"login": "alice"}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	matched, err := client.HasCommentMatching(10, "/hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("expected no match, got true")
	}
}

func TestHasCommentMatching_caseInsensitive(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/5/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "body": "/HERMIT please process", "user": map[string]any{"login": "carol"}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	matched, err := client.HasCommentMatching(5, "/hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected case-insensitive match, got false")
	}
}

func TestHasCommentMatching_noComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	matched, err := client.HasCommentMatching(7, "/hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("expected false for issue with no comments, got true")
	}
}

func TestHasCommentMatching_apiError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.HasCommentMatching(3, "/hermit")
	if err == nil {
		t.Error("expected error from API failure, got nil")
	}
}
