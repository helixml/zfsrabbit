package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/monitor"
	"zfsrabbit/internal/restore"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/slack"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type Server struct {
	config          *config.Config
	scheduler       *scheduler.Scheduler
	monitor         *monitor.Monitor
	zfsManager      *zfs.Manager
	restoreManager  *restore.RestoreManager
	migrationWizard *MigrationWizard
	slackHandler    *slack.CommandHandler
	transport       *transport.SSHTransport
	httpServer      *http.Server
}

func NewServer(cfg *config.Config, sched *scheduler.Scheduler, mon *monitor.Monitor, zfsMgr *zfs.Manager, restoreMgr *restore.RestoreManager, transport *transport.SSHTransport) *Server {
	slackHandler := slack.NewCommandHandler(&cfg.Slack, sched, mon, zfsMgr, restoreMgr, transport)
	migrationWizard := NewMigrationWizard(transport, zfsMgr, restoreMgr, sched)

	return &Server{
		config:          cfg,
		scheduler:       sched,
		monitor:         mon,
		zfsManager:      zfsMgr,
		restoreManager:  restoreMgr,
		migrationWizard: migrationWizard,
		slackHandler:    slackHandler,
		transport:       transport,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.basicAuth(s.handleIndex))
	mux.HandleFunc("/api/status", s.basicAuth(s.handleStatus))
	mux.HandleFunc("/api/snapshots", s.basicAuth(s.handleSnapshots))
	mux.HandleFunc("/api/trigger/snapshot", s.basicAuth(s.handleTriggerSnapshot))
	mux.HandleFunc("/api/trigger/scrub", s.basicAuth(s.handleTriggerScrub))
	mux.HandleFunc("/api/trigger/retry", s.basicAuth(s.handleRetryPendingSends))
	mux.HandleFunc("/api/restore", s.basicAuth(s.handleRestore))
	mux.HandleFunc("/api/restore/jobs", s.basicAuth(s.handleRestoreJobs))
	mux.HandleFunc("/api/restore/confirm/", s.basicAuth(s.handleRestoreConfirm))
	mux.HandleFunc("/api/remote/datasets", s.basicAuth(s.handleRemoteDatasets))
	mux.HandleFunc("/api/remote/dataset/", s.basicAuth(s.handleRemoteDatasetInfo))
	mux.HandleFunc("/api/migration/start", s.basicAuth(s.migrationWizard.StartMigrationHandler))
	mux.HandleFunc("/api/migration/status", s.basicAuth(s.migrationWizard.GetMigrationStatusHandler))
	mux.HandleFunc("/api/migration/step", s.basicAuth(s.migrationWizard.ExecuteStepHandler))
	mux.HandleFunc("/api/migration/cancel", s.basicAuth(s.migrationWizard.CancelMigrationHandler))
	mux.HandleFunc("/api/migration/target/prepare", s.basicAuth(s.migrationWizard.PrepareTargetHandler))
	mux.HandleFunc("/api/migration/target/restore", s.basicAuth(s.migrationWizard.FinalRestoreHandler))
	mux.HandleFunc("/migration", s.basicAuth(s.handleMigrationPage))
	mux.HandleFunc("/health", s.handleHealth) // Unauthenticated health check
	mux.HandleFunc("/slack/command", s.slackHandler.HandleSlashCommand)
	mux.HandleFunc("/static/", s.handleStatic)

	addr := fmt.Sprintf(":%d", s.config.Server.Port)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Web server starting on %s", addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		log.Println("Gracefully shutting down web server")
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) basicAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != s.config.GetAdminPassword() {
			w.Header().Set("WWW-Authenticate", `Basic realm="ZFSRabbit"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized"))
			return
		}
		handler(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, "web/templates/dashboard.html")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.monitor.GetSystemStatus()

	healthy := true
	if pools, ok := status["pools"].(map[string]interface{}); ok {
		for _, pool := range pools {
			if poolData, ok := pool.(*zfs.PoolStatus); ok {
				if poolData.State != "ONLINE" || len(poolData.Errors) > 0 {
					healthy = false
					break
				}
			}
		}
	}

	response := map[string]interface{}{
		"healthy":      healthy,
		"pools":        status["pools"],
		"disks":        status["disks"],
		"pendingSends": s.scheduler.GetPendingSends(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.zfsManager.ListSnapshots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]map[string]string, len(snapshots))
	for i, snap := range snapshots {
		response[i] = map[string]string{
			"name":    snap.Name,
			"created": snap.Created.Format("2006-01-02 15:04:05"),
			"used":    snap.Used,
			"refer":   snap.Refer,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleTriggerSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.scheduler.TriggerSnapshot(); err != nil {
		if err.Error() == "snapshot operation already in progress" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error": "snapshot operation already in progress"}`))
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true, "message": "Snapshot triggered"}`))
}

func (s *Server) handleTriggerScrub(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.scheduler.TriggerScrub(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Snapshot      string `json:"snapshot"`
		Dataset       string `json:"dataset"`
		SourceDataset string `json:"source_dataset,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Snapshot == "" || req.Dataset == "" {
		http.Error(w, "Snapshot and dataset are required", http.StatusBadRequest)
		return
	}

	var job *restore.RestoreJob
	var err error

	if req.SourceDataset != "" {
		job, err = s.restoreManager.StartRestoreFromDatasetWithTracking(req.SourceDataset, req.Snapshot, req.Dataset)
	} else {
		job, err = s.restoreManager.StartRestoreWithTracking(req.Snapshot, req.Dataset)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

func (s *Server) handleRestoreJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.restoreManager.ListJobs()

	response := make([]map[string]interface{}, len(jobs))
	for i, job := range jobs {
		jobData := map[string]interface{}{
			"id":         job.ID,
			"snapshot":   job.SnapshotName,
			"dataset":    job.TargetDataset,
			"status":     job.Status,
			"progress":   job.Progress,
			"start_time": job.StartTime.Format("2006-01-02 15:04:05"),
		}

		if job.EndTime != nil {
			jobData["end_time"] = job.EndTime.Format("2006-01-02 15:04:05")
		}

		if job.Error != nil {
			jobData["error"] = job.Error.Error()
		}

		// Add safety warning fields for destructive operations
		if job.RequiresConfirm {
			jobData["requires_confirm"] = true
			jobData["safety_warning"] = job.SafetyWarning
		}

		response[i] = jobData
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleRestoreConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/restore/confirm/")
	jobID := strings.TrimSpace(path)

	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	err := s.restoreManager.ConfirmDestructiveRestore(jobID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to confirm restore: %v", err), http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Destructive restore confirmed for job %s - proceeding with data loss", jobID),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Simple health check endpoint for monitoring systems
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"service":   "zfsrabbit",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (s *Server) handleRemoteDatasets(w http.ResponseWriter, r *http.Request) {
	datasets, err := s.transport.ListAllRemoteDatasets()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list remote datasets: %v", err), http.StatusInternalServerError)
		return
	}

	// Categorize datasets
	response := map[string]interface{}{
		"local_dataset":         s.config.ZFS.Dataset,
		"remote_datasets":       datasets,
		"available_for_restore": []map[string]interface{}{},
		"managed_by_this_instance": map[string]interface{}{
			"dataset":   s.config.SSH.RemoteDataset,
			"snapshots": datasets[s.config.SSH.RemoteDataset],
		},
	}

	// Find datasets that exist remotely but not locally managed
	for dataset, snapshots := range datasets {
		if dataset != s.config.SSH.RemoteDataset && len(snapshots) > 0 {
			response["available_for_restore"] = append(
				response["available_for_restore"].([]map[string]interface{}),
				map[string]interface{}{
					"dataset":         dataset,
					"snapshots":       snapshots,
					"latest_snapshot": snapshots[len(snapshots)-1], // Last snapshot
				})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleRemoteDatasetInfo(w http.ResponseWriter, r *http.Request) {
	// Extract dataset name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/remote/dataset/")
	if path == "" {
		http.Error(w, "Dataset name required", http.StatusBadRequest)
		return
	}

	// URL decode the dataset name (in case it contains special characters)
	dataset := strings.ReplaceAll(path, "%2F", "/")

	info, err := s.transport.GetRemoteDatasetInfo(dataset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get dataset info: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (s *Server) handleMigrationPage(w http.ResponseWriter, r *http.Request) {
	// Serve the migration wizard HTML template
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, "web/templates/migration.html")
}

func (s *Server) handleRetryPendingSends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("Manual retry of pending snapshot sends requested")
	
	if err := s.scheduler.RetryPendingSends(); err != nil {
		http.Error(w, fmt.Sprintf("Retry failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"success": true, "message": "All pending snapshots sent successfully"}`))
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
