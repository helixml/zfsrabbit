package monitor

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/zfs"
)

type AlertSeverity int

const (
	SeverityInfo AlertSeverity = iota
	SeverityWarning
	SeverityCritical
	SeverityEmergency
)

func (s AlertSeverity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityWarning:
		return "WARNING"
	case SeverityCritical:
		return "CRITICAL"
	case SeverityEmergency:
		return "EMERGENCY"
	default:
		return "UNKNOWN"
	}
}

type AlertState struct {
	LastAlertTime     time.Time
	LastSeverity      AlertSeverity
	LastTemperature   int
	LastCriticalWarning int
}

type Monitor struct {
	config       *config.Config
	alerter      Alerter
	ctx          context.Context
	cancel       context.CancelFunc
	alertStates  map[string]*AlertState // Per-device alert state
	alertCooldown time.Duration
}

type Alerter interface {
	SendAlert(subject, body string) error
}

type ExtendedAlerter interface {
	SendAlert(subject, body string) error
	SendSystemStatus(status map[string]interface{}) error
}

type PoolHealth struct {
	Pool     string
	State    string
	Errors   []string
	Devices  []DeviceHealth
	Degraded bool
	Scrub    ScrubStatus
}

type DeviceHealth struct {
	Name        string
	State       string
	ReadErrors  int
	WriteErrors int
	CksumErrors int
	Temperature int
	HasErrors   bool
}

type ScrubStatus struct {
	InProgress bool
	LastRun    time.Time
	Errors     int
	Status     string
}

type SMARTData struct {
	Device      string
	Healthy     bool
	Temperature int
	Errors      []string
	// NVMe-specific fields
	IsNVMe           bool
	CriticalWarning  int    // NVMe critical warning bits
	PercentageUsed   int    // Wear level (0-100%+)
	AvailableSpare   int    // Spare capacity remaining
	MaxTemperature   int    // Lifetime max temperature
	DataUnitsWritten uint64 // Total data written (for wear tracking)
}

func New(cfg *config.Config, alerter Alerter) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Monitor{
		config:        cfg,
		alerter:       alerter,
		ctx:           ctx,
		cancel:        cancel,
		alertStates:   make(map[string]*AlertState),
		alertCooldown: 1 * time.Hour,
	}
}

func (m *Monitor) Start() {
	log.Println("Starting system monitor")
	
	ticker := time.NewTicker(m.config.Schedule.MonitorInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			log.Println("System monitor stopped")
			return
		case <-ticker.C:
			m.checkSystemHealth()
		}
	}
}

func (m *Monitor) Stop() {
	m.cancel()
}

func (m *Monitor) checkSystemHealth() {
	pools, err := zfs.GetPools()
	if err != nil {
		log.Printf("Failed to get ZFS pools: %v", err)
		return
	}
	
	for _, pool := range pools {
		if err := m.checkPoolHealth(pool); err != nil {
			log.Printf("Failed to check health of pool %s: %v", pool, err)
		}
	}
	
	if err := m.checkDiskHealth(); err != nil {
		log.Printf("Failed to check disk health: %v", err)
	}
}

