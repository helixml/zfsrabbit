package alert

import (
	"fmt"
	"time"

	"zfsrabbit/internal/config"
)

type MultiAlerter struct {
	email *EmailAlerter
	slack *SlackAlerter
}

func NewMultiAlerter(emailCfg *config.EmailConfig, slackCfg *config.SlackConfig) *MultiAlerter {
	return &MultiAlerter{
		email: NewEmailAlerter(emailCfg),
		slack: NewSlackAlerter(slackCfg),
	}
}

func (m *MultiAlerter) SendAlert(subject, body string) error {
	var errs []error
	
	if err := m.email.SendAlert(subject, body); err != nil {
		errs = append(errs, fmt.Errorf("email alert failed: %w", err))
	}
	
	if err := m.slack.SendAlert(subject, body); err != nil {
		errs = append(errs, fmt.Errorf("slack alert failed: %w", err))
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("alert failures: %v", errs)
	}
	
	return nil
}

func (m *MultiAlerter) SendSyncSuccess(snapshot, dataset string, duration time.Duration) error {
	var errs []error
	
	if err := m.slack.SendSyncSuccess(snapshot, dataset, duration); err != nil {
		errs = append(errs, fmt.Errorf("slack sync success alert failed: %w", err))
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("sync success alert failures: %v", errs)
	}
	
	return nil
}

func (m *MultiAlerter) SendSyncFailure(snapshot, dataset string, err error) error {
	var errs []error
	
	if slackErr := m.slack.SendSyncFailure(snapshot, dataset, err); slackErr != nil {
		errs = append(errs, fmt.Errorf("slack sync failure alert failed: %w", slackErr))
	}
	
	// Also send email for failures
	subject := "ZFS Sync Failed"
	body := fmt.Sprintf("Failed to replicate snapshot %s from dataset %s\nError: %s", snapshot, dataset, err.Error())
	if emailErr := m.email.SendAlert(subject, body); emailErr != nil {
		errs = append(errs, fmt.Errorf("email sync failure alert failed: %w", emailErr))
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("sync failure alert failures: %v", errs)
	}
	
	return nil
}

func (m *MultiAlerter) SendSystemStatus(status map[string]interface{}) error {
	return m.slack.SendSystemStatus(status)
}

func (m *MultiAlerter) TestConnection() error {
	var errs []error
	
	if err := m.email.TestConnection(); err != nil {
		errs = append(errs, fmt.Errorf("email test failed: %w", err))
	}
	
	if err := m.slack.TestConnection(); err != nil {
		errs = append(errs, fmt.Errorf("slack test failed: %w", err))
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("connection test failures: %v", errs)
	}
	
	return nil
}