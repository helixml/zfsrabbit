package validation

import (
	"testing"
)

func TestValidateDatasetName(t *testing.T) {
	tests := []struct {
		name    string
		dataset string
		valid   bool
	}{
		{"valid simple name", "tank", true},
		{"valid hierarchical name", "tank/data", true},
		{"valid with underscore", "tank_backup/data", true},
		{"valid with dash", "tank-backup/data", true},
		{"empty name", "", false},
		{"with semicolon", "tank;rm -rf /", false},
		{"with pipe", "tank|cat /etc/passwd", false},
		{"with backtick", "tank`whoami`", false},
		{"with dollar", "tank$USER", false},
		{"too long", string(make([]byte, 300)), false},
		{"starts with number", "123tank", true},
		{"contains dot", "tank.backup", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDatasetName(tt.dataset)
			if tt.valid && err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.dataset, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected %s to be invalid, but validation passed", tt.dataset)
			}
		})
	}
}

func TestValidateSnapshotName(t *testing.T) {
	tests := []struct {
		name     string
		snapshot string
		valid    bool
	}{
		{"valid simple name", "snapshot1", true},
		{"valid with date", "backup-2024-01-01", true},
		{"valid with underscore", "auto_snap", true},
		{"empty name", "", false},
		{"with slash", "snap/shot", false},
		{"with at symbol", "snap@shot", false},
		{"with semicolon", "snap;rm", false},
		{"with backtick", "snap`whoami`", false},
		{"valid with dots", "snap.2024.01.01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnapshotName(tt.snapshot)
			if tt.valid && err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.snapshot, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected %s to be invalid, but validation passed", tt.snapshot)
			}
		})
	}
}

func TestValidateEmailAddress(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		{"valid email", "user@example.com", true},
		{"valid with plus", "user+tag@example.com", true},
		{"valid with subdomain", "user@mail.example.com", true},
		{"empty email", "", false},
		{"no at symbol", "userexample.com", false},
		{"no domain", "user@", false},
		{"no user", "@example.com", false},
		{"invalid characters", "user@exam<ple.com", false},
		{"missing TLD", "user@example", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmailAddress(tt.email)
			if tt.valid && err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.email, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected %s to be invalid, but validation passed", tt.email)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name  string
		port  int
		valid bool
	}{
		{"valid port 80", 80, true},
		{"valid port 8080", 8080, true},
		{"valid port 65535", 65535, true},
		{"invalid port 0", 0, false},
		{"invalid port -1", -1, false},
		{"invalid port 65536", 65536, false},
		{"valid port 1", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if tt.valid && err != nil {
				t.Errorf("Expected port %d to be valid, got error: %v", tt.port, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected port %d to be invalid, but validation passed", tt.port)
			}
		})
	}
}

func TestSanitizeCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean input", "tank/data", "tank/data"},
		{"with semicolon", "tank;rm -rf", "tankrm -rf"},
		{"with backtick", "tank`whoami`", "tank\\`whoami\\`"},
		{"with dollar", "tank$USER", "tank\\$USER"},
		{"multiple dangerous chars", "test;|&$", "test\\$"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeCommand(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}