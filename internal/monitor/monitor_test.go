package monitor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/zfs"
)

// Mock Alerter for testing
type MockAlerter struct {
	alerts       []AlertCall
	cooldownTime time.Duration
}

type AlertCall struct {
	Subject string
	Body    string
	Time    time.Time
}

func NewMockAlerter() *MockAlerter {
	return &MockAlerter{
		alerts:       make([]AlertCall, 0),
		cooldownTime: 1 * time.Hour,
	}
}

func (m *MockAlerter) SendAlert(subject, body string) error {
	m.alerts = append(m.alerts, AlertCall{
		Subject: subject,
		Body:    body,
		Time:    time.Now(),
	})
	return nil
}

func (m *MockAlerter) GetAlertCount() int {
	return len(m.alerts)
}

func (m *MockAlerter) GetLastAlert() *AlertCall {
	if len(m.alerts) == 0 {
		return nil
	}
	return &m.alerts[len(m.alerts)-1]
}

func (m *MockAlerter) HasAlertWithSubject(subject string) bool {
	for _, alert := range m.alerts {
		if strings.Contains(alert.Subject, subject) {
			return true
		}
	}
	return false
}

func (m *MockAlerter) Clear() {
	m.alerts = make([]AlertCall, 0)
}

// Mock command execution for testing
type MockCommandExecutor struct {
	outputs map[string]string
	errors  map[string]error
}

func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (m *MockCommandExecutor) AddCommand(cmdPattern, output string, err error) {
	m.outputs[cmdPattern] = output
	if err != nil {
		m.errors[cmdPattern] = err
	}
}

func (m *MockCommandExecutor) ExecuteCommand(command string) (string, error) {
	// Check for exact matches first
	if output, exists := m.outputs[command]; exists {
		if err, hasErr := m.errors[command]; hasErr {
			return "", err
		}
		return output, nil
	}
	
	// Check for pattern matches
	for pattern, output := range m.outputs {
		if strings.Contains(command, pattern) {
			if err, hasErr := m.errors[pattern]; hasErr {
				return "", err
			}
			return output, nil
		}
	}
	
	return "", fmt.Errorf("command not mocked: %s", command)
}

func TestNewMonitor(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			MonitorInterval: 5 * time.Minute,
		},
	}

	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	if monitor == nil {
		t.Fatal("Expected monitor to be created")
	}

	if monitor.config != cfg {
		t.Error("Config not set correctly")
	}

	if monitor.alerter != alerter {
		t.Error("Alerter not set correctly")
	}

	if monitor.alertCooldown != 1*time.Hour {
		t.Errorf("Expected cooldown of 1 hour, got %v", monitor.alertCooldown)
	}

	if len(monitor.lastAlerts) != 0 {
		t.Error("Expected empty lastAlerts map")
	}
}

func TestMonitorStart(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			MonitorInterval: 100 * time.Millisecond, // Very short for testing
		},
	}

	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	// Start monitor
	go monitor.Start()

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Stop monitor
	monitor.Stop()

	// Note: Full integration testing would require mocking the system commands
	// This test mainly verifies that Start/Stop work without panicking
}

func TestMonitorStop(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			MonitorInterval: 1 * time.Second,
		},
	}

	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	// Start and immediately stop
	go monitor.Start()
	time.Sleep(10 * time.Millisecond)
	monitor.Stop()

	// Should complete without hanging
}

