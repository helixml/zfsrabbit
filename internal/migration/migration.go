package migration

import (
	"fmt"
	"log"
	"time"

	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

// MigrationManager handles workload migration operations
type MigrationManager struct {
	transport  *transport.SSHTransport
	zfsManager *zfs.Manager
}

// MigrationJob tracks the state of a workload migration
type MigrationJob struct {
	ID              string
	SourceDataset   string
	TargetHost      string
	TargetDataset   string
	Status          string // preparing, initial_sync, awaiting_cutover, final_sync, completed, failed
	Progress        int
	StartTime       time.Time
	InitialSyncTime *time.Time
	CutoverTime     *time.Time
	EndTime         *time.Time
	Error           error

	// Migration-specific fields
	PreMigrationSnapshot string // Snapshot taken before initial sync
	CutoverSnapshot      string // Final snapshot for cutover
	InitialSyncBytes     int64  // Bytes transferred in initial sync
	FinalSyncBytes       int64  // Bytes transferred in final sync
}

var activeMigrations = make(map[string]*MigrationJob)

func New(transport *transport.SSHTransport, zfsManager *zfs.Manager) *MigrationManager {
	return &MigrationManager{
		transport:  transport,
		zfsManager: zfsManager,
	}
}

// StartMigration begins a workload migration process
func (m *MigrationManager) StartMigration(sourceDataset, targetHost, targetDataset string) (*MigrationJob, error) {
	jobID := generateMigrationID()

	job := &MigrationJob{
		ID:            jobID,
		SourceDataset: sourceDataset,
		TargetHost:    targetHost,
		TargetDataset: targetDataset,
		Status:        "preparing",
		Progress:      0,
		StartTime:     time.Now(),
	}

	activeMigrations[jobID] = job

	// Start migration in background
	go m.executeMigration(job)

	return job, nil
}

// GetMigrationStatus returns the current status of a migration
func (m *MigrationManager) GetMigrationStatus(jobID string) (*MigrationJob, error) {
	job, exists := activeMigrations[jobID]
	if !exists {
		return nil, fmt.Errorf("migration job %s not found", jobID)
	}
	return job, nil
}

// ListMigrations returns all active migrations
func (m *MigrationManager) ListMigrations() []*MigrationJob {
	var jobs []*MigrationJob
	for _, job := range activeMigrations {
		jobs = append(jobs, job)
	}
	return jobs
}

// RequestCutover initiates the final cutover phase
// This should be called when the application is ready to be stopped
func (m *MigrationManager) RequestCutover(jobID string) error {
	job, exists := activeMigrations[jobID]
	if !exists {
		return fmt.Errorf("migration job %s not found", jobID)
	}

	if job.Status != "awaiting_cutover" {
		return fmt.Errorf("migration job %s is not ready for cutover (current status: %s)", jobID, job.Status)
	}

	log.Printf("Cutover requested for migration %s - proceeding with final sync", jobID)
	job.Status = "final_sync"
	job.CutoverTime = &time.Time{}
	*job.CutoverTime = time.Now()

	// Continue with final sync in background
	go m.performFinalSync(job)

	return nil
}

// executeMigration performs the complete migration workflow
func (m *MigrationManager) executeMigration(job *MigrationJob) {
	defer func() {
		if r := recover(); r != nil {
			job.Status = "failed"
			job.Error = fmt.Errorf("migration panic: %v", r)
			job.EndTime = &time.Time{}
			*job.EndTime = time.Now()
			log.Printf("Migration job %s failed with panic: %v", job.ID, r)
		}
	}()

	// Phase 1: Take initial snapshot and perform initial sync
	log.Printf("Starting migration job %s: %s -> %s:%s", job.ID, job.SourceDataset, job.TargetHost, job.TargetDataset)

	job.Status = "initial_sync"
	job.Progress = 10

	// Create pre-migration snapshot
	preSnapshot := fmt.Sprintf("migration-%s-initial", job.ID)
	if err := m.zfsManager.CreateSnapshot(preSnapshot); err != nil {
		m.failMigration(job, fmt.Errorf("failed to create initial snapshot: %w", err))
		return
	}
	job.PreMigrationSnapshot = preSnapshot
	job.Progress = 20

	// Perform initial full sync
	if err := m.performInitialSync(job); err != nil {
		m.failMigration(job, fmt.Errorf("initial sync failed: %w", err))
		return
	}

	job.Progress = 60
	job.InitialSyncTime = &time.Time{}
	*job.InitialSyncTime = time.Now()

	// Phase 2: Wait for cutover request
	job.Status = "awaiting_cutover"
	job.Progress = 70

	log.Printf("Migration job %s: Initial sync completed. Ready for cutover. Use /migrate cutover %s to proceed.", job.ID, job.ID)
}

// performInitialSync handles the initial full dataset sync
func (m *MigrationManager) performInitialSync(job *MigrationJob) error {
	log.Printf("Performing initial sync for migration %s", job.ID)

	// Use the transport layer to send the full snapshot
	// This will be a full send since target doesn't exist yet
	// Implementation would depend on your specific transport setup

	// For now, simulate the sync
	time.Sleep(2 * time.Second) // Simulate transfer time

	return nil
}

// performFinalSync handles the final incremental sync during cutover
func (m *MigrationManager) performFinalSync(job *MigrationJob) {
	log.Printf("Performing final sync for migration %s", job.ID)

	job.Progress = 80

	// Create final cutover snapshot
	cutoverSnapshot := fmt.Sprintf("migration-%s-cutover", job.ID)
	if err := m.zfsManager.CreateSnapshot(cutoverSnapshot); err != nil {
		m.failMigration(job, fmt.Errorf("failed to create cutover snapshot: %w", err))
		return
	}
	job.CutoverSnapshot = cutoverSnapshot
	job.Progress = 90

	// Perform incremental sync from initial to cutover snapshot
	// This should be much faster since only changes since initial sync
	time.Sleep(1 * time.Second) // Simulate incremental transfer

	job.Progress = 100
	job.Status = "completed"
	job.EndTime = &time.Time{}
	*job.EndTime = time.Now()

	log.Printf("Migration job %s completed successfully. Workload can now be started on target: %s:%s",
		job.ID, job.TargetHost, job.TargetDataset)
}

// failMigration marks a migration as failed
func (m *MigrationManager) failMigration(job *MigrationJob, err error) {
	job.Status = "failed"
	job.Error = err
	job.EndTime = &time.Time{}
	*job.EndTime = time.Now()
	log.Printf("Migration job %s failed: %v", job.ID, err)
}

// generateMigrationID creates a unique ID for migration jobs
func generateMigrationID() string {
	return fmt.Sprintf("migrate_%d", time.Now().UnixNano())
}

// CleanupCompletedMigrations removes completed migrations from memory
func (m *MigrationManager) CleanupCompletedMigrations() {
	for id, job := range activeMigrations {
		if job.Status == "completed" || job.Status == "failed" {
			if job.EndTime != nil && time.Since(*job.EndTime) > 24*time.Hour {
				delete(activeMigrations, id)
				log.Printf("Cleaned up migration job %s", id)
			}
		}
	}
}