func (m *Monitor) checkPoolHealth(pool string) error {
	status, err := zfs.GetPoolStatus(pool)
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

func (m *Monitor) parseScrubStatus(scanLine string) ScrubStatus {
	scrub := ScrubStatus{}
	
	if strings.Contains(scanLine, "in progress") {
		scrub.InProgress = true
	}
	
	// Handle completed scrub with error count
	re := regexp.MustCompile(`with (\d+) errors on (.+?)$`)
	matches := re.FindStringSubmatch(scanLine)
	if len(matches) >= 3 {
		if errors, err := strconv.Atoi(matches[1]); err == nil {
			scrub.Errors = errors
		}
		if t, err := time.Parse("Mon Jan 2 15:04:05 2006", matches[2]); err == nil {
			scrub.LastRun = t
		}
	}
	
	scrub.Status = scanLine
	return scrub
}

func (m *Monitor) checkDiskHealth() error {
	disks, err := m.getSystemDisks()
	if err != nil {
		return err
	}
	
	for _, disk := range disks {
		smart, err := m.getSMARTData(disk)
		if err != nil {
			log.Printf("Failed to get SMART data for %s: %v", disk, err)
			continue
		}
		
		if !smart.Healthy || len(smart.Errors) > 0 {
			m.sendDiskAlert(smart)
		}
	}
	
	return nil
}

func (m *Monitor) getSystemDisks() ([]string, error) {
	cmd := exec.Command("lsblk", "-d", "-n", "-o", "NAME")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	var disks []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		disk := strings.TrimSpace(line)
		if disk != "" && !strings.HasPrefix(disk, "loop") && !strings.HasPrefix(disk, "sr") {
			disks = append(disks, "/dev/"+disk)
		}
	}
	
	return disks, nil
}

func (m *Monitor) getSMARTData(device string) (*SMARTData, error) {
	smart := &SMARTData{
		Device:  device,
		Healthy: true,
	}
	
	// Check if this is an NVMe device
	if strings.Contains(device, "nvme") {
		return m.getNVMeSMARTData(device, smart)
	}
	
	// Traditional SMART data for HDDs/SATA SSDs
	cmd := exec.Command("smartctl", "-H", "-A", device)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	lines := strings.Split(string(output), "\n")
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
				if temp, err := strconv.Atoi(fields[9]); err == nil {
					smart.Temperature = temp
					if temp > 60 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High temperature: %d°C", temp))
					}
				}
			}
		}
		
		if strings.Contains(line, "Reallocated_Sector_Ct") || 
		   strings.Contains(line, "Current_Pending_Sector") ||
		   strings.Contains(line, "Offline_Uncorrectable") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				if value, err := strconv.Atoi(fields[9]); err == nil && value > 0 {
					smart.Errors = append(smart.Errors, fmt.Sprintf("%s: %d", fields[1], value))
				}
			}
		}
	}
	
	return smart, nil
}

func (m *Monitor) getNVMeSMARTData(device string, smart *SMARTData) (*SMARTData, error) {
	smart.IsNVMe = true
	
	// Try nvme-cli first for better NVMe support
	if err := m.parseNVMeCLI(device, smart); err == nil {
		return smart, nil
	}
	
	// Fallback to smartctl for NVMe
	return m.parseSmartctlNVMe(device, smart)
}

func (m *Monitor) parseNVMeCLI(device string, smart *SMARTData) error {
	cmd := exec.Command("nvme", "smart-log", device)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Parse critical warning
		if strings.Contains(line, "critical_warning") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if warning, err := strconv.Atoi(fields[2]); err == nil {
					smart.CriticalWarning = warning
					if warning > 0 {
						smart.Healthy = false
						smart.Errors = append(smart.Errors, fmt.Sprintf("Critical warning: 0x%x", warning))
					}
				}
			}
		}
		
		// Parse temperature
		if strings.Contains(line, "temperature") && strings.Contains(line, "Celsius") {
			fields := strings.Fields(line)
			for _, field := range fields {
				if temp, err := strconv.Atoi(field); err == nil && temp > 0 && temp < 200 {
					smart.Temperature = temp
					if temp > 60 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High temperature: %d°C", temp))
					}
					break
				}
			}
		}
		
		// Parse percentage used (wear level)
		if strings.Contains(line, "percentage_used") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if used, err := strconv.Atoi(strings.TrimSuffix(fields[2], "%")); err == nil {
					smart.PercentageUsed = used
					if used > 90 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High wear level: %d%%", used))
					}
				}
			}
		}
		
		// Parse available spare
		if strings.Contains(line, "available_spare") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if spare, err := strconv.Atoi(strings.TrimSuffix(fields[2], "%")); err == nil {
					smart.AvailableSpare = spare
					if spare < 10 {
						smart.Healthy = false
						smart.Errors = append(smart.Errors, fmt.Sprintf("Low spare capacity: %d%%", spare))
					}
				}
			}
		}
		
		// Parse data units written
		if strings.Contains(line, "data_units_written") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if written, err := strconv.ParseUint(strings.Replace(fields[2], ",", "", -1), 10, 64); err == nil {
					smart.DataUnitsWritten = written
				}
			}
		}
	}
	
	return nil
}

