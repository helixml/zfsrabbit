package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"zfsrabbit/internal/config"
)

type SlackAlerter struct {
	config *config.SlackConfig
}

type SlackMessage struct {
	Channel   string       `json:"channel,omitempty"`
	Username  string       `json:"username,omitempty"`
	IconEmoji string       `json:"icon_emoji,omitempty"`
	Text      string       `json:"text,omitempty"`
	Blocks    []SlackBlock `json:"blocks,omitempty"`
}

type SlackBlock struct {
	Type      string          `json:"type"`
	Text      *SlackText      `json:"text,omitempty"`
	Fields    []SlackField    `json:"fields,omitempty"`
	Accessory *SlackAccessory `json:"accessory,omitempty"`
}

type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackField struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackAccessory struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

func NewSlackAlerter(cfg *config.SlackConfig) *SlackAlerter {
	return &SlackAlerter{
		config: cfg,
	}
}

func (s *SlackAlerter) SendAlert(subject, body string) error {
	if !s.config.Enabled || s.config.WebhookURL == "" {
		return nil
	}

	return s.sendMessage(s.formatAlert(subject, body, "warning"))
}

func (s *SlackAlerter) SendSyncSuccess(snapshot, dataset string, duration time.Duration) error {
	if !s.config.Enabled || !s.config.AlertOnSync || s.config.WebhookURL == "" {
		return nil
	}

	title := "✅ ZFS Sync Completed"
	message := fmt.Sprintf("Successfully replicated snapshot `%s` from dataset `%s`\nDuration: %s",
		snapshot, dataset, duration.String())

	return s.sendMessage(s.formatAlert(title, message, "good"))
}

func (s *SlackAlerter) SendSyncFailure(snapshot, dataset string, err error) error {
	if !s.config.Enabled || !s.config.AlertOnSync || s.config.WebhookURL == "" {
		return nil
	}

	title := "❌ ZFS Sync Failed"
	message := fmt.Sprintf("Failed to replicate snapshot `%s` from dataset `%s`\nError: %s",
		snapshot, dataset, err.Error())

	return s.sendMessage(s.formatAlert(title, message, "danger"))
}

func (s *SlackAlerter) SendSystemStatus(status map[string]interface{}) error {
	if !s.config.Enabled || s.config.WebhookURL == "" {
		return nil
	}

	blocks := []SlackBlock{
		{
			Type: "header",
			Text: &SlackText{
				Type: "plain_text",
				Text: "🐰 ZFSRabbit System Status",
			},
		},
	}

	// Pool status
	if pools, ok := status["pools"].(map[string]interface{}); ok {
		fields := []SlackField{}
		for pool, poolStatus := range pools {
			if ps, ok := poolStatus.(map[string]interface{}); ok {
				state, _ := ps["State"].(string)
				emoji := "✅"
				if state != "ONLINE" {
					emoji = "⚠️"
				}
				fields = append(fields, SlackField{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*%s %s:*\n%s", emoji, pool, state),
				})
			}
		}

		if len(fields) > 0 {
			blocks = append(blocks, SlackBlock{
				Type:   "section",
				Fields: fields,
			})
		}
	}

	// Disk status
	if disks, ok := status["disks"].(map[string]interface{}); ok {
		fields := []SlackField{}
		for disk, diskStatus := range disks {
			if ds, ok := diskStatus.(map[string]interface{}); ok {
				healthy, _ := ds["Healthy"].(bool)
				temp, _ := ds["Temperature"].(int)
				emoji := "✅"
				if !healthy {
					emoji = "❌"
				} else if temp > 50 {
					emoji = "🔥"
				}
				fields = append(fields, SlackField{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*%s %s:*\n%s %d°C", emoji, disk,
						map[bool]string{true: "Healthy", false: "Issues"}[healthy], temp),
				})
			}
		}

		if len(fields) > 0 {
			blocks = append(blocks, SlackBlock{
				Type:   "section",
				Fields: fields,
			})
		}
	}

	blocks = append(blocks, SlackBlock{
		Type: "context",
		Fields: []SlackField{
			{
				Type: "mrkdwn",
				Text: fmt.Sprintf("Updated: %s", time.Now().Format("2006-01-02 15:04:05")),
			},
		},
	})

	msg := SlackMessage{
		Channel:   s.config.Channel,
		Username:  s.config.Username,
		IconEmoji: s.config.IconEmoji,
		Blocks:    blocks,
	}

	return s.sendMessage(msg)
}

func (s *SlackAlerter) formatAlert(title, body, color string) SlackMessage {
	colorEmoji := map[string]string{
		"good":    "✅",
		"warning": "⚠️",
		"danger":  "❌",
	}

	emoji := colorEmoji[color]
	if emoji == "" {
		emoji = "ℹ️"
	}

	blocks := []SlackBlock{
		{
			Type: "header",
			Text: &SlackText{
				Type: "plain_text",
				Text: fmt.Sprintf("%s %s", emoji, title),
			},
		},
		{
			Type: "section",
			Text: &SlackText{
				Type: "mrkdwn",
				Text: body,
			},
		},
		{
			Type: "context",
			Fields: []SlackField{
				{
					Type: "mrkdwn",
					Text: fmt.Sprintf("Time: %s", time.Now().Format("2006-01-02 15:04:05")),
				},
			},
		},
	}

	return SlackMessage{
		Channel:   s.config.Channel,
		Username:  s.config.Username,
		IconEmoji: s.config.IconEmoji,
		Blocks:    blocks,
	}
}

func (s *SlackAlerter) sendMessage(msg SlackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack message: %w", err)
	}

	resp, err := http.Post(s.config.WebhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to send Slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack API returned status %d", resp.StatusCode)
	}

	return nil
}

// Implement the Alerter interface
func (s *SlackAlerter) TestConnection() error {
	return s.SendAlert("Test Alert", "This is a test message from ZFSRabbit to verify Slack integration.")
}
