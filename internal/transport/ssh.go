package transport

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"zfsrabbit/internal/config"
	"zfsrabbit/internal/validation"
)

type SSHTransport struct {
	config *config.SSHConfig
	client *ssh.Client
}

func NewSSHTransport(cfg *config.SSHConfig) *SSHTransport {
	return &SSHTransport{
		config: cfg,
	}
}

func (t *SSHTransport) Connect() error {
	key, err := loadPrivateKey(t.config.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: t.config.RemoteUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // WARNING: Insecure - should implement proper host key verification in production
		Timeout:         30 * time.Second,
	}

	// Add port if not specified
	host := t.config.RemoteHost
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "22")
	}

	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return fmt.Errorf("failed to connect to remote host: %w", err)
	}

	t.client = client
	return nil
}

func (t *SSHTransport) Close() error {
	if t.client != nil {
		return t.client.Close()
	}
	return nil
}

func (t *SSHTransport) SendSnapshot(snapshotReader io.Reader, isIncremental bool) error {
	if t.client == nil {
		if err := t.Connect(); err != nil {
			return err
		}
	}

	session, err := t.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Sanitize dataset name to prevent command injection
	sanitizedDataset := validation.SanitizeCommand(t.config.RemoteDataset)
	sanitizedMbufferSize := validation.SanitizeCommand(t.config.MbufferSize)

	// Build command safely - BACKUP OPERATIONS: Use -F for automation (backup server should be clean)
	// This prioritizes automation over data safety on backup server (expected behavior)
	receiveCmd := fmt.Sprintf("mbuffer -s 128k -m %s | zfs receive -F %s",
		sanitizedMbufferSize, sanitizedDataset)

	session.Stdin = snapshotReader
	return session.Run(receiveCmd)
}

func (t *SSHTransport) ExecuteCommand(command string) (string, error) {
	if t.client == nil {
		if err := t.Connect(); err != nil {
			return "", err
		}
	}

	session, err := t.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	output, err := session.Output(command)
	if err != nil {
		return "", fmt.Errorf("command execution failed: %w", err)
	}

	return string(output), nil
}

func (t *SSHTransport) ListRemoteSnapshots() ([]string, error) {
	output, err := t.ExecuteCommand(fmt.Sprintf("zfs list -t snapshot -H -o name %s", t.config.RemoteDataset))
	if err != nil {
		return nil, err
	}

	var snapshots []string
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line != "" && strings.Contains(line, "@") {
			parts := strings.Split(line, "@")
			if len(parts) == 2 {
				snapshots = append(snapshots, parts[1])
			}
		}
	}

	return snapshots, nil
}

func (t *SSHTransport) ListAllRemoteDatasets() (map[string][]string, error) {
	// Get all datasets on remote server
	output, err := t.ExecuteCommand("zfs list -H -o name -t filesystem,volume")
	if err != nil {
		return nil, err
	}

	datasets := make(map[string][]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, dataset := range lines {
		dataset = strings.TrimSpace(dataset)
		if dataset == "" {
			continue
		}

		// Get snapshots for this dataset
		snapshots, err := t.GetSnapshotsForDataset(dataset)
		if err != nil {
			// Continue if we can't get snapshots for this dataset
			continue
		}

		if len(snapshots) > 0 {
			datasets[dataset] = snapshots
		}
	}

	return datasets, nil
}

func (t *SSHTransport) GetSnapshotsForDataset(dataset string) ([]string, error) {
	output, err := t.ExecuteCommand(fmt.Sprintf("zfs list -t snapshot -H -o name %s 2>/dev/null", dataset))
	if err != nil {
		return nil, err
	}

	var snapshots []string
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line != "" && strings.Contains(line, "@") {
			parts := strings.Split(line, "@")
			if len(parts) == 2 {
				snapshots = append(snapshots, parts[1])
			}
		}
	}

	return snapshots, nil
}

