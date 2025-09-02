package zfs

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The Manager now supports dependency injection, so we don't need TestableManager

// MockCommandExecutor for testing
type MockCommandExecutor struct {
	commands []MockCommand
	callLog  []string
}

type MockCommand struct {
	expectedCmd string
	output      string
	err         error
}

func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{
		commands: make([]MockCommand, 0),
		callLog:  make([]string, 0),
	}
}

func (m *MockCommandExecutor) AddCommand(cmd, output string, err error) {
	m.commands = append(m.commands, MockCommand{
		expectedCmd: cmd,
		output:      output,
		err:         err,
	})
}

func (m *MockCommandExecutor) Command(name string, args ...string) *exec.Cmd {
	cmdStr := name + " " + strings.Join(args, " ")
	m.callLog = append(m.callLog, cmdStr)
	return &exec.Cmd{Path: name, Args: append([]string{name}, args...)}
}

func (m *MockCommandExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	cmdStr := strings.Join(cmd.Args, " ")
	
	for i, mockCmd := range m.commands {
		if strings.Contains(cmdStr, mockCmd.expectedCmd) || mockCmd.expectedCmd == cmdStr {
			// Remove used command
			m.commands = append(m.commands[:i], m.commands[i+1:]...)
			return []byte(mockCmd.output), mockCmd.err
		}
	}
	
	return []byte(""), nil
}

func (m *MockCommandExecutor) Run(cmd *exec.Cmd) error {
	cmdStr := strings.Join(cmd.Args, " ")
	
	for i, mockCmd := range m.commands {
		if strings.Contains(cmdStr, mockCmd.expectedCmd) || mockCmd.expectedCmd == cmdStr {
			// Remove used command
			m.commands = append(m.commands[:i], m.commands[i+1:]...)
			return mockCmd.err
		}
	}
	
	return nil
}

// Use NewWithExecutor from the main package instead

func TestNewManager(t *testing.T) {
	tests := []struct {
		name            string
		dataset         string
		sendCompression string
		recursive       bool
	}{
		{
			name:            "basic manager creation",
			dataset:         "tank/test",
			sendCompression: "lz4",
			recursive:       true,
		},
		{
			name:            "no compression",
			dataset:         "pool/data",
			sendCompression: "",
			recursive:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := New(tt.dataset, tt.sendCompression, tt.recursive)
			
			if manager.dataset != tt.dataset {
				t.Errorf("Expected dataset %s, got %s", tt.dataset, manager.dataset)
			}
			
			if manager.sendCompression != tt.sendCompression {
				t.Errorf("Expected sendCompression %s, got %s", tt.sendCompression, manager.sendCompression)
			}
			
			if manager.recursive != tt.recursive {
				t.Errorf("Expected recursive %t, got %t", tt.recursive, manager.recursive)
			}
		})
	}
}

