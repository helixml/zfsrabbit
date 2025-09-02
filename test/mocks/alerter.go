package mocks

import (
	"time"
)

// MockAlerter mocks alert functionality
type MockAlerter struct {
	SentAlerts       []Alert
	SyncSuccesses    []SyncSuccess
	SyncFailures     []SyncFailure
	SystemStatuses   []map[string]interface{}
	SendAlertError   error
	TestConnectError error
}

type Alert struct {
	Subject string
	Body    string
}

type SyncSuccess struct {
	Snapshot string
	Dataset  string
	Duration time.Duration
}

type SyncFailure struct {
	Snapshot string
	Dataset  string
	Error    error
}

func NewMockAlerter() *MockAlerter {
	return &MockAlerter{
		SentAlerts:     make([]Alert, 0),
		SyncSuccesses:  make([]SyncSuccess, 0),
		SyncFailures:   make([]SyncFailure, 0),
		SystemStatuses: make([]map[string]interface{}, 0),
	}
}

func (m *MockAlerter) SendAlert(subject, body string) error {
	m.SentAlerts = append(m.SentAlerts, Alert{
		Subject: subject,
		Body:    body,
	})
	return m.SendAlertError
}

func (m *MockAlerter) SendSyncSuccess(snapshot, dataset string, duration time.Duration) error {
	m.SyncSuccesses = append(m.SyncSuccesses, SyncSuccess{
		Snapshot: snapshot,
		Dataset:  dataset,
		Duration: duration,
	})
	return nil
}

func (m *MockAlerter) SendSyncFailure(snapshot, dataset string, err error) error {
	m.SyncFailures = append(m.SyncFailures, SyncFailure{
		Snapshot: snapshot,
		Dataset:  dataset,
		Error:    err,
	})
	return nil
}

func (m *MockAlerter) SendSystemStatus(status map[string]interface{}) error {
	m.SystemStatuses = append(m.SystemStatuses, status)
	return nil
}

func (m *MockAlerter) TestConnection() error {
	return m.TestConnectError
}

func (m *MockAlerter) GetLastAlert() *Alert {
	if len(m.SentAlerts) == 0 {
		return nil
	}
	return &m.SentAlerts[len(m.SentAlerts)-1]
}

func (m *MockAlerter) GetAlertCount() int {
	return len(m.SentAlerts)
}

func (m *MockAlerter) HasAlert(subject string) bool {
	for _, alert := range m.SentAlerts {
		if alert.Subject == subject {
			return true
		}
	}
	return false
}

func (m *MockAlerter) GetSyncSuccessCount() int {
	return len(m.SyncSuccesses)
}

func (m *MockAlerter) GetSyncFailureCount() int {
	return len(m.SyncFailures)
}

func (m *MockAlerter) Clear() {
	m.SentAlerts = make([]Alert, 0)
	m.SyncSuccesses = make([]SyncSuccess, 0)
	m.SyncFailures = make([]SyncFailure, 0)
	m.SystemStatuses = make([]map[string]interface{}, 0)
}

// MockEmailAlerter specifically for email functionality
type MockEmailAlerter struct {
	*MockAlerter
}

func NewMockEmailAlerter() *MockEmailAlerter {
	return &MockEmailAlerter{
		MockAlerter: NewMockAlerter(),
	}
}

// MockSlackAlerter specifically for Slack functionality
type MockSlackAlerter struct {
	*MockAlerter
	WebhookCalls []WebhookCall
}

type WebhookCall struct {
	URL     string
	Payload string
	Error   error
}

func NewMockSlackAlerter() *MockSlackAlerter {
	return &MockSlackAlerter{
		MockAlerter:  NewMockAlerter(),
		WebhookCalls: make([]WebhookCall, 0),
	}
}

func (m *MockSlackAlerter) AddWebhookCall(url, payload string, err error) {
	m.WebhookCalls = append(m.WebhookCalls, WebhookCall{
		URL:     url,
		Payload: payload,
		Error:   err,
	})
}