func (t *SSHTransport) GetRemoteDatasetInfo(dataset string) (*RemoteDatasetInfo, error) {
	output, err := t.ExecuteCommand(fmt.Sprintf("zfs list -H -o name,used,avail,refer,mountpoint %s", dataset))
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, fmt.Errorf("dataset %s not found", dataset)
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return nil, fmt.Errorf("invalid dataset info format")
	}

	snapshots, _ := t.GetSnapshotsForDataset(dataset)

	return &RemoteDatasetInfo{
		Name:       fields[0],
		Used:       fields[1],
		Available:  fields[2],
		Referenced: fields[3],
		Mountpoint: fields[4],
		Snapshots:  snapshots,
	}, nil
}

type RemoteDatasetInfo struct {
	Name       string   `json:"name"`
	Used       string   `json:"used"`
	Available  string   `json:"available"`
	Referenced string   `json:"referenced"`
	Mountpoint string   `json:"mountpoint"`
	Snapshots  []string `json:"snapshots"`
}

func (t *SSHTransport) RestoreSnapshot(snapshotName, localDataset string) error {
	return t.RestoreSnapshotFromDataset(t.config.RemoteDataset, snapshotName, localDataset)
}

func (t *SSHTransport) RestoreSnapshotSafe(snapshotName, localDataset string) error {
	return t.RestoreSnapshotFromDatasetSafe(t.config.RemoteDataset, snapshotName, localDataset)
}

func (t *SSHTransport) RestoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset string) error {
	return t.restoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset, true) // Force mode
}

func (t *SSHTransport) RestoreSnapshotFromDatasetSafe(remoteDataset, snapshotName, localDataset string) error {
	return t.restoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset, false) // Safe mode
}

func (t *SSHTransport) restoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset string, forceOverwrite bool) error {
	if t.client == nil {
		if err := t.Connect(); err != nil {
			return err
		}
	}

	session, err := t.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Determine send flags based on whether we need recursive send
	sendCmd := fmt.Sprintf("zfs send -R %s@%s", remoteDataset, snapshotName) // Always use -R for full dataset trees

	session.Stdout = &mbufferReceiver{
		dataset:        localDataset,
		size:           t.config.MbufferSize,
		forceOverwrite: forceOverwrite,
		remoteDataset:  remoteDataset, // Pass source dataset name for proper mapping
	}

	return session.Run(sendCmd)
}

type mbufferReceiver struct {
	dataset        string
	size           string
	forceOverwrite bool   // Use -F flag for destructive operations
	remoteDataset  string // Source dataset name for proper mapping
}

func (m *mbufferReceiver) Write(p []byte) (n int, err error) {
	// Sanitize inputs to prevent command injection
	sanitizedSize := validation.SanitizeCommand(m.size)
	sanitizedDataset := validation.SanitizeCommand(m.dataset)

	// Build command safely - choose safe vs. destructive mode with proper dataset mapping
	var safeCommand string

	// Use -d flag to strip first element of path (avoids nesting issues)
	// Example: remote "data1/helix-backup" -> local "data" (strips "data1")
	receiveFlags := "-d"
	if m.forceOverwrite {
		receiveFlags += " -F" // Add force flag for destructive operations
	}

	safeCommand = fmt.Sprintf("mbuffer -s 128k -m %s | zfs receive %s %s",
		sanitizedSize, receiveFlags, sanitizedDataset)

	cmd := exec.Command("sh", "-c", safeCommand)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	n, err = stdin.Write(p)
	if err != nil {
		stdin.Close()
		cmd.Wait()
		return n, err
	}

	stdin.Close()
	return n, cmd.Wait()
}

func loadPrivateKey(keyPath string) (ssh.Signer, error) {
	// Enhanced path validation to prevent path traversal attacks
	if keyPath == "" {
		return nil, fmt.Errorf("private key path cannot be empty")
	}

	// Clean the path to remove any . or .. elements
	cleanedPath := filepath.Clean(keyPath)

	// Ensure path is absolute to prevent relative path attacks
	if !filepath.IsAbs(cleanedPath) {
		return nil, fmt.Errorf("private key path must be absolute, got: %s", keyPath)
	}

	// Additional safety check - still block obvious traversal attempts
	if strings.Contains(cleanedPath, "..") {
		return nil, fmt.Errorf("invalid key path contains traversal: %s", keyPath)
	}

	key, err := os.ReadFile(cleanedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file %s: %w", cleanedPath, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return signer, nil
}
