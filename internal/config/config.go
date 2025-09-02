package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
	"zfsrabbit/internal/validation"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	ZFS      ZFSConfig      `yaml:"zfs"`
	SSH      SSHConfig      `yaml:"ssh"`
	Email    EmailConfig    `yaml:"email"`
	Slack    SlackConfig    `yaml:"slack"`
	Schedule ScheduleConfig `yaml:"schedule"`
}

type ServerConfig struct {
	Port         int    `yaml:"port"`
	AdminPassEnv string `yaml:"admin_pass_env"`
	LogLevel     string `yaml:"log_level"`
}

type ZFSConfig struct {
	Dataset         string `yaml:"dataset"`
	SendCompression string `yaml:"send_compression"`
	Recursive       bool   `yaml:"recursive"`
}

type SSHConfig struct {
	RemoteHost    string `yaml:"remote_host"`
	RemoteUser    string `yaml:"remote_user"`
	PrivateKey    string `yaml:"private_key"`
	RemoteDataset string `yaml:"remote_dataset"`
	MbufferSize   string `yaml:"mbuffer_size"`
}

type EmailConfig struct {
	SMTPHost     string   `yaml:"smtp_host"`
	SMTPPort     int      `yaml:"smtp_port"`
	SMTPUser     string   `yaml:"smtp_user"`
	SMTPPassword string   `yaml:"smtp_password"`
	FromEmail    string   `yaml:"from_email"`
	ToEmails     []string `yaml:"to_emails"`
	UseTLS       bool     `yaml:"use_tls"`
}

type SlackConfig struct {
	WebhookURL    string `yaml:"webhook_url"`
	Channel       string `yaml:"channel"`
	Username      string `yaml:"username"`
	IconEmoji     string `yaml:"icon_emoji"`
	Enabled       bool   `yaml:"enabled"`
	AlertOnSync   bool   `yaml:"alert_on_sync"`
	AlertOnErrors bool   `yaml:"alert_on_errors"`
	SlashToken    string `yaml:"slash_token"`
}

type ScheduleConfig struct {
	SnapshotCron    string        `yaml:"snapshot_cron"`
	ScrubCron       string        `yaml:"scrub_cron"`
	MonitorInterval time.Duration `yaml:"monitor_interval"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:         8080,
			AdminPassEnv: "ZFSRABBIT_ADMIN_PASSWORD",
			LogLevel:     "info",
		},
		ZFS: ZFSConfig{
			SendCompression: "lz4",
			Recursive:       true,
		},
		SSH: SSHConfig{
			MbufferSize: "1G",
		},
		Email: EmailConfig{
			SMTPPort: 587,
			UseTLS:   true,
		},
		Slack: SlackConfig{
			Username:      "ZFSRabbit",
			IconEmoji:     ":rabbit:",
			Enabled:       false,
			AlertOnSync:   true,
			AlertOnErrors: true,
		},
		Schedule: ScheduleConfig{
			SnapshotCron:    "0 2 * * *", // Daily at 2 AM
			ScrubCron:       "0 3 * * 0", // Weekly on Sunday at 3 AM
			MonitorInterval: 5 * time.Minute,
		},
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration for security and correctness
func (c *Config) Validate() error {
	// Server validation
	if err := validation.ValidatePort(c.Server.Port); err != nil {
		return fmt.Errorf("server port: %w", err)
	}

	if c.Server.AdminPassEnv == "" {
		return fmt.Errorf("server.admin_pass_env cannot be empty")
	}

	// ZFS validation
	if c.ZFS.Dataset == "" {
		return fmt.Errorf("zfs.dataset cannot be empty")
	}

	if err := validation.ValidateDatasetName(c.ZFS.Dataset); err != nil {
		return fmt.Errorf("zfs.dataset: %w", err)
	}

	// SSH validation
	if c.SSH.RemoteHost == "" {
		return fmt.Errorf("ssh.remote_host cannot be empty")
	}

	if c.SSH.RemoteUser == "" {
		return fmt.Errorf("ssh.remote_user cannot be empty")
	}

	if c.SSH.PrivateKey == "" {
		return fmt.Errorf("ssh.private_key cannot be empty")
	}

	if c.SSH.RemoteDataset == "" {
		return fmt.Errorf("ssh.remote_dataset cannot be empty")
	}

	if err := validation.ValidateDatasetName(c.SSH.RemoteDataset); err != nil {
		return fmt.Errorf("ssh.remote_dataset: %w", err)
	}

	// Email validation
	if c.Email.SMTPHost != "" { // Email is optional
		if err := validation.ValidatePort(c.Email.SMTPPort); err != nil {
			return fmt.Errorf("email.smtp_port: %w", err)
		}

		for _, email := range c.Email.ToEmails {
			if err := validation.ValidateEmailAddress(email); err != nil {
				return fmt.Errorf("email.to_emails: %w", err)
			}
		}

		if c.Email.FromEmail != "" {
			if err := validation.ValidateEmailAddress(c.Email.FromEmail); err != nil {
				return fmt.Errorf("email.from_email: %w", err)
			}
		}
	}

	// Slack validation
	if c.Slack.Enabled && c.Slack.WebhookURL != "" {
		if !strings.HasPrefix(c.Slack.WebhookURL, "https://hooks.slack.com/") {
			return fmt.Errorf("slack.webhook_url must be a valid Slack webhook URL")
		}
	}

	// Schedule validation - validate cron expressions
	if c.Schedule.MonitorInterval < time.Minute {
		return fmt.Errorf("schedule.monitor_interval must be at least 1 minute")
	}

	if err := validateCronExpression(c.Schedule.SnapshotCron); err != nil {
		return fmt.Errorf("invalid snapshot_cron expression '%s': %w", c.Schedule.SnapshotCron, err)
	}

	if err := validateCronExpression(c.Schedule.ScrubCron); err != nil {
		return fmt.Errorf("invalid scrub_cron expression '%s': %w", c.Schedule.ScrubCron, err)
	}

	return nil
}

func validateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err
}

func (c *Config) GetAdminPassword() string {
	return os.Getenv(c.Server.AdminPassEnv)
}
