package restore

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type RestoreManager struct {
	transport  *transport.SSHTransport
	zfsManager *zfs.Manager
}

type RestoreJob struct {
	ID              string
	SnapshotName    string
	SourceDataset   string
	TargetDataset   string
	Status          string // starting, verifying, safety_check, awaiting_confirmation, restoring, completed, failed
	Progress        int
	StartTime       time.Time
	EndTime         *time.Time
	Error           error
	RequiresConfirm bool   // True if destructive operation needs user confirmation
	SafetyWarning   string // Warning message about data loss
	ForceConfirmed  bool   // Set to true by user to proceed with destructive operation
}

func New(transport *transport.SSHTransport, zfsManager *zfs.Manager) *RestoreManager {
	return &RestoreManager{
		transport:  transport,
		zfsManager: zfsManager,
	}
}

// ConfirmDestructiveRestore allows user to confirm and proceed with a destructive restore
func (r *RestoreManager) ConfirmDestructiveRestore(jobID string) error {
	job, exists := activeJobs[jobID]
	if !exists {
		return fmt.Errorf("restore job %s not found", jobID)
	}
	
	if job.Status != "awaiting_confirmation" {
		return fmt.Errorf("restore job %s is not awaiting confirmation (status: %s)", jobID, job.Status)
	}
	
	if !job.RequiresConfirm {
		return fmt.Errorf("restore job %s does not require confirmation", jobID)
	}
	
	log.Printf("User confirmed destructive restore for job %s - proceeding with data loss", jobID)
	
	// Set confirmation flag and restart the restore process
	job.ForceConfirmed = true
	job.RequiresConfirm = false
	job.SafetyWarning = ""
	
	// Resume the restore process
	go r.performRestore(job)
	
	return nil
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

	// CRITICAL SAFETY CHECK: Check if target dataset exists and warn about data loss
	job.Status = "safety_check"
	job.Progress = 5
	
	exists, err := r.checkTargetDatasetExists(job.TargetDataset)
	if err != nil {
		r.failJob(job, fmt.Errorf("failed to check target dataset: %w", err))
		return
	}
	
	if exists {
		hasUncommittedData, err := r.checkForUncommittedData(job.TargetDataset)
		if err != nil {
			r.failJob(job, fmt.Errorf("failed to check for uncommitted data: %w", err))
			return
		}
		
		if hasUncommittedData && !job.ForceConfirmed {
			// STOP and require manual confirmation
			job.RequiresConfirm = true
			job.Status = "awaiting_confirmation"
			job.SafetyWarning = fmt.Sprintf("⚠️  DESTRUCTIVE OPERATION WARNING ⚠️\n\n"+
				"Target dataset '%s' contains data that will be PERMANENTLY LOST.\n"+
				"ZFS restore will roll back to the snapshot, destroying any changes made after the last snapshot.\n\n"+
				"🚨 DO NOT proceed if you have active workloads writing to this filesystem!\n\n"+
				"To proceed:\n"+
				"1. STOP all applications writing to %s\n"+
				"2. Manually confirm you want to lose uncommitted data\n"+
				"3. Click 'Force Restore' to proceed\n\n"+
				"This action cannot be undone!", job.TargetDataset, job.TargetDataset)
			
			log.Printf("Restore job %s requires manual confirmation - target dataset has uncommitted data", job.ID)
			return // Wait for user confirmation
		}
	}

	// Step 1: Verify remote snapshot exists
	job.Status = "verifying"
	job.Progress = 10

	var remoteSnapshots []string
	var remoteErr error

	if job.SourceDataset != "" {
		// Get snapshots from specific dataset
		remoteSnapshots, remoteErr = r.transport.GetSnapshotsForDataset(job.SourceDataset)
		if remoteErr != nil {
			r.failJob(job, fmt.Errorf("failed to list snapshots for dataset %s: %w", job.SourceDataset, remoteErr))
			return
		}
	} else {
		// Use default remote dataset
		remoteSnapshots, remoteErr = r.transport.ListRemoteSnapshots()
		if remoteErr != nil {
			r.failJob(job, fmt.Errorf("failed to list remote snapshots: %w", remoteErr))
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
	exists, existsErr := r.datasetExists(job.TargetDataset)
	if existsErr != nil {
		r.failJob(job, fmt.Errorf("failed to check if dataset exists: %w", existsErr))
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
	if job.ForceConfirmed {
		// User confirmed destructive operation - use force mode
		log.Printf("Restore job %s: Using DESTRUCTIVE mode (user confirmed)", job.ID)
		if job.SourceDataset != "" {
			restoreErr = r.transport.RestoreSnapshotFromDataset(job.SourceDataset, job.SnapshotName, job.TargetDataset)
		} else {
			restoreErr = r.transport.RestoreSnapshot(job.SnapshotName, job.TargetDataset)
		}
	} else {
		// Use safe mode - will fail if conflicts exist
		log.Printf("Restore job %s: Using SAFE mode (no data loss)", job.ID)
		if job.SourceDataset != "" {
			restoreErr = r.transport.RestoreSnapshotFromDatasetSafe(job.SourceDataset, job.SnapshotName, job.TargetDataset)
		} else {
			restoreErr = r.transport.RestoreSnapshotSafe(job.SnapshotName, job.TargetDataset)
		}
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

func (r *RestoreManager) checkTargetDatasetExists(dataset string) (bool, error) {
	snapshots, err := r.zfsManager.ListSnapshots()
	if err != nil {
		return false, nil // Assume doesn't exist if we can't check
	}
	
	// Check if any snapshots exist for this dataset
	for _, snap := range snapshots {
		if snap.Dataset == dataset {
			return true, nil
		}
	}
	return false, nil
}

func (r *RestoreManager) checkForUncommittedData(dataset string) (bool, error) {
	// Method 1: Check if dataset exists at all
	snapshots, err := r.zfsManager.ListSnapshots()
	if err != nil {
		return false, err
	}
	
	var latestSnapshot *zfs.Snapshot
	for _, snap := range snapshots {
		if snap.Dataset == dataset {
			if latestSnapshot == nil || snap.Created.After(latestSnapshot.Created) {
				latestSnapshot = &snap
			}
		}
	}
	
	if latestSnapshot == nil {
		// Dataset exists but no snapshots = definitely has uncommitted data
		return true, nil
	}
	
	// Use ZFS diff - the ONE reliable way to check for changes since snapshot
	hasChanges, err := r.checkZFSDiffSinceSnapshot(dataset, latestSnapshot.Name)
	if err != nil {
		return false, fmt.Errorf("failed to check for uncommitted data using zfs diff: %w", err)
	}
	
	return hasChanges, nil
}

func (r *RestoreManager) checkZFSDiffSinceSnapshot(dataset, snapshotName string) (bool, error) {
	// Use ZFS diff - the ONE way to detect changes since snapshot
	cmd := exec.Command("zfs", "diff", fmt.Sprintf("%s@%s", dataset, snapshotName))
	output, err := cmd.Output()
	
	if err != nil {
		return false, fmt.Errorf("zfs diff command failed for %s@%s: %w", dataset, snapshotName, err)
	}
	
	// If output is empty, no changes since snapshot
	diffOutput := strings.TrimSpace(string(output))
	if diffOutput == "" {
		return false, nil // No changes detected
	}
	
	log.Printf("ZFS diff detected changes in %s since %s:\n%s", dataset, snapshotName, diffOutput)
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
