package transport

import (
	"testing"

	"zfsrabbit/internal/config"
)

func TestNewSSHTransport(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		PrivateKey:    "/path/to/key",
		RemoteDataset: "backup/test",
		MbufferSize:   "1G",
	}

	transport := NewSSHTransport(cfg)

	if transport.config != cfg {
		t.Error("Config not set correctly")
	}

	if transport.client != nil {
		t.Error("Client should be nil initially")
	}
}

func TestSSHTransportConnect(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "nonexistent.example.com",
		RemoteUser:    "testuser",
		PrivateKey:    "/nonexistent/key",
		RemoteDataset: "backup/test",
		MbufferSize:   "1G",
	}

	transport := NewSSHTransport(cfg)

	// This will fail because we don't have a real SSH connection
	// but it tests the connect logic
	err := transport.Connect()
	if err == nil {
		t.Error("Expected error when connecting to nonexistent host")
	}
}

func TestSSHTransportClose(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		PrivateKey:    "/path/to/key",
		RemoteDataset: "backup/test",
		MbufferSize:   "1G",
	}

	transport := NewSSHTransport(cfg)

	// Close should work even with no active connection
	err := transport.Close()
	if err != nil {
		t.Errorf("Unexpected error closing transport: %v", err)
	}
}

// Skip the SSH functionality tests that require actual network connections
// and ZFS commands. These would be better as integration tests with 
// proper test infrastructure.

func TestSSHTransportListRemoteSnapshots_SkipIntegration(t *testing.T) {
	t.Skip("Skipping SSH integration test - requires live SSH connection and ZFS")
}

func TestSSHTransportListAllRemoteDatasets_SkipIntegration(t *testing.T) {
	t.Skip("Skipping SSH integration test - requires live SSH connection and ZFS")
}

func TestSSHTransportGetRemoteDatasetInfo_SkipIntegration(t *testing.T) {
	t.Skip("Skipping SSH integration test - requires live SSH connection and ZFS")
}

func TestSSHTransportSendSnapshot_SkipIntegration(t *testing.T) {
	t.Skip("Skipping SSH integration test - requires live SSH connection and ZFS")
}

func TestSSHTransportRestoreSnapshot_SkipIntegration(t *testing.T) {
	t.Skip("Skipping SSH integration test - requires live SSH connection and ZFS")
}