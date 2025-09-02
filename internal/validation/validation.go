package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// ZFS dataset name validation regex - based on ZFS naming rules
var datasetNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?(/[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?)*$`)

// ZFS snapshot name validation regex
var snapshotNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)

// ValidateDatasetName validates ZFS dataset names to prevent injection attacks
func ValidateDatasetName(name string) error {
	if name == "" {
		return fmt.Errorf("dataset name cannot be empty")
	}

	if len(name) > 255 {
		return fmt.Errorf("dataset name too long (max 255 characters)")
	}

	// Check for dangerous characters and command injection attempts
	if strings.ContainsAny(name, ";|&$`\"'\\*?[]{}()<>") {
		return fmt.Errorf("dataset name contains invalid characters")
	}

	if !datasetNameRegex.MatchString(name) {
		return fmt.Errorf("invalid dataset name format")
	}

	return nil
}

// ValidateSnapshotName validates ZFS snapshot names
func ValidateSnapshotName(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name cannot be empty")
	}

	if len(name) > 255 {
		return fmt.Errorf("snapshot name too long (max 255 characters)")
	}

	// Check for dangerous characters
	if strings.ContainsAny(name, ";|&$`\"'\\*?[]{}()<>/@") {
		return fmt.Errorf("snapshot name contains invalid characters")
	}

	if !snapshotNameRegex.MatchString(name) {
		return fmt.Errorf("invalid snapshot name format")
	}

	return nil
}

// SanitizeCommand sanitizes shell command arguments by escaping dangerous characters
func SanitizeCommand(arg string) string {
	// Remove or escape potentially dangerous characters
	replacer := strings.NewReplacer(
		";", "",
		"|", "",
		"&", "",
		"$", "\\$",
		"`", "\\`",
		"\"", "\\\"",
		"'", "\\'",
		"\\", "\\\\",
	)
	return replacer.Replace(arg)
}

// ValidateEmailAddress validates email addresses
func ValidateEmailAddress(email string) error {
	if email == "" {
		return fmt.Errorf("email address cannot be empty")
	}

	// Simple email validation - not RFC compliant but secure
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("invalid email address format")
	}

	return nil
}

// ValidatePort validates network port numbers
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
