package alert

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zfsrabbit/internal/config"
)

func TestNewSlackAlerter(t *testing.T) {
	cfg := &config.SlackConfig{
		WebhookURL:    "https://hooks.slack.com/test",
		Channel:       "#test",
		Username:      "TestBot",
		IconEmoji:     ":test:",
		Enabled:       true,
		AlertOnSync:   true,
		AlertOnErrors: true,
	}

	alerter := NewSlackAlerter(cfg)

	if alerter.config != cfg {
		t.Error("Config not set correctly")
	}
}

func TestSlackAlerterSendAlert(t *testing.T) {
	// Mock Slack webhook server
	var receivedPayload SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("Failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		WebhookURL:    server.URL,
		Channel:       "#alerts",
		Username:      "ZFSRabbit",
		IconEmoji:     ":rabbit:",
		Enabled:       true,
		AlertOnErrors: true,
	}

	alerter := NewSlackAlerter(cfg)

	err := alerter.SendAlert("Test Alert", "This is a test alert message")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify payload
	if receivedPayload.Channel != "#alerts" {
		t.Errorf("Expected channel #alerts, got %s", receivedPayload.Channel)
	}

	if receivedPayload.Username != "ZFSRabbit" {
		t.Errorf("Expected username ZFSRabbit, got %s", receivedPayload.Username)
	}

	if receivedPayload.IconEmoji != ":rabbit:" {
		t.Errorf("Expected icon :rabbit:, got %s", receivedPayload.IconEmoji)
	}

	if len(receivedPayload.Blocks) == 0 {
		t.Error("Expected blocks in message")
	}

	// Check for header block
	foundHeader := false
	for _, block := range receivedPayload.Blocks {
		if block.Type == "header" && block.Text != nil {
			if strings.Contains(block.Text.Text, "Test Alert") {
				foundHeader = true
				break
			}
		}
	}

	if !foundHeader {
		t.Error("Expected header block with alert title")
	}
}

func TestSlackAlerterSendSyncSuccess(t *testing.T) {
	var receivedPayload SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		WebhookURL:  server.URL,
		Channel:     "#sync",
		Username:    "ZFSRabbit",
		IconEmoji:   ":rabbit:",
		Enabled:     true,
		AlertOnSync: true,
	}

	alerter := NewSlackAlerter(cfg)

	duration := 5 * time.Minute
	err := alerter.SendSyncSuccess("test-snapshot", "tank/test", duration)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify sync success message format
	foundSyncMessage := false
	for _, block := range receivedPayload.Blocks {
		if block.Type == "header" && block.Text != nil {
			if strings.Contains(block.Text.Text, "✅") && strings.Contains(block.Text.Text, "Sync Completed") {
				foundSyncMessage = true
				break
			}
		}
	}

	if !foundSyncMessage {
		t.Error("Expected sync success message format")
	}
}

func TestSlackAlerterSendSyncFailure(t *testing.T) {
	var receivedPayload SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		WebhookURL:  server.URL,
		Channel:     "#alerts",
		Username:    "ZFSRabbit",
		IconEmoji:   ":rabbit:",
		Enabled:     true,
		AlertOnSync: true,
	}

	alerter := NewSlackAlerter(cfg)

	testError := errors.New("sync failed")
	err := alerter.SendSyncFailure("test-snapshot", "tank/test", testError)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify sync failure message format
	foundFailureMessage := false
	for _, block := range receivedPayload.Blocks {
		if block.Type == "header" && block.Text != nil {
			if strings.Contains(block.Text.Text, "❌") && strings.Contains(block.Text.Text, "Sync Failed") {
				foundFailureMessage = true
				break
			}
		}
	}

	if !foundFailureMessage {
		t.Error("Expected sync failure message format")
	}
}