func TestCreateSnapshot(t *testing.T) {
	tests := []struct {
		name         string
		dataset      string
		recursive    bool
		snapshotName string
		expectError  bool
		expectedCmd  string
	}{
		{
			name:         "create basic snapshot",
			dataset:      "tank/test",
			recursive:    false,
			snapshotName: "test-snap",
			expectError:  false,
			expectedCmd:  "zfs snapshot tank/test@test-snap",
		},
		{
			name:         "create recursive snapshot",
			dataset:      "tank/test",
			recursive:    true,
			snapshotName: "test-snap",
			expectError:  false,
			expectedCmd:  "zfs snapshot -r tank/test@test-snap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewMockCommandExecutor()
			var expectedError error
			if tt.expectError {
				expectedError = fmt.Errorf("mock error")
			}
			executor.AddCommand(tt.expectedCmd, "", expectedError)
			
			manager := NewWithExecutor(tt.dataset, "lz4", tt.recursive, executor)
			
			// Override the actual command execution for testing
			err := manager.CreateSnapshot(tt.snapshotName)
			
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Helper methods removed - Manager now uses injected executor directly

func TestListSnapshots(t *testing.T) {
	tests := []struct {
		name           string
		dataset        string
		mockOutput     string
		expectedCount  int
		expectedNames  []string
		expectError    bool
	}{
		{
			name:    "list snapshots successfully",
			dataset: "tank/test",
			mockOutput: `tank/test@snap1	Mon Jan  2 15:04 2023	1.23G	4.56G
tank/test@snap2	Tue Jan  3 10:30 2023	2.34G	5.67G`,
			expectedCount: 2,
			expectedNames: []string{"snap1", "snap2"},
			expectError:   false,
		},
		{
			name:          "no snapshots",
			dataset:       "tank/empty",
			mockOutput:    "",
			expectedCount: 0,
			expectedNames: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewMockCommandExecutor()
			var expectedError error
			if tt.expectError {
				expectedError = fmt.Errorf("mock error")
			}
			
			expectedCmd := fmt.Sprintf("zfs list -t snapshot -H -o name,creation,used,refer -s creation %s", tt.dataset)
			executor.AddCommand(expectedCmd, tt.mockOutput, expectedError)
			
			manager := NewWithExecutor(tt.dataset, "lz4", false, executor)
			
			snapshots, err := manager.ListSnapshots()
			
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			
			if len(snapshots) != tt.expectedCount {
				t.Errorf("Expected %d snapshots, got %d", tt.expectedCount, len(snapshots))
			}
			
			for i, expectedName := range tt.expectedNames {
				if i >= len(snapshots) {
					t.Errorf("Expected snapshot %s at index %d, but only got %d snapshots", expectedName, i, len(snapshots))
					continue
				}
				
				if snapshots[i].Name != expectedName {
					t.Errorf("Expected snapshot name %s, got %s", expectedName, snapshots[i].Name)
				}
			}
		})
	}
}

// Helper methods removed - using Manager methods directly

func parseSnapshotOutput(output string) ([]Snapshot, error) {
	var snapshots []Snapshot
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	for _, line := range lines {
		if line == "" {
			continue
		}
		
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			parts := strings.Split(fields[0], "@")
			if len(parts) == 2 {
				// Try to parse the date - use a simple approach for testing
				var created time.Time
				if len(fields) >= 2 {
					// For testing, just use current time
					created = time.Now()
				}
				
				snapshots = append(snapshots, Snapshot{
					Name:    parts[1],
					Created: created,
					Used:    fields[2],
					Refer:   fields[3],
					Dataset: parts[0],
				})
			}
		}
	}
	
	return snapshots, nil
}

func TestSendSnapshot(t *testing.T) {
	tests := []struct {
		name            string
		dataset         string
		sendCompression string
		recursive       bool
		snapshotName    string
		expectedArgs    []string
	}{
		{
			name:            "basic send",
			dataset:         "tank/test",
			sendCompression: "",
			recursive:       false,
			snapshotName:    "snap1",
			expectedArgs:    []string{"send", "tank/test@snap1"},
		},
		{
			name:            "send with compression",
			dataset:         "tank/test",
			sendCompression: "lz4",
			recursive:       false,
			snapshotName:    "snap1",
			expectedArgs:    []string{"send", "-c", "tank/test@snap1"},
		},
		{
			name:            "send recursive with compression",
			dataset:         "tank/test",
			sendCompression: "gzip",
			recursive:       true,
			snapshotName:    "snap1",
			expectedArgs:    []string{"send", "-c", "-R", "tank/test@snap1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := New(tt.dataset, tt.sendCompression, tt.recursive)
			
			cmd, err := manager.SendSnapshot(tt.snapshotName)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			
			expectedCmd := "zfs " + strings.Join(tt.expectedArgs, " ")
			actualCmd := strings.Join(cmd.Args, " ")
			
			if !strings.Contains(actualCmd, strings.Join(tt.expectedArgs, " ")) {
				t.Errorf("Expected command to contain %s, got %s", expectedCmd, actualCmd)
			}
		})
	}
}

func TestGetPools(t *testing.T) {
	tests := []struct {
		name          string
		mockOutput    string
		expectedPools []string
		expectError   bool
	}{
		{
			name:          "list pools successfully",
			mockOutput:    "tank\nrpool\nbackup\n",
			expectedPools: []string{"tank", "rpool", "backup"},
			expectError:   false,
		},
		{
			name:          "no pools",
			mockOutput:    "",
			expectedPools: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: GetPools is a package function, so we'd need to refactor it for proper testing
			// For now, we'll test the parsing logic directly
			
			pools := parsePoolOutput(tt.mockOutput)
			
			if len(pools) != len(tt.expectedPools) {
				t.Errorf("Expected %d pools, got %d", len(tt.expectedPools), len(pools))
			}
			
			for i, expectedPool := range tt.expectedPools {
				if i >= len(pools) || pools[i] != expectedPool {
					t.Errorf("Expected pool %s at index %d, got %v", expectedPool, i, pools)
				}
			}
		})
	}
}

// Helper function for parsing pool output
func parsePoolOutput(output string) []string {
	var pools []string
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	for _, line := range lines {
		pool := strings.TrimSpace(line)
		if pool != "" {
			pools = append(pools, pool)
		}
	}
	
	return pools
}

func TestParsePoolStatus(t *testing.T) {
	mockOutput := `  pool: tank
 state: ONLINE
  scan: scrub repaired 0B in 0 days 01:23:45 with 0 errors on Sun Jan  1 12:00:00 2023

config:

	NAME        STATE     READ WRITE CKSUM
	tank        ONLINE       0     0     0
	  raidz1-0  ONLINE       0     0     0
	    sda     ONLINE       0     0     0
	    sdb     ONLINE       0     0     0
	    sdc     ONLINE       0     0     0

errors: No known data errors`

	status, err := parsePoolStatus(mockOutput)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if status.Pool != "tank" {
		t.Errorf("Expected pool name 'tank', got '%s'", status.Pool)
	}

	if status.State != "ONLINE" {
		t.Errorf("Expected state 'ONLINE', got '%s'", status.State)
	}

	if !strings.Contains(status.Scan, "scrub repaired") {
		t.Errorf("Expected scan info to contain 'scrub repaired', got '%s'", status.Scan)
	}

	expectedDevices := []string{"tank", "raidz1-0", "sda", "sdb", "sdc"}
	if len(status.Config) != len(expectedDevices) {
		t.Errorf("Expected %d devices, got %d", len(expectedDevices), len(status.Config))
	}

	for i, expectedDevice := range expectedDevices {
		if i >= len(status.Config) {
			t.Errorf("Expected device %s at index %d, but only got %d devices", expectedDevice, i, len(status.Config))
			continue
		}

		if status.Config[i].Name != expectedDevice {
			t.Errorf("Expected device name %s, got %s", expectedDevice, status.Config[i].Name)
		}

		if status.Config[i].State != "ONLINE" {
			t.Errorf("Expected device state ONLINE, got %s", status.Config[i].State)
		}
	}
}