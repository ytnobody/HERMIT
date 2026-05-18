package github

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCloseIssue_WithoutComment(t *testing.T) {
	editCalled := false
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/issues/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["state"] != "closed" {
			t.Errorf("expected state=closed, got %v", body["state"])
		}
		editCalled = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "state": "closed"})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	if err := client.CloseIssue(42, ""); err != nil {
		t.Fatalf("CloseIssue() unexpected error: %v", err)
	}
	if !editCalled {
		t.Error("expected PATCH /issues/42 to be called")
	}
}

func TestCloseIssue_WithComment(t *testing.T) {
	commentCalled := false
	editCalled := false

	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s for comments", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode comment body: %v", err)
		}
		bodyStr, ok := body["body"].(string)
		if !ok {
			t.Fatalf("expected comment body to be a string, got %T", body["body"])
		}
		if !strings.Contains(bodyStr, "resolved") {
			t.Errorf("expected comment body to contain 'resolved', got %v", bodyStr)
		}
		commentCalled = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "body": body["body"]})
	})

	mux.HandleFunc("/repos/owner/repo/issues/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s for issue edit", r.Method)
		}
		editCalled = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "closed"})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	if err := client.CloseIssue(7, "Issue resolved"); err != nil {
		t.Fatalf("CloseIssue() unexpected error: %v", err)
	}
	if !commentCalled {
		t.Error("expected POST /issues/7/comments to be called")
	}
	if !editCalled {
		t.Error("expected PATCH /issues/7 to be called")
	}
}

func TestCloseIssue_CommentError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/issues/9/comments", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.CloseIssue(9, "some comment")
	if err == nil {
		t.Error("expected error when posting comment fails")
	}
}

func TestCloseIssue_EditError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/issues/10", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.CloseIssue(10, "")
	if err == nil {
		t.Error("expected error when edit fails")
	}
}