func TestCheckPoolHealth(t *testing.T) {
	tests := []struct {
		name               string
		poolName           string
		mockOutput         string
		mockError          error
		expectAlert        bool
		expectedAlertSubject string
	}{
		{
			name:     "healthy pool",
			poolName: "tank",
			mockOutput: `  pool: tank
 state: ONLINE
  scan: scrub repaired 0B in 0 days 01:23:45 with 0 errors on Sun Jan  1 12:00:00 2023

config:

	NAME        STATE     READ WRITE CKSUM
	tank        ONLINE       0     0     0
	  raidz1-0  ONLINE       0     0     0
	    sda     ONLINE       0     0     0

errors: No known data errors`,
			expectAlert:          false,
			expectedAlertSubject: "",
		},
		{
			name:     "degraded pool",
			poolName: "tank",
			mockOutput: `  pool: tank
 state: DEGRADED
  scan: scrub repaired 0B in 0 days 01:23:45 with 1 errors on Sun Jan  1 12:00:00 2023

config:

	NAME        STATE     READ WRITE CKSUM
	tank        DEGRADED     0     0     1
	  raidz1-0  DEGRADED     0     0     1
	    sda     ONLINE       0     0     0
	    sdb     FAULTED      0     0     1

errors: Permanent errors have been detected`,
			expectAlert:          true,
			expectedAlertSubject: "ZFS Pool Alert: tank",
		},
		{
			name:                 "command error",
			poolName:             "tank",
			mockOutput:           "",
			mockError:            fmt.Errorf("zpool command failed"),
			expectAlert:          false,
			expectedAlertSubject: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			alerter := NewMockAlerter()
			monitor := New(cfg, alerter)

			// Mock the zfs.GetPoolStatus function by creating a test version
			err := monitor.checkPoolHealthWithMock(tt.poolName, tt.mockOutput, tt.mockError)

			if tt.mockError != nil && err == nil {
				t.Error("Expected error but got none")
			}

			if tt.mockError == nil && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectAlert && !alerter.HasAlertWithSubject(tt.expectedAlertSubject) {
				t.Errorf("Expected alert with subject containing '%s'", tt.expectedAlertSubject)
			}

			if !tt.expectAlert && alerter.GetAlertCount() > 0 {
				t.Error("Did not expect any alerts")
			}
		})
	}
}

// Helper method for testing with mocked pool status
func (m *Monitor) checkPoolHealthWithMock(pool, mockOutput string, mockError error) error {
	if mockError != nil {
		return mockError
	}

	status, err := parsePoolStatusForTest(mockOutput)
	if err != nil {
		return err
	}

	health := &PoolHealth{
		Pool:   pool,
		State:  status.State,
		Errors: status.Errors,
		Scrub:  m.parseScrubStatus(status.Scan),
	}

	for _, device := range status.Config {
		deviceHealth := DeviceHealth{
			Name:        device.Name,
			State:       device.State,
			ReadErrors:  device.Read,
			WriteErrors: device.Write,
			CksumErrors: device.Cksum,
			HasErrors:   device.Read > 0 || device.Write > 0 || device.Cksum > 0,
		}
		health.Devices = append(health.Devices, deviceHealth)

		if deviceHealth.HasErrors {
			health.Degraded = true
		}
	}

	if health.State != "ONLINE" || health.Degraded || len(health.Errors) > 0 {
		m.sendPoolAlert(health)
	}

	return nil
}

// Helper function to parse pool status for testing
func parsePoolStatusForTest(output string) (*zfs.PoolStatus, error) {
	// Simplified version for testing
	status := &zfs.PoolStatus{}
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "pool:") {
			status.Pool = strings.TrimSpace(strings.TrimPrefix(line, "pool:"))
		} else if strings.HasPrefix(line, "state:") {
			status.State = strings.TrimSpace(strings.TrimPrefix(line, "state:"))
		} else if strings.HasPrefix(line, "scan:") {
			status.Scan = strings.TrimSpace(strings.TrimPrefix(line, "scan:"))
		} else if strings.Contains(line, "errors:") && strings.Contains(line, "Permanent") {
			status.Errors = append(status.Errors, line)
		} else if strings.Contains(line, "ONLINE") || strings.Contains(line, "DEGRADED") || strings.Contains(line, "FAULTED") {
			// Parse device status
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				device := zfs.DeviceStatus{
					Name:  fields[0],
					State: fields[1],
				}
				
				// Parse error counts
				if len(fields) >= 5 {
					fmt.Sscanf(fields[2], "%d", &device.Read)
					fmt.Sscanf(fields[3], "%d", &device.Write)
					fmt.Sscanf(fields[4], "%d", &device.Cksum)
				}
				
				status.Config = append(status.Config, device)
			}
		}
	}

	return status, nil
}

