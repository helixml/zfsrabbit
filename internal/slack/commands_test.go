package slack

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/monitor"
	"zfsrabbit/internal/restore"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
	"zfsrabbit/test/mocks"
)

// MockZFSExecutor for Slack tests
type MockZFSExecutor struct{}

func (m *MockZFSExecutor) Command(name string, args ...string) *exec.Cmd {
	return exec.Command("echo", "mock")
}

func (m *MockZFSExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	return []byte(""), nil
}

func (m *MockZFSExecutor) Run(cmd *exec.Cmd) error {
	return nil
}

func createTestHandler(t *testing.T) *CommandHandler {
	cfg := &config.SlackConfig{
		SlashToken: "test-token",
	}

	// Create real components for testing
	zfsCfg := &config.Config{
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
	}

	// Use mocked components to avoid real ZFS/SSH operations
	mockExecutor := &MockZFSExecutor{}
	zfsManager := zfs.NewWithExecutor(zfsCfg.ZFS.Dataset, zfsCfg.ZFS.SendCompression, zfsCfg.ZFS.Recursive, mockExecutor)
	
	// Use non-existent host to avoid real network calls
	zfsCfg.SSH.RemoteHost = "nonexistent.test.invalid"
	sshTransport := transport.NewSSHTransport(&zfsCfg.SSH)
	
	mockAlerter := mocks.NewMockAlerter()
	sched := scheduler.New(zfsCfg, zfsManager, sshTransport, mockAlerter)
	mon := monitor.New(zfsCfg, mockAlerter)
	restoreMgr := restore.New(sshTransport, zfsManager)

	return NewCommandHandler(cfg, sched, mon, zfsManager, restoreMgr, sshTransport)
}

func TestNewCommandHandler(t *testing.T) {
	handler := createTestHandler(t)

	if handler == nil {
		t.Fatal("Expected command handler to be created")
	}

	if handler.config == nil {
		t.Error("Config not set correctly")
	}

	if handler.scheduler == nil {
		t.Error("Scheduler not set correctly")
	}

	if handler.monitor == nil {
		t.Error("Monitor not set correctly")
	}

	if handler.zfsManager == nil {
		t.Error("ZFS manager not set correctly")
	}

	if handler.restoreManager == nil {
		t.Error("Restore manager not set correctly")
	}

	if handler.transport == nil {
		t.Error("Transport not set correctly")
	}
}

func TestSlackCommandsInvalidToken(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "wrong-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "status")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestSlackCommandsValidToken(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "help")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ZFSRabbit Commands") {
		t.Error("Expected help text in response")
	}
}

func TestSlackCommandsStatus(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "status")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Should return some status info (even if ZFS commands fail)
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected non-empty response for status command")
	}
}

func TestSlackCommandsSnapshot(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "snapshot")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Should trigger snapshot (will likely fail without ZFS but shouldn't crash)
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected non-empty response for snapshot command")
	}
}

func TestSlackCommandsScrub(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "scrub")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Should trigger scrub (will likely fail without ZFS but shouldn't crash)
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected non-empty response for scrub command")
	}
}

func TestSlackCommandsJobs(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "jobs")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Should return job list (initially empty)
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected non-empty response for jobs command")
	}
}

func TestSlackCommandsUnknown(t *testing.T) {
	handler := createTestHandler(t)

	data := url.Values{}
	data.Set("token", "test-token")
	data.Set("command", "/zfsrabbit")
	data.Set("text", "unknown")

	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Unknown command") {
		t.Error("Expected 'Unknown command' message for unknown command")
	}
}

func TestSlackCommandsInvalidMethod(t *testing.T) {
	handler := createTestHandler(t)

	req := httptest.NewRequest("GET", "/slack/command", nil)
	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestSlackCommandsInvalidForm(t *testing.T) {
	handler := createTestHandler(t)

	// Send invalid form data with valid token
	data := url.Values{}
	data.Set("token", "test-token")
	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader("invalid form data&token=test-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handler.HandleSlashCommand(w, req)

	// The form parsing should succeed and return OK since token is valid
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Skip tests that require actual ZFS/SSH operations
func TestSlackCommandsRemote_SkipIntegration(t *testing.T) {
	t.Skip("Skipping remote dataset commands - requires SSH connectivity")
}

func TestSlackCommandsRestore_SkipIntegration(t *testing.T) {
	t.Skip("Skipping restore commands - requires ZFS and SSH connectivity")
}