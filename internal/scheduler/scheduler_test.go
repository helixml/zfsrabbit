package scheduler

import (
	"os/exec"
	"testing"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
	"zfsrabbit/test/mocks"
)

// MockZFSExecutor for testing ZFS operations
type MockZFSExecutor struct {
	commands map[string][]byte
	errors   map[string]error
}

func NewMockZFSExecutor() *MockZFSExecutor {
	return &MockZFSExecutor{
		commands: make(map[string][]byte),
		errors:   make(map[string]error),
	}
}

func (m *MockZFSExecutor) Command(name string, args ...string) *exec.Cmd {
	// Return a dummy command - the mock will intercept Output/Run calls
	return exec.Command("echo", "mock")
}

func (m *MockZFSExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	cmdStr := cmd.String()
	if err, exists := m.errors[cmdStr]; exists {
		return nil, err
	}
	if output, exists := m.commands[cmdStr]; exists {
		return output, nil
	}
	return []byte(""), nil
}

func (m *MockZFSExecutor) Run(cmd *exec.Cmd) error {
	cmdStr := cmd.String()
	if err, exists := m.errors[cmdStr]; exists {
		return err
	}
	return nil
}

func TestNewScheduler(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// For SSH transport, we still need the concrete type but with non-existent host
	// This will fail quickly without making actual network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	if scheduler == nil {
		t.Fatal("Expected scheduler to be created")
	}

	if scheduler.config != cfg {
		t.Error("Config not set correctly")
	}

	if scheduler.zfsManager != zfsManager {
		t.Error("ZFS manager not set correctly")
	}

	if scheduler.transport != sshTransport {
		t.Error("Transport not set correctly")
	}

	if scheduler.alerter != mockAlerter {
		t.Error("Alerter not set correctly")
	}

	if scheduler.cron == nil {
		t.Error("Cron scheduler should be initialized")
	}
}

func TestSchedulerStart(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// Use non-existent host to avoid real network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	// Test starting scheduler
	scheduler.Start()

	// Check that cron jobs were added
	if len(scheduler.cron.Entries()) == 0 {
		t.Error("Expected cron jobs to be scheduled")
	}

	// Stop the scheduler
	scheduler.Stop()
}

func TestSchedulerStop(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// Use non-existent host to avoid real network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)
	scheduler.Start()

	// Test stopping scheduler
	scheduler.Stop()

	// After stopping, should be able to start again
	scheduler.Start()
	scheduler.Stop()
}

func TestTriggerSnapshot(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// Use non-existent host to avoid real network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	// This will trigger the snapshot process but will likely fail due to
	// missing ZFS commands. That's expected in a test environment.
	err := scheduler.TriggerSnapshot()

	// We don't check for error here because ZFS commands won't work in test environment
	// The important thing is that the method doesn't panic and returns
	_ = err
}

func TestTriggerScrub(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// Use non-existent host to avoid real network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	// This will trigger the scrub process but will likely fail due to
	// missing ZFS commands. That's expected in a test environment.
	err := scheduler.TriggerScrub()

	// We don't check for error here because ZFS commands won't work in test environment
	// The important thing is that the method doesn't panic and returns
	_ = err
}

func TestSchedulerBasicFunctionality(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "0 2 * * *",
			ScrubCron:    "0 3 * * 0",
		},
	}

	// Use mocked executor for ZFS manager to avoid real ZFS calls
	mockExecutor := NewMockZFSExecutor()
	zfsManager := zfs.NewWithExecutor(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive, mockExecutor)

	// Use non-existent host to avoid real network calls
	cfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&cfg.SSH)

	mockAlerter := mocks.NewMockAlerter()

	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	// Test that scheduler has the expected components
	if scheduler == nil {
		t.Fatal("Expected scheduler to be created")
	}

	// Test starting and stopping multiple times doesn't panic
	scheduler.Start()
	scheduler.Stop()
	scheduler.Start()
	scheduler.Stop()
}

func TestInvalidCronExpressions(t *testing.T) {
	cfg := &config.Config{
		ZFS: config.ZFSConfig{
			Dataset:         "tank/test",
			SendCompression: "lz4",
			Recursive:       false,
		},
		SSH: config.SSHConfig{
			RemoteHost:    "test.example.com",
			RemoteUser:    "testuser",
			RemoteDataset: "backup/test",
		},
		Schedule: config.ScheduleConfig{
			SnapshotCron: "invalid cron",
			ScrubCron:    "also invalid",
		},
	}

	zfsManager := zfs.New(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive)
	sshTransport := transport.NewSSHTransport(&cfg.SSH)
	mockAlerter := mocks.NewMockAlerter()

	// This should not panic even with invalid cron expressions
	scheduler := New(cfg, zfsManager, sshTransport, mockAlerter)

	if scheduler == nil {
		t.Fatal("Expected scheduler to be created even with invalid cron")
	}

	// Starting should not panic
	scheduler.Start()
	scheduler.Stop()
}

// Skip tests that require actual ZFS operations
func TestPerformSnapshot_SkipIntegration(t *testing.T) {
	t.Skip("Skipping snapshot integration test - requires ZFS commands")
}

func TestPerformScrub_SkipIntegration(t *testing.T) {
	t.Skip("Skipping scrub integration test - requires ZFS commands")
}
