package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	gogithub "github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// newTestClientFor creates a Client backed by a test HTTP server with a custom
// owner/repo so we can test the resolveRepo logic independently.
func newTestClientFor(t *testing.T, mux *http.ServeMux, owner, repo string) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(mux)
	ghClient := gogithub.NewClient(oauth2.NewClient(context.Background(), nil))
	baseURL, _ := url.Parse(srv.URL + "/")
	ghClient.BaseURL = baseURL
	ghClient.UploadURL = baseURL
	c := &Client{gh: ghClient, owner: owner, repo: repo}
	return c, srv.Close
}

// issueListResponse builds a minimal GitHub Issues.ListByRepo response.
func issueListResponse(issues []map[string]any) []map[string]any {
	return issues
}

// --- resolveRepo ---

func TestResolveRepo_DefaultsToClientValues(t *testing.T) {
	c := &Client{owner: "myorg", repo: "myrepo"}
	o, r := c.resolveRepo("", "")
	if o != "myorg" || r != "myrepo" {
		t.Errorf("resolveRepo(\"\",\"\") = (%q, %q), want (\"myorg\", \"myrepo\")", o, r)
	}
}

func TestResolveRepo_UsesProvidedValues(t *testing.T) {
	c := &Client{owner: "myorg", repo: "myrepo"}
	o, r := c.resolveRepo("otherorg", "otherrepo")
	if o != "otherorg" || r != "otherrepo" {
		t.Errorf("resolveRepo(\"otherorg\",\"otherrepo\") = (%q, %q), want (\"otherorg\", \"otherrepo\")", o, r)
	}
}

func TestResolveRepo_PartialOverride(t *testing.T) {
	c := &Client{owner: "myorg", repo: "myrepo"}
	o, r := c.resolveRepo("", "override-repo")
	if o != "myorg" || r != "override-repo" {
		t.Errorf("resolveRepo(\"\",\"override-repo\") = (%q, %q), want (\"myorg\", \"override-repo\")", o, r)
	}
}

// --- ListOpenIssues (single-repo, backward compat) ---

func TestListOpenIssues_SingleRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(issueListResponse([]map[string]any{
			{"number": 1, "title": "Issue one", "body": "body", "labels": []map[string]any{}},
			{"number": 2, "title": "Issue two", "body": "body2", "labels": []map[string]any{},
				"pull_request": map[string]any{"url": "http://example.com"}}, // should be skipped
		}))
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListOpenIssues("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (PR skipped), got %d", len(issues))
	}
	if issues[0].Number != 1 {
		t.Errorf("issues[0].Number = %d, want 1", issues[0].Number)
	}
	// backward compat: Owner/Repo should be empty in single-repo mode
	if issues[0].Owner != "" || issues[0].Repo != "" {
		t.Errorf("single-repo mode: expected empty Owner/Repo, got Owner=%q Repo=%q",
			issues[0].Owner, issues[0].Repo)
	}
}

func TestListOpenIssues_LabelFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != "hermit" {
			http.Error(w, "unexpected labels param", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 5, "title": "Labeled issue", "body": "", "labels": []map[string]any{
				{"name": "hermit"},
			}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListOpenIssues("hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 5 {
		t.Errorf("expected issue #5, got %+v", issues)
	}
}

func TestListOpenIssues_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.ListOpenIssues("")
	if err == nil {
		t.Error("expected error from API failure, got nil")
	}
}

// --- listOpenIssuesFromRepo ---

func TestListOpenIssuesFromRepo_SetsOwnerAndRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 10, "title": "Test", "body": "", "labels": []map[string]any{}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.listOpenIssuesFromRepo("owner", "repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Owner != "owner" || issues[0].Repo != "repo" {
		t.Errorf("expected Owner=owner Repo=repo, got Owner=%q Repo=%q", issues[0].Owner, issues[0].Repo)
	}
}

// --- ListAllIssues ---

func TestListAllIssues_MultiRepo(t *testing.T) {
	// Use separate muxes to simulate two different repo endpoints through one test server.
	// We'll register both paths on one mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/frontend/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Frontend issue", "body": "", "labels": []map[string]any{}},
		})
	})
	mux.HandleFunc("/repos/org/backend/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 2, "title": "Backend issue", "body": "", "labels": []map[string]any{}},
		})
	})

	client, teardown := newTestClientFor(t, mux, "org", "frontend")
	defer teardown()

	repos := []RepoConfig{
		{Owner: "org", Repo: "frontend"},
		{Owner: "org", Repo: "backend"},
	}
	issues, err := client.ListAllIssues(repos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues from 2 repos, got %d", len(issues))
	}

	// Verify Owner/Repo are set correctly.
	byNum := make(map[int]Issue)
	for _, iss := range issues {
		byNum[iss.Number] = iss
	}
	if byNum[1].Owner != "org" || byNum[1].Repo != "frontend" {
		t.Errorf("issue #1: want Owner=org Repo=frontend, got Owner=%q Repo=%q",
			byNum[1].Owner, byNum[1].Repo)
	}
	if byNum[2].Owner != "org" || byNum[2].Repo != "backend" {
		t.Errorf("issue #2: want Owner=org Repo=backend, got Owner=%q Repo=%q",
			byNum[2].Owner, byNum[2].Repo)
	}
}

