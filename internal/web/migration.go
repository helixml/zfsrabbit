package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"zfsrabbit/internal/restore"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

// MigrationWizard handles the step-by-step migration process
type MigrationWizard struct {
	transport      *transport.SSHTransport
	zfsManager     *zfs.Manager
	restoreManager *restore.RestoreManager
	scheduler      *scheduler.Scheduler
}

// MigrationSession tracks an active migration
type MigrationSession struct {
	ID              string    `json:"id"`
	SourceDataset   string    `json:"sourceDataset"`
	TargetHost      string    `json:"targetHost"`
	TargetDataset   string    `json:"targetDataset"`
	CurrentStep     int       `json:"currentStep"`
	Status          string    `json:"status"` // active, completed, failed, cancelled
	StartTime       time.Time `json:"startTime"`
	
	// Step-specific data
	InitialSnapshot  string    `json:"initialSnapshot,omitempty"`
	InitialSyncTime  *time.Time `json:"initialSyncTime,omitempty"`
	FinalSnapshot    string    `json:"finalSnapshot,omitempty"`
	CompletionTime   *time.Time `json:"completionTime,omitempty"`
	
	// User confirmations
	WorkloadStopped  bool      `json:"workloadStopped"`
	WorkloadStarted  bool      `json:"workloadStarted"`
	TargetPrepared   bool      `json:"targetPrepared"`
	
	Error            string    `json:"error,omitempty"`
}

var activeMigrationSession *MigrationSession

// Migration steps for the wizard
var migrationSteps = []struct {
	Title       string
	Description string
	Action      string
	TargetAction string // What user should do on target node
}{
	{
		Title:       "Prepare Migration",
		Description: "Set up initial parameters and validate both nodes are ready",
		Action:      "validate_setup",
	},
	{
		Title:       "Initial Data Sync",
		Description: "Create snapshot and sync majority of data while application runs",
		Action:      "initial_sync",
		TargetAction: "Go to target node and click 'Prepare Target Dataset'",
	},
	{
		Title:       "Stop Source Application",
		Description: "Stop your application on the source node to prevent new writes",
		Action:      "confirm_workload_stopped",
	},
	{
		Title:       "Final Sync",
		Description: "Create final snapshot and sync remaining changes",
		Action:      "final_sync",
		TargetAction: "Go to target node and click 'Start Final Restore'",
	},
	{
		Title:       "Start Target Application",
		Description: "Start your application on the target node",
		Action:      "confirm_workload_started",
	},
	{
		Title:       "Migration Complete",
		Description: "Verify everything is working and clean up",
		Action:      "complete",
	},
}

func NewMigrationWizard(transport *transport.SSHTransport, zfsManager *zfs.Manager, restoreManager *restore.RestoreManager, scheduler *scheduler.Scheduler) *MigrationWizard {
	return &MigrationWizard{
		transport:      transport,
		zfsManager:     zfsManager,
		restoreManager: restoreManager,
		scheduler:      scheduler,
	}
}

