package github

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestReviewPR_Basic(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    10,
			"additions": 30,
			"deletions": 5,
			"head":      map[string]any{"sha": "abc123"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/pulls/10/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"filename": "pkg/foo.go", "additions": 20, "deletions": 3, "status": "modified"},
			{"filename": "pkg/foo_test.go", "additions": 10, "deletions": 2, "status": "modified"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comment, err := client.ReviewPR(10)
	if err != nil {
		t.Fatalf("ReviewPR() unexpected error: %v", err)
	}

	// Should contain summary section
	if !strings.Contains(comment, "File Change Summary") {
		t.Error("expected comment to contain 'File Change Summary'")
	}
	// Should contain risk section
	if !strings.Contains(comment, "Risk Assessment") {
		t.Error("expected comment to contain 'Risk Assessment'")
	}
	// Should contain checklist section
	if !strings.Contains(comment, "Checklist") {
		t.Error("expected comment to contain 'Checklist'")
	}
	// Tests present (foo_test.go was changed)
	if !strings.Contains(comment, "[x] Tests present") {
		t.Error("expected checklist to mark tests as present")
	}
	// No docs changed
	if !strings.Contains(comment, "[ ] Docs updated") {
		t.Error("expected checklist to mark docs as not updated")
	}
}

func TestReviewPR_WithDocs(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/11", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    11,
			"additions": 15,
			"deletions": 2,
			"head":      map[string]any{"sha": "def456"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/pulls/11/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"filename": "docs/guide.md", "additions": 15, "deletions": 2, "status": "modified"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comment, err := client.ReviewPR(11)
	if err != nil {
		t.Fatalf("ReviewPR() unexpected error: %v", err)
	}

	// No test files changed
	if !strings.Contains(comment, "[ ] Tests present") {
		t.Error("expected checklist to mark tests as not present")
	}
	// Docs changed
	if !strings.Contains(comment, "[x] Docs updated") {
		t.Error("expected checklist to mark docs as updated")
	}
}

func TestReviewPR_HighRisk(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/12", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    12,
			"additions": 300,
			"deletions": 250,
			"head":      map[string]any{"sha": "ghi789"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/pulls/12/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"filename": "cmd/main.go", "additions": 300, "deletions": 250, "status": "modified"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comment, err := client.ReviewPR(12)
	if err != nil {
		t.Fatalf("ReviewPR() unexpected error: %v", err)
	}

	// Should be HIGH risk due to cmd/ path
	if !strings.Contains(comment, "HIGH") {
		t.Error("expected comment to contain HIGH risk level")
	}
	// Breaking changes expected (deletions >= 50 and HIGH risk)
	if !strings.Contains(comment, "[x] Possible breaking changes") {
		t.Error("expected checklist to mark breaking changes")
	}
}

func TestReviewPR_LineCountsInSummary(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/13", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    13,
			"additions": 42,
			"deletions": 7,
			"head":      map[string]any{"sha": "jkl012"},
		})
	})

	mux.HandleFunc("/repos/owner/repo/pulls/13/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"filename": "README.md", "additions": 42, "deletions": 7, "status": "modified"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	comment, err := client.ReviewPR(13)
	if err != nil {
		t.Fatalf("ReviewPR() unexpected error: %v", err)
	}

	if !strings.Contains(comment, "+42") {
		t.Error("expected comment to contain '+42' additions")
	}
	if !strings.Contains(comment, "-7") {
		t.Error("expected comment to contain '-7' deletions")
	}
	if !strings.Contains(comment, "Files changed**: 1") {
		t.Error("expected comment to mention 1 file changed")
	}
}

func TestReviewPR_GetPRError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/99", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.ReviewPR(99)
	if err == nil {
		t.Error("expected error when PR not found")
	}
}

func TestReviewPR_ListFilesError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/owner/repo/pulls/20", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/files") {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":    20,
			"additions": 10,
			"deletions": 2,
			"head":      map[string]any{"sha": "mno345"},
		})
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	_, err := client.ReviewPR(20)
	if err == nil {
		t.Error("expected error when listing files fails")
	}
}

func TestReviewEvaluate_Low(t *testing.T) {
	files := []PRFile{{Filename: "README.md", Additions: 5, Deletions: 2}}
	level, reasons := reviewEvaluate(files, 5, 2)
	if level != "LOW" {
		t.Errorf("expected LOW, got %s", level)
	}
	if len(reasons) != 0 {
		t.Errorf("expected no reasons, got %v", reasons)
	}
}

func TestReviewEvaluate_Medium(t *testing.T) {
	files := []PRFile{{Filename: "internal/service/svc.go", Additions: 30, Deletions: 10}}
	level, reasons := reviewEvaluate(files, 30, 10)
	if level != "MEDIUM" {
		t.Errorf("expected MEDIUM, got %s", level)
	}
	if len(reasons) == 0 {
		t.Error("expected reasons for MEDIUM risk")
	}
}

func TestReviewEvaluate_HighLines(t *testing.T) {
	files := []PRFile{{Filename: "pkg/big.go", Additions: 400, Deletions: 200}}
	level, _ := reviewEvaluate(files, 400, 200)
	if level != "HIGH" {
		t.Errorf("expected HIGH, got %s", level)
	}
}

func TestReviewEvaluate_HighPath(t *testing.T) {
	files := []PRFile{{Filename: "cmd/main.go", Additions: 1, Deletions: 0}}
	level, _ := reviewEvaluate(files, 1, 0)
	if level != "HIGH" {
		t.Errorf("expected HIGH, got %s", level)
	}
}

func TestReviewEvaluate_HighGoMod(t *testing.T) {
	files := []PRFile{{Filename: "go.mod", Additions: 1, Deletions: 0}}
	level, _ := reviewEvaluate(files, 1, 0)
	if level != "HIGH" {
		t.Errorf("expected HIGH, got %s", level)
	}
}
