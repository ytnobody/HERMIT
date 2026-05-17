package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// WebhookType represents the type of webhook target.
type WebhookType string

const (
	TypeSlack   WebhookType = "slack"
	TypeDiscord WebhookType = "discord"
	TypeGeneric WebhookType = "generic"
)

// DetectType infers the webhook type from the URL if possible.
func DetectType(webhookURL string) WebhookType {
	switch {
	case strings.Contains(webhookURL, "hooks.slack.com"):
		return TypeSlack
	case strings.Contains(webhookURL, "discord.com/api/webhooks"):
		return TypeDiscord
	default:
		return TypeGeneric
	}
}

// buildPayload constructs the JSON payload for the given webhook type.
func buildPayload(wtype WebhookType, event, message string) ([]byte, error) {
	var payload map[string]string
	switch wtype {
	case TypeSlack:
		payload = map[string]string{"text": message}
	case TypeDiscord:
		payload = map[string]string{"content": message}
	default:
		return json.Marshal(map[string]string{"event": event, "message": message})
	}
	return json.Marshal(payload)
}

// Send sends a notification to webhookURL.
// If webhookURL is empty it silently returns nil.
// wtype may be empty string to trigger auto-detection from the URL.
func Send(webhookURL, wtype, event, message string) error {
	if webhookURL == "" {
		return nil
	}

	t := DetectType(webhookURL)
	if wtype != "" {
		t = WebhookType(wtype)
	}

	body, err := buildPayload(t, event, message)
	if err != nil {
		return fmt.Errorf("notification: failed to build payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("notification: HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification: webhook returned status %d", resp.StatusCode)
	}
	return nil
}
