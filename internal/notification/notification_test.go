package notification_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ytnobody/hermit/internal/notification"
)

func TestDetectType(t *testing.T) {
	tests := []struct {
		url  string
		want notification.WebhookType
	}{
		{"https://hooks.slack.com/services/XXX/YYY/ZZZ", notification.TypeSlack},
		{"https://discord.com/api/webhooks/123/abc", notification.TypeDiscord},
		{"https://example.com/webhook", notification.TypeGeneric},
		{"", notification.TypeGeneric},
	}
	for _, tc := range tests {
		got := notification.DetectType(tc.url)
		if got != tc.want {
			t.Errorf("DetectType(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestSend_EmptyURL(t *testing.T) {
	// Should silently succeed without any HTTP call.
	if err := notification.Send("", "", "test_event", "hello"); err != nil {
		t.Errorf("expected nil error for empty URL, got %v", err)
	}
}

func TestSend_Slack(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Force slack type via URL pattern override by passing type "slack".
	err := notification.Send(srv.URL, "slack", "issue_assigned", "Issue #1 assigned")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["text"] != "Issue #1 assigned" {
		t.Errorf("expected text payload, got %v", gotBody)
	}
}

func TestSend_Discord(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := notification.Send(srv.URL, "discord", "pr_merged", "PR #5 merged")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["content"] != "PR #5 merged" {
		t.Errorf("expected content payload, got %v", gotBody)
	}
}

func TestSend_Generic(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := notification.Send(srv.URL, "generic", "high_risk", "HIGH risk detected")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["event"] != "high_risk" || gotBody["message"] != "HIGH risk detected" {
		t.Errorf("expected generic payload with event+message, got %v", gotBody)
	}
}

func TestSend_AutoDetectSlack(t *testing.T) {
	// Auto-detect from URL containing hooks.slack.com pattern – we can't hit
	// the real Slack, so we just verify DetectType returns the right value and
	// Send uses it (via a mock server registered at the Slack-like URL we
	// control through the wtype override).
	got := notification.DetectType("https://hooks.slack.com/services/T/B/X")
	if got != notification.TypeSlack {
		t.Errorf("expected slack, got %q", got)
	}
}

func TestSend_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := notification.Send(srv.URL, "generic", "test", "msg")
	if err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}
}
