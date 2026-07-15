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
			}},
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