func (m *Monitor) parseSmartctlNVMe(device string, smart *SMARTData) (*SMARTData, error) {
	cmd := exec.Command("smartctl", "-H", "-A", device)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		if strings.Contains(line, "SMART overall-health") {
			if !strings.Contains(line, "PASSED") {
				smart.Healthy = false
				smart.Errors = append(smart.Errors, "SMART health check failed")
			}
		}
		
		// Parse NVMe-specific attributes from smartctl
		if strings.Contains(line, "Critical Warning:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if warning, err := strconv.Atoi(strings.TrimPrefix(fields[2], "0x")); err == nil {
					smart.CriticalWarning = warning
					if warning > 0 {
						smart.Healthy = false
						smart.Errors = append(smart.Errors, fmt.Sprintf("Critical warning: 0x%x", warning))
					}
				}
			}
		}
		
		if strings.Contains(line, "Temperature:") {
			fields := strings.Fields(line)
			for _, field := range fields {
				if temp, err := strconv.Atoi(field); err == nil && temp > 0 && temp < 200 {
					smart.Temperature = temp
					if temp > 60 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High temperature: %d°C", temp))
					}
					break
				}
			}
		}
		
		if strings.Contains(line, "Percentage Used:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if used, err := strconv.Atoi(strings.TrimSuffix(fields[2], "%")); err == nil {
					smart.PercentageUsed = used
					if used > 90 {
						smart.Errors = append(smart.Errors, fmt.Sprintf("High wear level: %d%%", used))
					}
				}
			}
		}
		
		if strings.Contains(line, "Available Spare:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if spare, err := strconv.Atoi(strings.TrimSuffix(fields[2], "%")); err == nil {
					smart.AvailableSpare = spare
					if spare < 10 {
						smart.Healthy = false
						smart.Errors = append(smart.Errors, fmt.Sprintf("Low spare capacity: %d%%", spare))
					}
				}
			}
		}
	}
	
	return smart, nil
}

func (m *Monitor) sendPoolAlert(health *PoolHealth) {
	alertKey := fmt.Sprintf("pool_%s", health.Pool)
	currentState, exists := m.alertStates[alertKey]
	
	if exists && time.Since(currentState.LastAlertTime) < m.alertCooldown {
		return
	}
	
	subject := fmt.Sprintf("ZFS Pool Alert: %s", health.Pool)
	body := fmt.Sprintf(`ZFS Pool Health Alert

Pool: %s
State: %s
Degraded: %v

Device Status:
`, health.Pool, health.State, health.Degraded)
	
	for _, device := range health.Devices {
		body += fmt.Sprintf("  %s: %s (R:%d W:%d C:%d)\n", 
			device.Name, device.State, device.ReadErrors, device.WriteErrors, device.CksumErrors)
	}
	
	if len(health.Errors) > 0 {
		body += "\nErrors:\n"
		for _, err := range health.Errors {
			body += fmt.Sprintf("  %s\n", err)
		}
	}
	
	if health.Scrub.Errors > 0 {
		body += fmt.Sprintf("\nScrub Errors: %d\n", health.Scrub.Errors)
	}
	
	if err := m.alerter.SendAlert(subject, body); err != nil {
		log.Printf("Failed to send pool alert: %v", err)
	} else {
		// Update or create alert state for this pool
		if !exists {
			m.alertStates[alertKey] = &AlertState{
				LastAlertTime: time.Now(),
			}
		} else {
			currentState.LastAlertTime = time.Now()
		}
		log.Printf("Sent pool health alert for %s", health.Pool)
	}
}