func TestSlackAlerterSendSystemStatus(t *testing.T) {
	var receivedPayload SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		WebhookURL: server.URL,
		Channel:    "#status",
		Username:   "ZFSRabbit",
		IconEmoji:  ":rabbit:",
		Enabled:    true,
	}

	alerter := NewSlackAlerter(cfg)

	status := map[string]interface{}{
		"pools": map[string]interface{}{
			"tank": map[string]interface{}{
				"State": "ONLINE",
			},
			"backup": map[string]interface{}{
				"State": "DEGRADED",
			},
		},
		"disks": map[string]interface{}{
			"/dev/sda": map[string]interface{}{
				"Healthy":     true,
				"Temperature": 35,
			},
			"/dev/sdb": map[string]interface{}{
				"Healthy":     false,
				"Temperature": 65,
			},
		},
	}

	err := alerter.SendSystemStatus(status)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify system status message format
	foundStatusHeader := false
	foundPoolInfo := false
	foundDiskInfo := false

	for _, block := range receivedPayload.Blocks {
		if block.Type == "header" && block.Text != nil {
			if strings.Contains(block.Text.Text, "System Status") {
				foundStatusHeader = true
			}
		}

		if block.Type == "section" && len(block.Fields) > 0 {
			for _, field := range block.Fields {
				if strings.Contains(field.Text, "tank") {
					foundPoolInfo = true
				}
				if strings.Contains(field.Text, "/dev/sda") {
					foundDiskInfo = true
				}
			}
		}
	}

	if !foundStatusHeader {
		t.Error("Expected system status header")
	}

	if !foundPoolInfo {
		t.Error("Expected pool information in status")
	}

	if !foundDiskInfo {
		t.Error("Expected disk information in status")
	}
}

func TestSlackAlerterDisabled(t *testing.T) {
	cfg := &config.SlackConfig{
		WebhookURL: "https://hooks.slack.com/test",
		Enabled:    false, // Disabled
	}

	alerter := NewSlackAlerter(cfg)

	// All methods should return early without error when disabled
	err := alerter.SendAlert("Test", "Test message")
	if err != nil {
		t.Errorf("Expected no error when disabled, got: %v", err)
	}

	err = alerter.SendSyncSuccess("test", "dataset", time.Minute)
	if err != nil {
		t.Errorf("Expected no error when disabled, got: %v", err)
	}

	err = alerter.SendSyncFailure("test", "dataset", errors.New("test"))
	if err != nil {
		t.Errorf("Expected no error when disabled, got: %v", err)
	}
}

func TestSlackAlerterEmptyWebhookURL(t *testing.T) {
	cfg := &config.SlackConfig{
		WebhookURL: "", // Empty URL
		Enabled:    true,
	}

	alerter := NewSlackAlerter(cfg)

	// Should return early without error when no webhook URL
	err := alerter.SendAlert("Test", "Test message")
	if err != nil {
		t.Errorf("Expected no error with empty webhook URL, got: %v", err)
	}
}

func TestSlackAlerterHTTPError(t *testing.T) {
	// Mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		WebhookURL: server.URL,
		Enabled:    true,
	}

	alerter := NewSlackAlerter(cfg)

	err := alerter.SendAlert("Test", "Test message")
	if err == nil {
		t.Error("Expected error from HTTP 500 response")
	}
}

func TestSlackAlerterFormatAlert(t *testing.T) {
	cfg := &config.SlackConfig{
		Channel:   "#test",
		Username:  "TestBot",
		IconEmoji: ":test:",
	}

	alerter := NewSlackAlerter(cfg)

	msg := alerter.formatAlert("Test Title", "Test Body", "warning")

	if msg.Channel != "#test" {
		t.Errorf("Expected channel #test, got %s", msg.Channel)
	}

	if msg.Username != "TestBot" {
		t.Errorf("Expected username TestBot, got %s", msg.Username)
	}

	if msg.IconEmoji != ":test:" {
		t.Errorf("Expected icon :test:, got %s", msg.IconEmoji)
	}

	// Check blocks structure
	if len(msg.Blocks) < 2 {
		t.Error("Expected at least 2 blocks (header and section)")
	}

	// Check header block
	if msg.Blocks[0].Type != "header" {
		t.Errorf("Expected first block to be header, got %s", msg.Blocks[0].Type)
	}

	if msg.Blocks[0].Text == nil {
		t.Error("Expected header block to have text")
	}

	if !strings.Contains(msg.Blocks[0].Text.Text, "Test Title") {
		t.Error("Expected header to contain title")
	}

	if !strings.Contains(msg.Blocks[0].Text.Text, "⚠️") {
		t.Error("Expected warning emoji in header for warning color")
	}

	// Check section block
	if msg.Blocks[1].Type != "section" {
		t.Errorf("Expected second block to be section, got %s", msg.Blocks[1].Type)
	}

	if msg.Blocks[1].Text == nil {
		t.Error("Expected section block to have text")
	}

	if !strings.Contains(msg.Blocks[1].Text.Text, "Test Body") {
		t.Error("Expected section to contain body text")
	}
}
