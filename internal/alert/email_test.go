package alert

import (
	"strings"
	"testing"

	"zfsrabbit/internal/config"
)

func TestNewEmailAlerter(t *testing.T) {
	cfg := &config.EmailConfig{
		SMTPHost:     "smtp.test.com",
		SMTPPort:     587,
		SMTPUser:     "test@test.com",
		SMTPPassword: "password",
		FromEmail:    "alerts@test.com",
		ToEmails:     []string{"admin@test.com"},
		UseTLS:       true,
	}

	alerter := NewEmailAlerter(cfg)

	if alerter.config != cfg {
		t.Error("Config not set correctly")
	}
}

func TestEmailAlerterBuildMessage(t *testing.T) {
	cfg := &config.EmailConfig{
		FromEmail: "alerts@test.com",
		ToEmails:  []string{"admin@test.com", "sysadmin@test.com"},
	}

	alerter := NewEmailAlerter(cfg)

	subject := "Test Alert"
	body := "This is a test message"

	message := alerter.buildMessage(subject, body)

	// Check headers
	if !strings.Contains(message, "From: alerts@test.com") {
		t.Error("From header not set correctly")
	}

	if !strings.Contains(message, "To: admin@test.com;sysadmin@test.com") {
		t.Error("To header not set correctly")
	}

	if !strings.Contains(message, "Subject: [ZFSRabbit] Test Alert") {
		t.Error("Subject header not set correctly")
	}

	if !strings.Contains(message, "MIME-Version: 1.0") {
		t.Error("MIME-Version header missing")
	}

	if !strings.Contains(message, "Content-Type: text/plain; charset=utf-8") {
		t.Error("Content-Type header not set correctly")
	}

	// Check body
	if !strings.Contains(message, body) {
		t.Error("Message body not included")
	}

	// Check structure (headers, blank line, body)
	parts := strings.Split(message, "\r\n\r\n")
	if len(parts) != 2 {
		t.Error("Message structure incorrect - should have headers, blank line, then body")
	}
}

func TestEmailAlerterSendAlert(t *testing.T) {
	cfg := &config.EmailConfig{
		SMTPHost:     "",  // Empty host will cause validation error
		SMTPPort:     587,
		FromEmail:    "alerts@test.com",
		ToEmails:     []string{},  // Empty recipients
		UseTLS:       false,
	}

	alerter := NewEmailAlerter(cfg)

	err := alerter.SendAlert("Test", "Test message")
	
	// Should fail due to incomplete configuration
	if err == nil {
		t.Error("Expected error due to incomplete configuration")
	}
}

func TestEmailAlerterTestConnection(t *testing.T) {
	cfg := &config.EmailConfig{
		SMTPHost: "",  // Empty config should cause error
	}

	alerter := NewEmailAlerter(cfg)

	err := alerter.TestConnection()
	
	// Should call SendAlert with test message, which should fail
	if err == nil {
		t.Error("Expected error from test connection")
	}
}