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

type Monitor struct {
	config       *config.Config
	alerter      Alerter
	ctx          context.Context
	cancel       context.CancelFunc
	lastAlerts   map[string]time.Time
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
}

func New(cfg *config.Config, alerter Alerter) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Monitor{
		config:        cfg,
		alerter:       alerter,
		ctx:           ctx,
		cancel:        cancel,
		lastAlerts:    make(map[string]time.Time),
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
	cmd := exec.Command("smartctl", "-H", "-A", device)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	smart := &SMARTData{
		Device:  device,
		Healthy: true,
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

func (m *Monitor) sendPoolAlert(health *PoolHealth) {
	alertKey := fmt.Sprintf("pool_%s", health.Pool)
	if time.Since(m.lastAlerts[alertKey]) < m.alertCooldown {
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
		m.lastAlerts[alertKey] = time.Now()
		log.Printf("Sent pool health alert for %s", health.Pool)
	}
}

func (m *Monitor) sendDiskAlert(smart *SMARTData) {
	alertKey := fmt.Sprintf("disk_%s", smart.Device)
	if time.Since(m.lastAlerts[alertKey]) < m.alertCooldown {
		return
	}
	
	subject := fmt.Sprintf("Disk Health Alert: %s", smart.Device)
	body := fmt.Sprintf(`Disk Health Alert

Device: %s
Healthy: %v
Temperature: %d°C

`, smart.Device, smart.Healthy, smart.Temperature)
	
	if len(smart.Errors) > 0 {
		body += "Errors:\n"
		for _, err := range smart.Errors {
			body += fmt.Sprintf("  %s\n", err)
		}
	}
	
	if err := m.alerter.SendAlert(subject, body); err != nil {
		log.Printf("Failed to send disk alert: %v", err)
	} else {
		m.lastAlerts[alertKey] = time.Now()
		log.Printf("Sent disk health alert for %s", smart.Device)
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