func (m *Monitor) getTemperatureSeverity(temperature int, isNVMe bool) AlertSeverity {
	if isNVMe {
		// NVMe drives have different temperature tolerances
		switch {
		case temperature >= 80:
			return SeverityEmergency // Immediate action required
		case temperature >= 70:
			return SeverityCritical // Very concerning
		case temperature >= 60:
			return SeverityWarning // Watch closely
		default:
			return SeverityInfo
		}
	} else {
		// Traditional HDD/SATA SSD thresholds
		switch {
		case temperature >= 70:
			return SeverityEmergency
		case temperature >= 60:
			return SeverityCritical
		case temperature >= 50:
			return SeverityWarning
		default:
			return SeverityInfo
		}
	}
}

func (m *Monitor) getCriticalWarningSeverity(warning int) AlertSeverity {
	maxSeverity := SeverityInfo
	
	if warning&8 != 0 { // Media read-only
		maxSeverity = SeverityEmergency
	} else if warning&4 != 0 { // Reliability degraded
		if SeverityCritical > maxSeverity {
			maxSeverity = SeverityCritical
		}
	}
	
	if warning&1 != 0 { // Spare capacity low
		if SeverityCritical > maxSeverity {
			maxSeverity = SeverityCritical
		}
	}
	
	if warning&2 != 0 { // Temperature threshold
		if SeverityWarning > maxSeverity {
			maxSeverity = SeverityWarning
		}
	}
	
	return maxSeverity
}

func (m *Monitor) getOverallSeverity(smart *SMARTData) AlertSeverity {
	maxSeverity := SeverityInfo
	
	// Check temperature severity
	tempSeverity := m.getTemperatureSeverity(smart.Temperature, smart.IsNVMe)
	if tempSeverity > maxSeverity {
		maxSeverity = tempSeverity
	}
	
	// Check NVMe critical warning severity
	if smart.IsNVMe && smart.CriticalWarning > 0 {
		warningSeverity := m.getCriticalWarningSeverity(smart.CriticalWarning)
		if warningSeverity > maxSeverity {
			maxSeverity = warningSeverity
		}
	}
	
	// Check wear level for NVMe
	if smart.IsNVMe {
		switch {
		case smart.PercentageUsed >= 100:
			if SeverityEmergency > maxSeverity {
				maxSeverity = SeverityEmergency
			}
		case smart.PercentageUsed >= 95:
			if SeverityCritical > maxSeverity {
				maxSeverity = SeverityCritical
			}
		case smart.PercentageUsed >= 90:
			if SeverityWarning > maxSeverity {
				maxSeverity = SeverityWarning
			}
		}
	}
	
	// Check available spare for NVMe
	if smart.IsNVMe {
		switch {
		case smart.AvailableSpare <= 5:
			if SeverityEmergency > maxSeverity {
				maxSeverity = SeverityEmergency
			}
		case smart.AvailableSpare <= 10:
			if SeverityCritical > maxSeverity {
				maxSeverity = SeverityCritical
			}
		case smart.AvailableSpare <= 20:
			if SeverityWarning > maxSeverity {
				maxSeverity = SeverityWarning
			}
		}
	}
	
	return maxSeverity
}

