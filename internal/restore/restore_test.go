package restore

import (
	"testing"
	"time"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

func TestNewRestoreManager(t *testing.T) {
	// Create real components for unit testing
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)

	manager := New(sshTransport, zfsManager)

	if manager == nil {
		t.Fatal("Expected restore manager to be created")
	}

	if manager.transport != sshTransport {
		t.Error("Transport not set correctly")
	}

	if manager.zfsManager != zfsManager {
		t.Error("ZFS manager not set correctly")
	}
}

func TestRestoreSnapshot(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	snapshotName := "test-snapshot"
	targetDataset := "tank/restored"

	job, err := manager.RestoreSnapshot(snapshotName, targetDataset)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if job == nil {
		t.Fatal("Expected job to be created")
	}

	if job.SnapshotName != snapshotName {
		t.Errorf("Expected snapshot name %s, got %s", snapshotName, job.SnapshotName)
	}

	if job.TargetDataset != targetDataset {
		t.Errorf("Expected target dataset %s, got %s", targetDataset, job.TargetDataset)
	}

	if job.SourceDataset != "" {
		t.Errorf("Expected empty source dataset for default restore, got %s", job.SourceDataset)
	}

	if job.Status != "starting" {
		t.Errorf("Expected status 'starting', got %s", job.Status)
	}

	if job.ID == "" {
		t.Error("Expected job ID to be generated")
	}
}

func TestRestoreSnapshotFromDataset(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	sourceDataset := "backup/server1"
	snapshotName := "test-snapshot"
	targetDataset := "tank/restored"

	job, err := manager.RestoreSnapshotFromDataset(sourceDataset, snapshotName, targetDataset)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if job == nil {
		t.Fatal("Expected job to be created")
	}

	if job.SourceDataset != sourceDataset {
		t.Errorf("Expected source dataset %s, got %s", sourceDataset, job.SourceDataset)
	}

	if job.SnapshotName != snapshotName {
		t.Errorf("Expected snapshot name %s, got %s", snapshotName, job.SnapshotName)
	}

	if job.TargetDataset != targetDataset {
		t.Errorf("Expected target dataset %s, got %s", targetDataset, job.TargetDataset)
	}
}

func TestGenerateJobID(t *testing.T) {
	id1 := generateJobID()
	time.Sleep(10 * time.Millisecond) // Ensure different timestamp
	id2 := generateJobID()

	if id1 == id2 {
		t.Error("Expected unique job IDs")
	}

	if len(id1) == 0 {
		t.Error("Expected non-empty job ID")
	}

	if len(id2) == 0 {
		t.Error("Expected non-empty job ID")
	}
}

func TestStartRestoreWithTracking(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	job, err := manager.StartRestoreWithTracking("test-snapshot", "tank/restored")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if job == nil {
		t.Fatal("Expected job to be created")
	}

	// Verify job is in active jobs map
	trackedJob, exists := manager.GetJob(job.ID)
	if !exists {
		t.Error("Expected job to be tracked")
	}

	if trackedJob != job {
		t.Error("Expected tracked job to be the same instance")
	}
}

func TestStartRestoreFromDatasetWithTracking(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	job, err := manager.StartRestoreFromDatasetWithTracking("backup/server1", "test-snapshot", "tank/restored")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if job == nil {
		t.Fatal("Expected job to be created")
	}

	if job.SourceDataset != "backup/server1" {
		t.Errorf("Expected source dataset backup/server1, got %s", job.SourceDataset)
	}

	// Verify job is tracked
	_, exists := manager.GetJob(job.ID)
	if !exists {
		t.Error("Expected job to be tracked")
	}
}

func TestGetJob(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	// Test non-existent job
	_, exists := manager.GetJob("nonexistent")
	if exists {
		t.Error("Expected job to not exist")
	}

	// Create a job and test
	job, err := manager.StartRestoreWithTracking("test-snapshot", "tank/test")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test existing job
	retrievedJob, exists := manager.GetJob(job.ID)
	if !exists {
		t.Error("Expected job to exist")
	}

	if retrievedJob != job {
		t.Error("Expected retrieved job to be the same instance")
	}
}

func TestListJobs(t *testing.T) {
	cfg := &config.SSHConfig{
		RemoteHost:    "test.example.com",
		RemoteUser:    "testuser",
		RemoteDataset: "backup/test",
	}

	sshTransport := transport.NewSSHTransport(cfg)
	zfsManager := zfs.New("tank/test", "lz4", false)
	manager := New(sshTransport, zfsManager)

	// Get initial count (may have jobs from other tests)
	jobs := manager.ListJobs()
	initialCount := len(jobs)

	// Create some jobs
	job1, err := manager.StartRestoreWithTracking("test-snapshot1", "tank/test1")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	job2, err := manager.StartRestoreWithTracking("test-snapshot2", "tank/test2")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	jobs = manager.ListJobs()
	if len(jobs) != initialCount+2 {
		t.Errorf("Expected %d jobs, got %d", initialCount+2, len(jobs))
	}

	// Verify jobs are in the list
	foundJob1 := false
	foundJob2 := false
	for _, job := range jobs {
		if job.ID == job1.ID {
			foundJob1 = true
		}
		if job.ID == job2.ID {
			foundJob2 = true
		}
	}

	if !foundJob1 {
		t.Error("Expected job1 in list")
	}

	if !foundJob2 {
		t.Error("Expected job2 in list")
	}
}

// Skip the actual restore tests that would require real ZFS and SSH connectivity
func TestPerformRestore_SkipIntegration(t *testing.T) {
	t.Skip("Skipping restore integration test - requires ZFS and SSH connectivity")
}
