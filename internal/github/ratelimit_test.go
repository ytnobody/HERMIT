package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// newTestClient creates a Client backed by a test HTTP server.
func newTestClient(t *testing.T, mux *http.ServeMux) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(mux)
	ghClient := gogithub.NewClient(oauth2.NewClient(context.Background(), nil))
	baseURL, _ := url.Parse(srv.URL + "/")
	ghClient.BaseURL = baseURL
	ghClient.UploadURL = baseURL
	c := &Client{gh: ghClient, owner: "owner", repo: "repo"}
	return c, srv.Close
}

func rateLimitResponse(remaining int, resetUnix int64) map[string]any {
	return map[string]any{
		"resources": map[string]any{
			"core": map[string]any{
				"limit":     5000,
				"remaining": remaining,
				"reset":     resetUnix,
				"used":      5000 - remaining,
			},
		},
		"rate": map[string]any{
			"limit":     5000,
			"remaining": remaining,
			"reset":     resetUnix,
		},
	}
}

func TestCheckRateLimit(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		remaining int
		resetAt   time.Time
		threshold int
		wantErr   bool
		wantSleep bool
	}{
		{
			name:      "above threshold: no error",
			remaining: 100,
			resetAt:   now.Add(5 * time.Minute),
			threshold: 10,
			wantErr:   false,
			wantSleep: false,
		},
		{
			name:      "at threshold with soon reset: waits",
			remaining: 5,
			resetAt:   now.Add(30 * time.Second),
			threshold: 10,
			wantErr:   false,
			wantSleep: true,
		},
		{
			name:      "at threshold with far reset: returns error",
			remaining: 5,
			resetAt:   now.Add(20 * time.Minute),
			threshold: 10,
			wantErr:   true,
			wantSleep: false,
		},
		{
			name:      "zero threshold uses default: above default",
			remaining: 50,
			resetAt:   now.Add(5 * time.Minute),
			threshold: 0,
			wantErr:   false,
			wantSleep: false,
		},
		{
			name:      "zero threshold uses default: below default with far reset",
			remaining: 5,
			resetAt:   now.Add(20 * time.Minute),
			threshold: 0,
			wantErr:   true,
			wantSleep: false,
		},
		{
			name:      "exactly at reset boundary (10 min): waits",
			remaining: 3,
			resetAt:   now.Add(10 * time.Minute),
			threshold: 10,
			wantErr:   false,
			wantSleep: true,
		},
		{
			name:      "reset already passed: no sleep, no error",
			remaining: 3,
			resetAt:   now.Add(-1 * time.Second),
			threshold: 10,
			wantErr:   false,
			wantSleep: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slept := false
			orig := sleepFunc
			sleepFunc = func(d time.Duration) { slept = true }
			defer func() { sleepFunc = orig }()

			mux := http.NewServeMux()
			mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(rateLimitResponse(tt.remaining, tt.resetAt.Unix()))
			})

			client, teardown := newTestClient(t, mux)
			defer teardown()

			err := client.CheckRateLimit(tt.threshold)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckRateLimit() error = %v, wantErr %v", err, tt.wantErr)
			}
			if slept != tt.wantSleep {
				t.Errorf("slept = %v, wantSleep %v", slept, tt.wantSleep)
			}
		})
	}
}

func TestCheckRateLimit_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	// fail-open: API error should not block execution
	err := client.CheckRateLimit(10)
	if err != nil {
		t.Errorf("CheckRateLimit() should fail-open on API error, got error: %v", err)
	}
}

func TestCheckRateLimit_NilCore(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"resources":{}, "rate":{"limit":5000,"remaining":100,"reset":9999999999}}`)
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	// fail-open: nil core should warn but not block
	err := client.CheckRateLimit(10)
	if err != nil {
		t.Errorf("CheckRateLimit() should fail-open when core is nil, got error: %v", err)
	}
}

func TestGetRateLimitInfo(t *testing.T) {
	resetTime := time.Now().Add(5 * time.Minute).Truncate(time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rateLimitResponse(42, resetTime.Unix()))
	})

	client, teardown := newTestClient(t, mux)
	defer teardown()

	info, err := client.getRateLimitInfo()
	if err != nil {
		t.Fatalf("getRateLimitInfo() unexpected error: %v", err)
	}
	if info.Remaining != 42 {
		t.Errorf("Remaining = %d, want 42", info.Remaining)
	}
	if !info.ResetAt.Equal(resetTime) {
		t.Errorf("ResetAt = %v, want %v", info.ResetAt, resetTime)
	}
}
