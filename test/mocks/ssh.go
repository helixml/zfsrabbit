package mocks

import (
	"fmt"
	"io"
	"zfsrabbit/internal/transport"
)

// MockRemoteDatasetInfo represents remote dataset information for testing
type MockRemoteDatasetInfo struct {
	Dataset   string   `json:"dataset"`
	Snapshots []string `json:"snapshots"`
	Size      string   `json:"size"`
	Used      string   `json:"used"`
	Available string   `json:"available"`
}

// MockSSHTransport mocks SSH transport functionality
type MockSSHTransport struct {
	ConnectError       error
	ExecuteCommands    map[string]string // command -> output
	ExecuteErrors      map[string]error  // command -> error
	RemoteSnapshots    []string
	RemoteDatasets     map[string][]string
	DatasetInfos       map[string]*transport.RemoteDatasetInfo
	SendSnapshotError  error
	RestoreError       error
	CallLog            []string
}

func NewMockSSHTransport() *MockSSHTransport {
	return &MockSSHTransport{
		ExecuteCommands: make(map[string]string),
		ExecuteErrors:   make(map[string]error),
		RemoteDatasets:  make(map[string][]string),
		DatasetInfos:    make(map[string]*transport.RemoteDatasetInfo),
		CallLog:         make([]string, 0),
	}
}

func (m *MockSSHTransport) Connect() error {
	m.CallLog = append(m.CallLog, "Connect")
	return m.ConnectError
}

func (m *MockSSHTransport) Close() error {
	m.CallLog = append(m.CallLog, "Close")
	return nil
}

func (m *MockSSHTransport) ExecuteCommand(command string) (string, error) {
	m.CallLog = append(m.CallLog, fmt.Sprintf("ExecuteCommand: %s", command))
	
	if err, exists := m.ExecuteErrors[command]; exists {
		return "", err
	}
	
	if output, exists := m.ExecuteCommands[command]; exists {
		return output, nil
	}
	
	return "", fmt.Errorf("command not mocked: %s", command)
}

func (m *MockSSHTransport) SendSnapshot(reader io.Reader, isIncremental bool) error {
	m.CallLog = append(m.CallLog, fmt.Sprintf("SendSnapshot: incremental=%t", isIncremental))
	return m.SendSnapshotError
}

func (m *MockSSHTransport) ListRemoteSnapshots() ([]string, error) {
	m.CallLog = append(m.CallLog, "ListRemoteSnapshots")
	return m.RemoteSnapshots, nil
}

func (m *MockSSHTransport) ListAllRemoteDatasets() (map[string][]string, error) {
	m.CallLog = append(m.CallLog, "ListAllRemoteDatasets")
	return m.RemoteDatasets, nil
}

func (m *MockSSHTransport) GetSnapshotsForDataset(dataset string) ([]string, error) {
	m.CallLog = append(m.CallLog, fmt.Sprintf("GetSnapshotsForDataset: %s", dataset))
	if snapshots, exists := m.RemoteDatasets[dataset]; exists {
		return snapshots, nil
	}
	return []string{}, nil
}

func (m *MockSSHTransport) GetRemoteDatasetInfo(dataset string) (*transport.RemoteDatasetInfo, error) {
	m.CallLog = append(m.CallLog, fmt.Sprintf("GetRemoteDatasetInfo: %s", dataset))
	if info, exists := m.DatasetInfos[dataset]; exists {
		return info, nil
	}
	return nil, fmt.Errorf("dataset not found: %s", dataset)
}

func (m *MockSSHTransport) RestoreSnapshot(snapshotName, localDataset string) error {
	m.CallLog = append(m.CallLog, fmt.Sprintf("RestoreSnapshot: %s -> %s", snapshotName, localDataset))
	return m.RestoreError
}

func (m *MockSSHTransport) RestoreSnapshotFromDataset(remoteDataset, snapshotName, localDataset string) error {
	m.CallLog = append(m.CallLog, fmt.Sprintf("RestoreSnapshotFromDataset: %s@%s -> %s", remoteDataset, snapshotName, localDataset))
	return m.RestoreError
}

func (m *MockSSHTransport) AddRemoteDataset(dataset string, snapshots []string) {
	m.RemoteDatasets[dataset] = snapshots
}

func (m *MockSSHTransport) SetRemoteDatasets(datasets map[string][]string) {
	m.RemoteDatasets = datasets
}

func (m *MockSSHTransport) SetDatasetInfo(dataset string, info *transport.RemoteDatasetInfo) {
	m.DatasetInfos[dataset] = info
}

func (m *MockSSHTransport) SetMockDatasetInfo(dataset string, info *MockRemoteDatasetInfo) {
	m.DatasetInfos[dataset] = &transport.RemoteDatasetInfo{
		Name:      info.Dataset,
		Snapshots: info.Snapshots,
		Used:      info.Used,
		Available: info.Available,
	}
}

func (m *MockSSHTransport) GetCallLog() []string {
	return m.CallLog
}

func (m *MockSSHTransport) ClearCallLog() {
	m.CallLog = make([]string, 0)
}