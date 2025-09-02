package zfs

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Manager struct {
	dataset         string
	sendCompression string
	recursive       bool
	executor        CommandExecutor
}

type CommandExecutor interface {
	Command(name string, args ...string) *exec.Cmd
	Output(cmd *exec.Cmd) ([]byte, error)
	Run(cmd *exec.Cmd) error
}

type DefaultCommandExecutor struct{}

func (d *DefaultCommandExecutor) Command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func (d *DefaultCommandExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	return cmd.Output()
}

func (d *DefaultCommandExecutor) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

type Snapshot struct {
	Name     string
	Created  time.Time
	Used     string
	Refer    string
	Dataset  string
}

type PoolStatus struct {
	Pool   string
	State  string
	Scan   string
	Config []DeviceStatus
	Errors []string
}

type DeviceStatus struct {
	Name   string
	State  string
	Read   int
	Write  int
	Cksum  int
}

func New(dataset, sendCompression string, recursive bool) *Manager {
	return &Manager{
		dataset:         dataset,
		sendCompression: sendCompression,
		recursive:       recursive,
		executor:        &DefaultCommandExecutor{},
	}
}

func NewWithExecutor(dataset, sendCompression string, recursive bool, executor CommandExecutor) *Manager {
	return &Manager{
		dataset:         dataset,
		sendCompression: sendCompression,
		recursive:       recursive,
		executor:        executor,
	}
}

func (m *Manager) CreateSnapshot(name string) error {
	snapshotName := fmt.Sprintf("%s@%s", m.dataset, name)
	
	args := []string{"snapshot"}
	if m.recursive {
		args = append(args, "-r")
	}
	args = append(args, snapshotName)
	
	cmd := m.executor.Command("zfs", args...)
	return m.executor.Run(cmd)
}

func (m *Manager) ListSnapshots() ([]Snapshot, error) {
	cmd := m.executor.Command("zfs", "list", "-t", "snapshot", "-H", "-o", "name,creation,used,refer", "-s", "creation", m.dataset)
	output, err := m.executor.Output(cmd)
	if err != nil {
		return nil, err
	}
	
	var snapshots []Snapshot
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			parts := strings.Split(fields[0], "@")
			if len(parts) == 2 {
				created, _ := time.Parse("Mon Jan 2 15:04 2006", fields[1])
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
	
	return snapshots, scanner.Err()
}

func (m *Manager) DestroySnapshot(name string) error {
	snapshotName := fmt.Sprintf("%s@%s", m.dataset, name)
	cmd := m.executor.Command("zfs", "destroy", snapshotName)
	return m.executor.Run(cmd)
}

func (m *Manager) SendSnapshot(snapshot string) (*exec.Cmd, error) {
	snapshotName := fmt.Sprintf("%s@%s", m.dataset, snapshot)
	
	args := []string{"send"}
	if m.sendCompression != "" {
		args = append(args, "-c")
	}
	if m.recursive {
		args = append(args, "-R")
	}
	args = append(args, snapshotName)
	
	cmd := m.executor.Command("zfs", args...)
	return cmd, nil
}

func (m *Manager) SendIncremental(fromSnapshot, toSnapshot string) (*exec.Cmd, error) {
	fromName := fmt.Sprintf("%s@%s", m.dataset, fromSnapshot)
	toName := fmt.Sprintf("%s@%s", m.dataset, toSnapshot)
	
	args := []string{"send"}
	if m.sendCompression != "" {
		args = append(args, "-c")
	}
	if m.recursive {
		args = append(args, "-R")
	}
	args = append(args, "-i", fromName, toName)
	
	cmd := m.executor.Command("zfs", args...)
	return cmd, nil
}

func (m *Manager) ReceiveSnapshot(dataset string) (*exec.Cmd, error) {
	cmd := m.executor.Command("zfs", "receive", "-F", dataset)
	return cmd, nil
}

func GetPoolStatus(pool string) (*PoolStatus, error) {
	cmd := exec.Command("zpool", "status", pool)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	return parsePoolStatus(string(output))
}

func parsePoolStatus(output string) (*PoolStatus, error) {
	lines := strings.Split(output, "\n")
	status := &PoolStatus{}
	
	var inConfig bool
	var inErrors bool
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		if strings.HasPrefix(line, "pool:") {
			status.Pool = strings.TrimSpace(strings.TrimPrefix(line, "pool:"))
		} else if strings.HasPrefix(line, "state:") {
			status.State = strings.TrimSpace(strings.TrimPrefix(line, "state:"))
		} else if strings.HasPrefix(line, "scan:") {
			status.Scan = strings.TrimSpace(strings.TrimPrefix(line, "scan:"))
		} else if strings.HasPrefix(line, "config:") {
			inConfig = true
			inErrors = false
		} else if strings.HasPrefix(line, "errors:") {
			inErrors = true
			inConfig = false
		} else if inConfig && line != "" {
			if device := parseDeviceStatus(line); device != nil {
				status.Config = append(status.Config, *device)
			}
		} else if inErrors && line != "" {
			status.Errors = append(status.Errors, line)
		}
	}
	
	return status, nil
}

func parseDeviceStatus(line string) *DeviceStatus {
	re := regexp.MustCompile(`^\s*(\S+)\s+(\S+)\s+(\d+)\s+(\d+)\s+(\d+)`)
	matches := re.FindStringSubmatch(line)
	
	if len(matches) != 6 {
		return nil
	}
	
	return &DeviceStatus{
		Name:  matches[1],
		State: matches[2],
		Read:  parseInt(matches[3]),
		Write: parseInt(matches[4]),
		Cksum: parseInt(matches[5]),
	}
}

func parseInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}

func ScrubPool(pool string) error {
	cmd := exec.Command("zpool", "scrub", pool)
	return cmd.Run()
}

func GetPools() ([]string, error) {
	cmd := exec.Command("zpool", "list", "-H", "-o", "name")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	var pools []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	
	for scanner.Scan() {
		pool := strings.TrimSpace(scanner.Text())
		if pool != "" {
			pools = append(pools, pool)
		}
	}
	
	return pools, scanner.Err()
}