func TestListAllIssues_EmptyRepos_FallbackToPrimary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 99, "title": "Primary repo issue", "body": "", "labels": []map[string]any{}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListAllIssues(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 99 {
		t.Errorf("expected issue #99 from primary repo fallback, got %+v", issues)
	}
	// In fallback mode owner/repo are set (unlike ListOpenIssues single-repo compat path).
	if issues[0].Owner != "owner" || issues[0].Repo != "repo" {
		t.Errorf("expected Owner=owner Repo=repo in fallback, got Owner=%q Repo=%q",
			issues[0].Owner, issues[0].Repo)
	}
}

func TestListAllIssues_MultiRepo_WithLabelFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/frontend/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != "hermit" {
			http.Error(w, "unexpected labels", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 3, "title": "Tagged issue", "body": "", "labels": []map[string]any{{"name": "hermit"}}},
		})
	})

	client, teardown := newTestClientFor(t, mux, "org", "frontend")
	defer teardown()

	repos := []RepoConfig{{Owner: "org", Repo: "frontend", Label: "hermit"}}
	issues, err := client.ListAllIssues(repos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 3 {
		t.Errorf("expected issue #3 with label filter, got %+v", issues)
	}
}

func TestListAllIssues_MultiRepo_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/broken/issues", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	client, teardown := newTestClientFor(t, mux, "org", "broken")
	defer teardown()

	repos := []RepoConfig{{Owner: "org", Repo: "broken"}}
	_, err := client.ListAllIssues(repos)
	if err == nil {
		t.Error("expected error from API failure in multi-repo mode, got nil")
	}
}

// --- AssignIssueInRepo ---

func TestAssignIssueInRepo_DefaultsToClientRepo(t *testing.T) {
	addAssigneesCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/5/assignees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			addAssigneesCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": 5})
	})
	mux.HandleFunc("/repos/owner/repo/issues/5/labels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.AssignIssueInRepo(5, "user1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !addAssigneesCalled {
		t.Error("expected assignees endpoint to be called")
	}
}

func TestAssignIssueInRepo_OverrideRepo(t *testing.T) {
	addAssigneesCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/other-org/other-repo/issues/7/assignees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			addAssigneesCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": 7})
	})
	mux.HandleFunc("/repos/other-org/other-repo/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.AssignIssueInRepo(7, "user1", "other-org", "other-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !addAssigneesCalled {
		t.Error("expected assignees endpoint to be called on overridden repo")
	}
}

// --- GetPRStatusInRepo ---

func TestGetPRStatusInRepo_DefaultsToClientRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    10,
			"additions": 5,
			"deletions": 3,
			"head":      map[string]any{"sha": "abc123"},
		})
	})
	mux.HandleFunc("/repos/owner/repo/pulls/10/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/repos/owner/repo/commits/abc123/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"state": "success"})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	status, err := client.GetPRStatusInRepo(10, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Number != 10 {
		t.Errorf("expected PR number 10, got %d", status.Number)
	}
}

func TestGetPRStatusInRepo_OverrideRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/other-org/other-repo/pulls/20", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    20,
			"additions": 10,
			"deletions": 2,
			"head":      map[string]any{"sha": "def456"},
		})
	})
	mux.HandleFunc("/repos/other-org/other-repo/pulls/20/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/repos/other-org/other-repo/commits/def456/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"state": ""})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	status, err := client.GetPRStatusInRepo(20, "other-org", "other-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Number != 20 {
		t.Errorf("expected PR number 20, got %d", status.Number)
	}
	if !status.CIPassing {
		t.Error("expected CIPassing=true for empty CI state")
	}
}

// --- MergePRInRepo ---

func TestMergePRInRepo_DefaultsToClientRepo(t *testing.T) {
	mergeCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/15/merge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mergeCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"merged": true})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.MergePRInRepo(15, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mergeCalled {
		t.Error("expected merge endpoint to be called")
	}
}

func TestMergePRInRepo_OverrideRepo(t *testing.T) {
	mergeCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/other-org/other-repo/pulls/25/merge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mergeCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"merged": true})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.MergePRInRepo(25, "other-org", "other-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mergeCalled {
		t.Error("expected merge endpoint to be called on overridden repo")
	}
}

// --- PostCommentInRepo ---

func TestPostCommentInRepo_DefaultsToClientRepo(t *testing.T) {
	commentCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/30/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			commentCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.PostCommentInRepo(30, "hello", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commentCalled {
		t.Error("expected comment endpoint to be called")
	}
}

func TestPostCommentInRepo_OverrideRepo(t *testing.T) {
	commentCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/other-org/other-repo/issues/40/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			commentCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 2})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.PostCommentInRepo(40, "override comment", "other-org", "other-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commentCalled {
		t.Error("expected comment endpoint to be called on overridden repo")
	}
}

// --- CloseIssueInRepo ---

func TestCloseIssueInRepo_OverrideRepo(t *testing.T) {
	editCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/other-org/other-repo/issues/50", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			editCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": 50, "state": "closed"})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	err := client.CloseIssueInRepo(50, "", "other-org", "other-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !editCalled {
		t.Error("expected PATCH endpoint to be called on overridden repo")
	}
}