// StartMigrationHandler starts a new migration session (source node)
func (w *MigrationWizard) StartMigrationHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SourceDataset string `json:"sourceDataset"`
		TargetHost    string `json:"targetHost"`
		TargetDataset string `json:"targetDataset"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Only allow one active migration at a time
	if activeMigrationSession != nil && activeMigrationSession.Status == "active" {
		http.Error(rw, "Migration already in progress", http.StatusConflict)
		return
	}

	sessionID := fmt.Sprintf("migration_%d", time.Now().UnixNano())
	activeMigrationSession = &MigrationSession{
		ID:            sessionID,
		SourceDataset: req.SourceDataset,
		TargetHost:    req.TargetHost,
		TargetDataset: req.TargetDataset,
		CurrentStep:   0,
		Status:        "active",
		StartTime:     time.Now(),
	}

	log.Printf("Started migration session %s: %s -> %s:%s", 
		sessionID, req.SourceDataset, req.TargetHost, req.TargetDataset)

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(activeMigrationSession)
}

// GetMigrationStatusHandler returns current migration status
func (w *MigrationWizard) GetMigrationStatusHandler(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	
	if activeMigrationSession == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"active": false,
			"steps":  migrationSteps,
		})
		return
	}

	response := map[string]interface{}{
		"active":   true,
		"session":  activeMigrationSession,
		"steps":    migrationSteps,
		"currentStepInfo": migrationSteps[activeMigrationSession.CurrentStep],
	}

	json.NewEncoder(rw).Encode(response)
}

// ExecuteStepHandler executes the current migration step
func (w *MigrationWizard) ExecuteStepHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if activeMigrationSession == nil || activeMigrationSession.Status != "active" {
		http.Error(rw, "No active migration", http.StatusBadRequest)
		return
	}

	session := activeMigrationSession
	step := migrationSteps[session.CurrentStep]

	var err error
	
	switch step.Action {
	case "validate_setup":
		err = w.validateSetup(session)
	case "initial_sync":
		err = w.performInitialSync(session)
	case "confirm_workload_stopped":
		session.WorkloadStopped = true
		log.Printf("Migration %s: User confirmed workload stopped", session.ID)
	case "final_sync":
		err = w.performFinalSync(session)
	case "confirm_workload_started":
		session.WorkloadStarted = true
		log.Printf("Migration %s: User confirmed workload started", session.ID)
	case "complete":
		err = w.completeMigration(session)
	default:
		err = fmt.Errorf("unknown step action: %s", step.Action)
	}

	if err != nil {
		session.Status = "failed"
		session.Error = err.Error()
		log.Printf("Migration %s failed at step %d: %v", session.ID, session.CurrentStep, err)
		
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"session": session,
		})
		return
	}

	// Advance to next step (unless we're at the last step)
	if session.CurrentStep < len(migrationSteps)-1 {
		session.CurrentStep++
	}

	log.Printf("Migration %s completed step %d: %s", session.ID, session.CurrentStep-1, step.Title)

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"success": true,
		"session": session,
	})
}

func (w *MigrationWizard) validateSetup(session *MigrationSession) error {
	// Validate source dataset exists
	snapshots, err := w.zfsManager.ListSnapshots()
	if err != nil {
		return fmt.Errorf("failed to list local snapshots: %w", err)
	}

	datasetExists := false
	for _, snap := range snapshots {
		if snap.Dataset == session.SourceDataset {
			datasetExists = true
			break
		}
	}

	if !datasetExists {
		return fmt.Errorf("source dataset %s not found", session.SourceDataset)
	}

	// Test remote connectivity
	if err := w.transport.Connect(); err != nil {
		return fmt.Errorf("failed to connect to backup server: %w", err)
	}

	log.Printf("Migration %s: Setup validation passed", session.ID)
	return nil
}

func (w *MigrationWizard) performInitialSync(session *MigrationSession) error {
	// Just trigger a normal backup - this creates and sends a regular autosnap_* snapshot
	log.Printf("Migration %s: Triggering normal backup for initial sync", session.ID)
	
	if err := w.scheduler.TriggerSnapshot(); err != nil {
		return fmt.Errorf("failed to trigger backup: %w", err)
	}
	
	// The scheduler creates autosnap_YYYY-MM-DD_HH-MM-SS snapshots
	// We'll get the latest snapshot name after it's created
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	session.InitialSnapshot = fmt.Sprintf("autosnap_%s", timestamp)
	
	session.InitialSyncTime = &time.Time{}
	*session.InitialSyncTime = time.Now()
	
	log.Printf("Migration %s: Initial backup triggered - regular snapshot will be sent to backup server", session.ID)
	return nil
}

func (w *MigrationWizard) performFinalSync(session *MigrationSession) error {
	if !session.WorkloadStopped {
		return fmt.Errorf("workload must be stopped before final sync")
	}

	// Just trigger another normal backup - this creates and sends another regular autosnap_* snapshot
	log.Printf("Migration %s: Triggering final backup (incremental from %s)", 
		session.ID, session.InitialSnapshot)
	
	if err := w.scheduler.TriggerSnapshot(); err != nil {
		return fmt.Errorf("failed to trigger final backup: %w", err)
	}
	
	// The scheduler will automatically do incremental send from last snapshot
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	session.FinalSnapshot = fmt.Sprintf("autosnap_%s", timestamp)
	
	log.Printf("Migration %s: Final backup triggered - incremental changes will be sent to backup server", session.ID)
	return nil
}

func (w *MigrationWizard) completeMigration(session *MigrationSession) error {
	if !session.WorkloadStarted {
		return fmt.Errorf("workload must be started on target before completing migration")
	}

	session.Status = "completed"
	session.CompletionTime = &time.Time{}
	*session.CompletionTime = time.Now()
	
	log.Printf("Migration %s completed successfully", session.ID)
	return nil
}

// Target node handlers (simpler)

// PrepareTargetHandler prepares the target dataset (target node)
func (w *MigrationWizard) PrepareTargetHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SourceDataset string `json:"sourceDataset"`
		TargetDataset string `json:"targetDataset"`
		SnapshotName  string `json:"snapshotName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Target node: Preparing dataset %s from snapshot %s (source: %s)", 
		req.TargetDataset, req.SnapshotName, req.SourceDataset)
	
	// Use the restore manager to restore the initial snapshot from backup server
	// IMPORTANT: This may overwrite existing data on target if target dataset already exists
	// The restore manager should handle this safely with appropriate warnings
	job, err := w.restoreManager.RestoreSnapshotFromDataset(req.SourceDataset, req.SnapshotName, req.TargetDataset)
	if err != nil {
		log.Printf("Target node: Failed to start restore: %v", err)
		http.Error(rw, fmt.Sprintf("Failed to start restore: %v", err), http.StatusInternalServerError)
		return
	}
	
	log.Printf("Target node: Started restore job %s for migration snapshot %s", job.ID, req.SnapshotName)
	
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"success":   true,
		"message":   fmt.Sprintf("Target dataset %s restore started", req.TargetDataset),
		"restoreJobId": job.ID,
	})
}

// FinalRestoreHandler performs final incremental restore (target node)  
func (w *MigrationWizard) FinalRestoreHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SourceDataset string `json:"sourceDataset"`
		TargetDataset string `json:"targetDataset"`
		SnapshotName  string `json:"snapshotName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Target node: Final incremental restore to %s from snapshot %s (source: %s)", 
		req.TargetDataset, req.SnapshotName, req.SourceDataset)
	
	// Use the restore manager to restore the final incremental snapshot from backup server
	// This will be an incremental restore on top of the initial snapshot already restored
	job, err := w.restoreManager.RestoreSnapshotFromDataset(req.SourceDataset, req.SnapshotName, req.TargetDataset)
	if err != nil {
		log.Printf("Target node: Failed to start final restore: %v", err)
		http.Error(rw, fmt.Sprintf("Failed to start final restore: %v", err), http.StatusInternalServerError)
		return
	}
	
	log.Printf("Target node: Started final restore job %s for migration snapshot %s", job.ID, req.SnapshotName)
	
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Final restore to %s started", req.TargetDataset),
		"restoreJobId": job.ID,
	})
}

// CancelMigrationHandler cancels active migration
func (w *MigrationWizard) CancelMigrationHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if activeMigrationSession != nil {
		activeMigrationSession.Status = "cancelled"
		log.Printf("Migration %s cancelled by user", activeMigrationSession.ID)
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"success": true,
		"message": "Migration cancelled",
	})
}