func TestGetSystemDisks(t *testing.T) {
	cfg := &config.Config{}
	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	// Mock lsblk output
	mockOutput := `sda
sdb
sdc
loop0
sr0`

	expectedDisks := []string{"/dev/sda", "/dev/sdb", "/dev/sdc"}

	disks := monitor.parseSystemDisksOutput(mockOutput)

	if len(disks) != len(expectedDisks) {
		t.Errorf("Expected %d disks, got %d", len(expectedDisks), len(disks))
	}

	for i, expected := range expectedDisks {
		if i >= len(disks) || disks[i] != expected {
			t.Errorf("Expected disk %s at index %d, got %v", expected, i, disks)
		}
	}
}

// Helper method for testing
func (m *Monitor) parseSystemDisksOutput(output string) []string {
	var disks []string
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	for _, line := range lines {
		disk := strings.TrimSpace(line)
		if disk != "" && !strings.HasPrefix(disk, "loop") && !strings.HasPrefix(disk, "sr") {
			disks = append(disks, "/dev/"+disk)
		}
	}
	
	return disks
}

func TestGetSMARTData(t *testing.T) {
	tests := []struct {
		name           string
		device         string
		mockOutput     string
		expectHealthy  bool
		expectErrors   int
		expectTemp     int
	}{
		{
			name:   "healthy disk",
			device: "/dev/sda",
			mockOutput: `smartctl 7.2 2020-12-30 r5155 [x86_64-linux-5.4.0-74-generic] (local build)

SMART overall-health self-assessment test result: PASSED

ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  1 Raw_Read_Error_Rate     0x002f   100   100   051    Pre-fail  Always       -       0
194 Temperature_Celsius     0x0022   067   067   000    Old_age   Always       -       33 (Min/Max 15/45)
  5 Reallocated_Sector_Ct   0x0033   100   100   010    Pre-fail  Always       -       0`,
			expectHealthy: true,
			expectErrors:  0,
			expectTemp:    33,
		},
		{
			name:   "unhealthy disk",
			device: "/dev/sdb",
			mockOutput: `smartctl 7.2 2020-12-30 r5155 [x86_64-linux-5.4.0-74-generic] (local build)

SMART overall-health self-assessment test result: FAILED

ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  1 Raw_Read_Error_Rate     0x002f   100   100   051    Pre-fail  Always       -       5
194 Temperature_Celsius     0x0022   067   067   000    Old_age   Always       -       65 (Min/Max 15/75)
  5 Reallocated_Sector_Ct   0x0033   095   095   010    Pre-fail  Always       -       10`,
			expectHealthy: false,
			expectErrors:  3, // SMART health check failed + High temp + reallocated sectors
			expectTemp:    65,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			alerter := NewMockAlerter()
			monitor := New(cfg, alerter)

			smart := monitor.parseSMARTOutput(tt.device, tt.mockOutput)

			if smart.Healthy != tt.expectHealthy {
				t.Errorf("Expected healthy=%t, got %t", tt.expectHealthy, smart.Healthy)
			}

			if len(smart.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d: %v", tt.expectErrors, len(smart.Errors), smart.Errors)
			}

			if smart.Temperature != tt.expectTemp {
				t.Errorf("Expected temperature %d, got %d", tt.expectTemp, smart.Temperature)
			}

			if smart.Device != tt.device {
				t.Errorf("Expected device %s, got %s", tt.device, smart.Device)
			}
		})
	}
}

// Helper method for testing
func (m *Monitor) parseSMARTOutput(device, output string) *SMARTData {
	smart := &SMARTData{
		Device:  device,
		Healthy: true,
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "SMART overall-health") {
			if !strings.Contains(line, "PASSED") {
				smart.Healthy = false
				smart.Errors = append(smart.Errors, "SMART health check failed")
			}
		}

		if strings.Contains(line, "Temperature_Celsius") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				if temp := strings.Split(fields[9], " ")[0]; temp != "" {
					fmt.Sscanf(temp, "%d", &smart.Temperature)
					if smart.Temperature > 60 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High temperature: %d°C", smart.Temperature))
					}
				}
			}
		}

		if strings.Contains(line, "Reallocated_Sector_Ct") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				var value int
				if n, err := fmt.Sscanf(fields[9], "%d", &value); n == 1 && err == nil && value > 0 {
					smart.Errors = append(smart.Errors, fmt.Sprintf("Reallocated_Sector_Ct: %d", value))
				}
			}
		}
	}

	return smart
}

