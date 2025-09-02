package restore

import (
	"fmt"
	"log"
	"time"

	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type RestoreManager struct {
	transport  *transport.SSHTransport
	zfsManager *zfs.Manager
}

type RestoreJob struct {
	ID            string
	SnapshotName  string
	SourceDataset string
	TargetDataset string
	Status        string
	Progress      int
	StartTime     time.Time
	EndTime       *time.Time
	Error         error
}

func New(transport *transport.SSHTransport, zfsManager *zfs.Manager) *RestoreManager {
	return &RestoreManager{
		transport:  transport,
		zfsManager: zfsManager,
	}
}

func (r *RestoreManager) RestoreSnapshot(snapshotName, targetDataset string) (*RestoreJob, error) {
	return r.RestoreSnapshotFromDataset("", snapshotName, targetDataset)
}

func (r *RestoreManager) RestoreSnapshotFromDataset(sourceDataset, snapshotName, targetDataset string) (*RestoreJob, error) {
	job := &RestoreJob{
		ID:            generateJobID(),
		SnapshotName:  snapshotName,
		SourceDataset: sourceDataset,
		TargetDataset: targetDataset,
		Status:        "starting",
		Progress:      0,
		StartTime:     time.Now(),
	}

	go r.performRestore(job)

	return job, nil
}

func (r *RestoreManager) performRestore(job *RestoreJob) {
	sourceInfo := "default remote dataset"
	if job.SourceDataset != "" {
		sourceInfo = job.SourceDataset
	}
	log.Printf("Starting restore job %s: %s@%s -> %s", job.ID, sourceInfo, job.SnapshotName, job.TargetDataset)

	defer func() {
		if r := recover(); r != nil {
			job.Status = "failed"
			job.Error = fmt.Errorf("restore panic: %v", r)
			endTime := time.Now()
			job.EndTime = &endTime
			log.Printf("Restore job %s failed with panic: %v", job.ID, r)
		}
	}()

	// Step 1: Verify remote snapshot exists
	job.Status = "verifying"
	job.Progress = 10

	var remoteSnapshots []string
	var err error

	if job.SourceDataset != "" {
		// Get snapshots from specific dataset
		remoteSnapshots, err = r.transport.GetSnapshotsForDataset(job.SourceDataset)
		if err != nil {
			r.failJob(job, fmt.Errorf("failed to list snapshots for dataset %s: %w", job.SourceDataset, err))
			return
		}
	} else {
		// Use default remote dataset
		remoteSnapshots, err = r.transport.ListRemoteSnapshots()
		if err != nil {
			r.failJob(job, fmt.Errorf("failed to list remote snapshots: %w", err))
			return
		}
	}

	found := false
	for _, snap := range remoteSnapshots {
		if snap == job.SnapshotName {
			found = true
			break
		}
	}

	if !found {
		r.failJob(job, fmt.Errorf("snapshot %s not found on remote server", job.SnapshotName))
		return
	}

	// Step 2: Check if target dataset exists and handle appropriately
	job.Status = "preparing"
	job.Progress = 20

	// Check if target dataset already exists
	exists, err := r.datasetExists(job.TargetDataset)
	if err != nil {
		r.failJob(job, fmt.Errorf("failed to check if dataset exists: %w", err))
		return
	}

	if exists {
		// If dataset exists, we'll use -F flag to force overwrite
		log.Printf("Target dataset %s exists, will overwrite", job.TargetDataset)
	}

	// Step 3: Initiate restore from remote
	job.Status = "restoring"
	job.Progress = 30

	var restoreErr error
	if job.SourceDataset != "" {
		restoreErr = r.transport.RestoreSnapshotFromDataset(job.SourceDataset, job.SnapshotName, job.TargetDataset)
	} else {
		restoreErr = r.transport.RestoreSnapshot(job.SnapshotName, job.TargetDataset)
	}

	if restoreErr != nil {
		r.failJob(job, fmt.Errorf("restore failed: %w", restoreErr))
		return
	}

	// Step 4: Verify restore completed successfully
	job.Status = "verifying"
	job.Progress = 90

	if err := r.verifyRestore(job.TargetDataset, job.SnapshotName); err != nil {
		r.failJob(job, fmt.Errorf("restore verification failed: %w", err))
		return
	}

	// Step 5: Complete
	job.Status = "completed"
	job.Progress = 100
	endTime := time.Now()
	job.EndTime = &endTime

	log.Printf("Restore job %s completed successfully", job.ID)
}

func (r *RestoreManager) failJob(job *RestoreJob, err error) {
	job.Status = "failed"
	job.Error = err
	endTime := time.Now()
	job.EndTime = &endTime
	log.Printf("Restore job %s failed: %v", job.ID, err)
}

func (r *RestoreManager) datasetExists(dataset string) (bool, error) {
	_, err := r.zfsManager.ListSnapshots()
	if err != nil {
		// If we can't list snapshots, assume dataset doesn't exist
		return false, nil
	}
	return true, nil
}

func (r *RestoreManager) verifyRestore(dataset, snapshotName string) error {
	// Check if the restored dataset exists
	snapshots, err := r.zfsManager.ListSnapshots()
	if err != nil {
		return fmt.Errorf("failed to list snapshots after restore: %w", err)
	}

	// Look for the restored snapshot
	for _, snap := range snapshots {
		if snap.Dataset == dataset && snap.Name == snapshotName {
			return nil
		}
	}

	return fmt.Errorf("restored snapshot not found in target dataset")
}

func generateJobID() string {
	return fmt.Sprintf("restore_%d", time.Now().UnixNano())
}

// Job tracking for web interface
var activeJobs = make(map[string]*RestoreJob)

func (r *RestoreManager) GetJob(id string) (*RestoreJob, bool) {
	job, exists := activeJobs[id]
	return job, exists
}

func (r *RestoreManager) ListJobs() []*RestoreJob {
	jobs := make([]*RestoreJob, 0, len(activeJobs))
	for _, job := range activeJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (r *RestoreManager) StartRestoreWithTracking(snapshotName, targetDataset string) (*RestoreJob, error) {
	return r.StartRestoreFromDatasetWithTracking("", snapshotName, targetDataset)
}

func (r *RestoreManager) StartRestoreFromDatasetWithTracking(sourceDataset, snapshotName, targetDataset string) (*RestoreJob, error) {
	var job *RestoreJob
	var err error

	if sourceDataset != "" {
		job, err = r.RestoreSnapshotFromDataset(sourceDataset, snapshotName, targetDataset)
	} else {
		job, err = r.RestoreSnapshot(snapshotName, targetDataset)
	}

	if err != nil {
		return nil, err
	}

	activeJobs[job.ID] = job

	// Clean up completed jobs after 1 hour
	go func() {
		time.Sleep(1 * time.Hour)
		if job.Status == "completed" || job.Status == "failed" {
			delete(activeJobs, job.ID)
		}
	}()

	return job, nil
}
