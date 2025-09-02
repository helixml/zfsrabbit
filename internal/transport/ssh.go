package transport

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
	"zfsrabbit/internal/config"
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", t.config.RemoteHost+":22", config)
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

	var receiveCmd string
	if isIncremental {
		receiveCmd = fmt.Sprintf("mbuffer -s %s -m %s | zfs receive -F %s",
			"128k", t.config.MbufferSize, t.config.RemoteDataset)
	} else {
		receiveCmd = fmt.Sprintf("mbuffer -s %s -m %s | zfs receive -F %s",
			"128k", t.config.MbufferSize, t.config.RemoteDataset)
	}

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

func (t *SSHTransport) RestoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset string) error {
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

	sendCmd := fmt.Sprintf("zfs send %s@%s", remoteDataset, snapshotName)

	session.Stdout = &mbufferReceiver{
		dataset: localDataset,
		size:    t.config.MbufferSize,
	}

	return session.Run(sendCmd)
}

type mbufferReceiver struct {
	dataset string
	size    string
}

func (m *mbufferReceiver) Write(p []byte) (n int, err error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("mbuffer -s 128k -m %s | zfs receive -F %s", m.size, m.dataset))

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
	key, err := exec.Command("cat", keyPath).Output()
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}

	return signer, nil
}
