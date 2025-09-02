package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
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
		return nil, err
	}

	return cfg, nil
}

func (c *Config) GetAdminPassword() string {
	return os.Getenv(c.Server.AdminPassEnv)
}