func (m *Monitor) shouldSendAlert(smart *SMARTData, severity AlertSeverity) bool {
	alertKey := smart.Device
	currentState, exists := m.alertStates[alertKey]
	
	if !exists {
		// First alert for this device
		m.alertStates[alertKey] = &AlertState{
			LastAlertTime:       time.Now(),
			LastSeverity:        severity,
			LastTemperature:     smart.Temperature,
			LastCriticalWarning: smart.CriticalWarning,
		}
		return severity > SeverityInfo
	}
	
	// Check for escalation conditions
	escalated := false
	
	// Severity increased
	if severity > currentState.LastSeverity {
		escalated = true
	}
	
	// Significant temperature increase (>10°C)
	if smart.Temperature > currentState.LastTemperature+10 {
		escalated = true
	}
	
	// New critical warning bits
	if smart.IsNVMe {
		newWarnings := smart.CriticalWarning & ^currentState.LastCriticalWarning
		if newWarnings > 0 {
			escalated = true
		}
	}
	
	// If escalated, bypass cooldown
	if escalated {
		currentState.LastAlertTime = time.Now()
		currentState.LastSeverity = severity
		currentState.LastTemperature = smart.Temperature
		currentState.LastCriticalWarning = smart.CriticalWarning
		return true
	}
	
	// Check cooldown for same severity
	if time.Since(currentState.LastAlertTime) >= m.alertCooldown && severity > SeverityInfo {
		currentState.LastAlertTime = time.Now()
		currentState.LastSeverity = severity
		currentState.LastTemperature = smart.Temperature
		currentState.LastCriticalWarning = smart.CriticalWarning
		return true
	}
	
	return false
}

func (m *Monitor) sendDiskAlert(smart *SMARTData) {
	severity := m.getOverallSeverity(smart)
	
	if !m.shouldSendAlert(smart, severity) {
		return
	}
	
	deviceType := "Disk"
	if smart.IsNVMe {
		deviceType = "NVMe SSD"
	}
	
	// Include severity in subject
	subject := fmt.Sprintf("[%s] %s Health Alert: %s", severity.String(), deviceType, smart.Device)
	body := fmt.Sprintf(`%s Health Alert

Severity: %s
Device: %s
Healthy: %v
Temperature: %d°C
`, deviceType, severity.String(), smart.Device, smart.Healthy, smart.Temperature)

	// Add NVMe-specific information
	if smart.IsNVMe {
		body += fmt.Sprintf(`
NVMe Specific Data:
Critical Warning: 0x%x
Wear Level: %d%%
Available Spare: %d%%
Data Written: %d units
`, smart.CriticalWarning, smart.PercentageUsed, smart.AvailableSpare, smart.DataUnitsWritten)

		// Add critical warning explanations
		if smart.CriticalWarning > 0 {
			body += "\nCritical Warning Details:\n"
			if smart.CriticalWarning&1 != 0 {
				body += "  - Available spare capacity has fallen below threshold\n"
			}
			if smart.CriticalWarning&2 != 0 {
				body += "  - Temperature threshold exceeded\n"
			}
			if smart.CriticalWarning&4 != 0 {
				body += "  - NVM subsystem reliability degraded\n"
			}
			if smart.CriticalWarning&8 != 0 {
				body += "  - Media placed in read-only mode\n"
			}
			if smart.CriticalWarning&16 != 0 {
				body += "  - Persistent memory region backed up (if applicable)\n"
			}
		}
	}
	
	if len(smart.Errors) > 0 {
		body += "\nErrors:\n"
		for _, err := range smart.Errors {
			body += fmt.Sprintf("  %s\n", err)
		}
	}
	
	if err := m.alerter.SendAlert(subject, body); err != nil {
		log.Printf("Failed to send disk alert: %v", err)
	} else {
		log.Printf("Sent %s [%s] health alert for %s", deviceType, severity.String(), smart.Device)
	}
}

func (m *Monitor) GetSystemStatus() map[string]interface{} {
	status := make(map[string]interface{})
	
	pools, err := zfs.GetPools()
	if err == nil {
		poolStatus := make(map[string]interface{})
		for _, pool := range pools {
			if health, err := zfs.GetPoolStatus(pool); err == nil {
				poolStatus[pool] = health
			}
		}
		status["pools"] = poolStatus
	}
	
	disks, err := m.getSystemDisks()
	if err == nil {
		diskStatus := make(map[string]interface{})
		for _, disk := range disks {
			if smart, err := m.getSMARTData(disk); err == nil {
				diskStatus[disk] = smart
			}
		}
		status["disks"] = diskStatus
	}
	
	return status
}