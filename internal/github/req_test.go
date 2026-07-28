package github

// REQ-named test for the requirements reconcile sweep (Issue #152). See
// REQUIREMENTS.md, REQ-003.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestREQ003_ListIssues_LabelFilterAndPRsSkipped verifies the label-filter
// half of REQ-003: when a label is given, only Issues carrying that label are
// requested/returned, and pull requests are never returned as Issues. The
// queue-exclusion half of REQ-003 is covered by
// TestREQ003_ListIssues_ExcludesNonQueueIssues in internal/mcp.
func TestREQ003_ListIssues_LabelFilterAndPRsSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != "hermit" {
			http.Error(w, "unexpected labels param", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != "open" {
			http.Error(w, "unexpected state param", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 5, "title": "Labeled issue", "body": "", "labels": []map[string]any{
				{"name": "hermit"},
			}, "author_association": "OWNER"},
			{"number": 6, "title": "A PR, not an issue", "body": "", "labels": []map[string]any{},
				"pull_request": map[string]any{"url": "http://example.com"}},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListOpenIssues("hermit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue (labeled, PR skipped), got %d", len(issues))
	}
	if issues[0].Number != 5 {
		t.Errorf("issues[0].Number = %d, want 5", issues[0].Number)
	}
}

// REQ-named tests for REQ-017 (Issue #178): list_issues only surfaces Issues
// from authors with a trusted author_association.

// TestREQ017_ListOpenIssues_ExcludesUntrustedAuthors verifies that Issues
// whose author_association is not in the trusted allowlist (default:
// OWNER/MEMBER/COLLABORATOR) are excluded from ListOpenIssues, while trusted
// ones are kept.
func TestREQ017_ListOpenIssues_ExcludesUntrustedAuthors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "From owner", "body": "b", "labels": []map[string]any{}, "author_association": "OWNER"},
			{"number": 2, "title": "From member", "body": "b", "labels": []map[string]any{}, "author_association": "MEMBER"},
			{"number": 3, "title": "From collaborator", "body": "b", "labels": []map[string]any{}, "author_association": "COLLABORATOR"},
			{"number": 4, "title": "From contributor", "body": "b", "labels": []map[string]any{}, "author_association": "CONTRIBUTOR"},
			{"number": 5, "title": "From first-timer", "body": "b", "labels": []map[string]any{}, "author_association": "FIRST_TIME_CONTRIBUTOR"},
			{"number": 6, "title": "From nobody", "body": "b", "labels": []map[string]any{}, "author_association": "NONE"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListOpenIssues("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []int
	for _, iss := range issues {
		got = append(got, iss.Number)
	}
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("expected issues %v, got %v", want, got)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("issue index %d: got #%d, want #%d (full result: %v)", i, got[i], n, got)
		}
	}
}

// TestREQ017_ListOpenIssues_DefaultsToSafeAllowlistWhenUnconfigured verifies
// that a Client which never had SetTrustedAuthorAssociations called (i.e.
// harness.toml has no [security] section) still applies the safe default
// allowlist rather than allowing every author through.
func TestREQ017_ListOpenIssues_DefaultsToSafeAllowlistWhenUnconfigured(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Untrusted", "body": "b", "labels": []map[string]any{}, "author_association": "NONE"},
		})
	})

	// newTestClient never calls SetTrustedAuthorAssociations, mirroring a
	// harness.toml with no [security] section.
	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.ListOpenIssues("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected untrusted-author issue to be excluded by default, got %+v", issues)
	}
}

// TestREQ017_SetTrustedAuthorAssociations_EmptySliceFallsBackToDefault
// verifies that explicitly configuring an empty allowlist (e.g.
// harness.toml's trusted_author_associations left as `[]` or omitted, which
// TOML decoding leaves as a nil/empty slice) does not degrade into "allow
// everyone" — it must resolve to the same safe default as never calling
// SetTrustedAuthorAssociations at all.
func TestREQ017_SetTrustedAuthorAssociations_EmptySliceFallsBackToDefault(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Untrusted", "body": "b", "labels": []map[string]any{}, "author_association": "NONE"},
			{"number": 2, "title": "Trusted", "body": "b", "labels": []map[string]any{}, "author_association": "OWNER"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()
	client.SetTrustedAuthorAssociations(nil)

	issues, err := client.ListOpenIssues("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 2 {
		t.Errorf("expected only the OWNER-authored issue #2, got %+v", issues)
	}
}

// TestREQ017_SetTrustedAuthorAssociations_CustomAllowlistIsHonored verifies
// that an explicit, non-empty allowlist configured via
// SetTrustedAuthorAssociations (i.e. harness.toml's
// [security].trusted_author_associations) is applied instead of the
// built-in default, including case-insensitive matching.
func TestREQ017_SetTrustedAuthorAssociations_CustomAllowlistIsHonored(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Contributor", "body": "b", "labels": []map[string]any{}, "author_association": "CONTRIBUTOR"},
			{"number": 2, "title": "None", "body": "b", "labels": []map[string]any{}, "author_association": "NONE"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()
	// Lower-case on purpose to verify case-insensitive comparison.
	client.SetTrustedAuthorAssociations([]string{"contributor"})

	issues, err := client.ListOpenIssues("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Errorf("expected only the CONTRIBUTOR-authored issue #1 under custom allowlist, got %+v", issues)
	}
}

// TestREQ017_ListAllIssues_ExcludesUntrustedAuthorsAcrossRepos verifies the
// multi-repo path (ListAllIssues) applies the same author_association
// filtering as the single-repo path.
func TestREQ017_ListAllIssues_ExcludesUntrustedAuthorsAcrossRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/frontend/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Trusted", "body": "", "labels": []map[string]any{}, "author_association": "MEMBER"},
			{"number": 2, "title": "Untrusted", "body": "", "labels": []map[string]any{}, "author_association": "NONE"},
		})
	})
	mux.HandleFunc("/repos/org/backend/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 3, "title": "Untrusted", "body": "", "labels": []map[string]any{}, "author_association": "FIRST_TIME_CONTRIBUTOR"},
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
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Errorf("expected only issue #1 (MEMBER) to survive across both repos, got %+v", issues)
	}
}

// TestREQ017_Issue_AuthorAssociationFieldIsPopulated verifies that surviving
// Issues carry their author_association value through to the returned
// struct, so callers/operators can observe why an Issue was (or would be)
// trusted.
func TestREQ017_Issue_AuthorAssociationFieldIsPopulated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "From owner", "body": "b", "labels": []map[string]any{}, "author_association": "OWNER"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	issues, err := client.listOpenIssuesFromRepo("owner", "repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].AuthorAssociation != "OWNER" {
		t.Errorf("expected AuthorAssociation=OWNER, got %+v", issues)
	}
}