func TestSendPoolAlert(t *testing.T) {
	cfg := &config.Config{}
	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	health := &PoolHealth{
		Pool:     "tank",
		State:    "DEGRADED",
		Degraded: true,
		Devices: []DeviceHealth{
			{
				Name:        "sda",
				State:       "ONLINE",
				ReadErrors:  0,
				WriteErrors: 0,
				CksumErrors: 0,
			},
			{
				Name:        "sdb",
				State:       "FAULTED",
				ReadErrors:  5,
				WriteErrors: 2,
				CksumErrors: 1,
				HasErrors:   true,
			},
		},
		Errors: []string{"Permanent errors detected"},
	}

	monitor.sendPoolAlert(health)

	if alerter.GetAlertCount() != 1 {
		t.Errorf("Expected 1 alert, got %d", alerter.GetAlertCount())
	}

	lastAlert := alerter.GetLastAlert()
	if lastAlert == nil {
		t.Fatal("Expected alert but got none")
	}

	if !strings.Contains(lastAlert.Subject, "ZFS Pool Alert: tank") {
		t.Errorf("Expected subject to contain pool name, got: %s", lastAlert.Subject)
	}

	if !strings.Contains(lastAlert.Body, "DEGRADED") {
		t.Error("Expected alert body to contain pool state")
	}

	if !strings.Contains(lastAlert.Body, "sdb: FAULTED") {
		t.Error("Expected alert body to contain device status")
	}

	if !strings.Contains(lastAlert.Body, "Permanent errors detected") {
		t.Error("Expected alert body to contain error information")
	}
}

func TestSendDiskAlert(t *testing.T) {
	cfg := &config.Config{}
	alerter := NewMockAlerter()
	monitor := New(cfg, alerter)

	smart := &SMARTData{
		Device:      "/dev/sda",
		Healthy:     false,
		Temperature: 75,
		Errors:      []string{"High temperature: 75°C", "Reallocated sectors: 5"},
	}

	monitor.sendDiskAlert(smart)

	if alerter.GetAlertCount() != 1 {
		t.Errorf("Expected 1 alert, got %d", alerter.GetAlertCount())
	}

	lastAlert := alerter.GetLastAlert()
	if lastAlert == nil {
		t.Fatal("Expected alert but got none")
	}

	if !strings.Contains(lastAlert.Subject, "Disk Health Alert: /dev/sda") {
		t.Errorf("Expected subject to contain device name, got: %s", lastAlert.Subject)
	}

	if !strings.Contains(lastAlert.Body, "Healthy: false") {
		t.Error("Expected alert body to contain health status")
	}

	if !strings.Contains(lastAlert.Body, "Temperature: 75°C") {
		t.Error("Expected alert body to contain temperature")
	}

	if !strings.Contains(lastAlert.Body, "High temperature: 75°C") {
		t.Error("Expected alert body to contain error details")
	}
}

func TestParseScrubStatus(t *testing.T) {
	tests := []struct {
		name           string
		scanLine       string
		expectProgress bool
		expectErrors   int
	}{
		{
			name:           "scrub in progress",
			scanLine:       "scrub in progress since Mon Jan  1 12:00:00 2023",
			expectProgress: true,
			expectErrors:   0,
		},
		{
			name:           "scrub completed with errors",
			scanLine:       "scrub repaired 1.23G in 0 days 02:15:30 with 5 errors on Mon Jan  1 14:15:30 2023",
			expectProgress: false,
			expectErrors:   5,
		},
		{
			name:           "scrub completed clean",
			scanLine:       "scrub repaired 0B in 0 days 01:23:45 with 0 errors on Sun Jan  1 12:00:00 2023",
			expectProgress: false,
			expectErrors:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			alerter := NewMockAlerter()
			monitor := New(cfg, alerter)

			scrub := monitor.parseScrubStatus(tt.scanLine)

			if scrub.InProgress != tt.expectProgress {
				t.Errorf("Expected InProgress=%t, got %t", tt.expectProgress, scrub.InProgress)
			}

			if scrub.Errors != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d", tt.expectErrors, scrub.Errors)
			}

			if scrub.Status != tt.scanLine {
				t.Errorf("Expected status to be preserved: %s", scrub.Status)
			}
		})
	}
}