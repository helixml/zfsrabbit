package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/monitor"
	"zfsrabbit/internal/restore"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/slack"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type Server struct {
	config         *config.Config
	scheduler      *scheduler.Scheduler
	monitor        *monitor.Monitor
	zfsManager     *zfs.Manager
	restoreManager *restore.RestoreManager
	slackHandler   *slack.CommandHandler
	transport      *transport.SSHTransport
}

func NewServer(cfg *config.Config, sched *scheduler.Scheduler, mon *monitor.Monitor, zfsMgr *zfs.Manager, restoreMgr *restore.RestoreManager, transport *transport.SSHTransport) *Server {
	slackHandler := slack.NewCommandHandler(&cfg.Slack, sched, mon, zfsMgr, restoreMgr, transport)

	return &Server{
		config:         cfg,
		scheduler:      sched,
		monitor:        mon,
		zfsManager:     zfsMgr,
		restoreManager: restoreMgr,
		slackHandler:   slackHandler,
		transport:      transport,
	}
}

func (s *Server) Start() error {
	http.HandleFunc("/", s.basicAuth(s.handleIndex))
	http.HandleFunc("/api/status", s.basicAuth(s.handleStatus))
	http.HandleFunc("/api/snapshots", s.basicAuth(s.handleSnapshots))
	http.HandleFunc("/api/trigger/snapshot", s.basicAuth(s.handleTriggerSnapshot))
	http.HandleFunc("/api/trigger/scrub", s.basicAuth(s.handleTriggerScrub))
	http.HandleFunc("/api/restore", s.basicAuth(s.handleRestore))
	http.HandleFunc("/api/restore/jobs", s.basicAuth(s.handleRestoreJobs))
	http.HandleFunc("/api/remote/datasets", s.basicAuth(s.handleRemoteDatasets))
	http.HandleFunc("/api/remote/dataset/", s.basicAuth(s.handleRemoteDatasetInfo))
	http.HandleFunc("/slack/command", s.slackHandler.HandleSlashCommand)
	http.HandleFunc("/static/", s.handleStatic)

	addr := fmt.Sprintf(":%d", s.config.Server.Port)
	log.Printf("Web server starting on %s", addr)
	return http.ListenAndServe(addr, nil)
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
	html := `<!DOCTYPE html>
<html>
<head>
    <title>ZFSRabbit - ZFS Replication Server</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header { border-bottom: 2px solid #007cba; padding-bottom: 20px; margin-bottom: 30px; }
        .header h1 { color: #007cba; margin: 0; }
        .section { margin-bottom: 30px; padding: 20px; border: 1px solid #ddd; border-radius: 4px; }
        .section h2 { margin-top: 0; color: #333; }
        .button { background: #007cba; color: white; border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; margin-right: 10px; }
        .button:hover { background: #005a87; }
        .button.danger { background: #dc3545; }
        .button.danger:hover { background: #c82333; }
        .status { padding: 10px; border-radius: 4px; margin: 10px 0; }
        .status.online { background: #d4edda; border: 1px solid #c3e6cb; color: #155724; }
        .status.offline { background: #f8d7da; border: 1px solid #f5c6cb; color: #721c24; }
        .status.degraded { background: #fff3cd; border: 1px solid #ffeaa7; color: #856404; }
        .snapshots { max-height: 300px; overflow-y: auto; }
        .snapshot { display: flex; justify-content: space-between; align-items: center; padding: 8px; border-bottom: 1px solid #eee; }
        .logs { max-height: 200px; overflow-y: auto; background: #f8f9fa; padding: 10px; font-family: monospace; font-size: 12px; }
        .dataset { background: #f8f9fa; padding: 10px; margin: 5px 0; border-radius: 4px; border-left: 4px solid #007cba; }
        .dataset.local { border-left-color: #28a745; }
        .dataset.remote { border-left-color: #ffc107; }
        .snapshots-list { max-height: 100px; overflow-y: auto; font-size: 12px; margin-top: 5px; }
        select, input[type="text"] { padding: 8px; margin: 5px; border: 1px solid #ddd; border-radius: 4px; }
        #refreshBtn { position: fixed; top: 20px; right: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🐰 ZFSRabbit</h1>
            <p>ZFS Replication & Monitoring Server</p>
            <button id="refreshBtn" class="button" onclick="location.reload()">Refresh</button>
        </div>

        <div class="section">
            <h2>System Status</h2>
            <div id="systemStatus">Loading...</div>
        </div>

        <div class="section">
            <h2>ZFS Snapshots</h2>
            <button class="button" onclick="triggerSnapshot()">Create Snapshot</button>
            <div id="snapshots">Loading...</div>
        </div>

        <div class="section">
            <h2>ZFS Pools</h2>
            <button class="button" onclick="triggerScrub()">Start Scrub</button>
            <div id="poolStatus">Loading...</div>
        </div>

        <div class="section">
            <h2>Remote Datasets</h2>
            <div id="remoteDatasets">Loading remote datasets...</div>
        </div>

        <div class="section">
            <h2>Restore</h2>
            <select id="restoreSourceDataset">
                <option value="">Select source dataset...</option>
            </select>
            <select id="restoreSnapshot">
                <option value="">Select snapshot...</option>
            </select>
            <input type="text" id="restoreTargetDataset" placeholder="Target dataset">
            <button class="button danger" onclick="restore()">Restore</button>
        </div>
    </div>

    <script>
        async function loadStatus() {
            try {
                const response = await fetch('/api/status');
                const data = await response.json();
                
                document.getElementById('systemStatus').innerHTML = 
                    '<div class="status ' + (data.healthy ? 'online' : 'offline') + '">' +
                    'System: ' + (data.healthy ? 'Healthy' : 'Issues Detected') + '</div>';
                
                if (data.pools) {
                    let poolsHtml = '';
                    for (const [pool, status] of Object.entries(data.pools)) {
                        const statusClass = status.State === 'ONLINE' ? 'online' : 'degraded';
                        poolsHtml += '<div class="status ' + statusClass + '">' + pool + ': ' + status.State + '</div>';
                    }
                    document.getElementById('poolStatus').innerHTML = poolsHtml;
                }
            } catch (error) {
                console.error('Failed to load status:', error);
            }
        }

        async function loadSnapshots() {
            try {
                const response = await fetch('/api/snapshots');
                const snapshots = await response.json();
                
                let html = '<div class="snapshots">';
                snapshots.forEach(snap => {
                    html += '<div class="snapshot">' +
                           '<span>' + snap.name + ' (' + snap.created + ')</span>' +
                           '<span>' + snap.used + '</span>' +
                           '</div>';
                });
                html += '</div>';
                
                document.getElementById('snapshots').innerHTML = html;
            } catch (error) {
                console.error('Failed to load snapshots:', error);
            }
        }

        async function triggerSnapshot() {
            try {
                await fetch('/api/trigger/snapshot', { method: 'POST' });
                alert('Snapshot creation started');
                setTimeout(loadSnapshots, 2000);
            } catch (error) {
                alert('Failed to trigger snapshot: ' + error.message);
            }
        }

        async function triggerScrub() {
            try {
                await fetch('/api/trigger/scrub', { method: 'POST' });
                alert('Scrub started');
            } catch (error) {
                alert('Failed to trigger scrub: ' + error.message);
            }
        }

        async function restore() {
            const snapshot = document.getElementById('restoreSnapshot').value;
            const dataset = document.getElementById('restoreDataset').value;
            
            if (!snapshot || !dataset) {
                alert('Please enter both snapshot name and target dataset');
                return;
            }
            
            if (!confirm('This will overwrite the target dataset. Are you sure?')) {
                return;
            }
            
            try {
                await fetch('/api/restore', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ snapshot, dataset })
                });
                alert('Restore started');
            } catch (error) {
                alert('Failed to start restore: ' + error.message);
            }
        }

        async function loadRemoteDatasets() {
            try {
                const response = await fetch('/api/remote/datasets');
                const data = await response.json();
                
                let html = '';
                
                // Show local dataset
                html += '<div class="dataset local">' +
                       '<strong>📂 Local Dataset (This Instance):</strong> ' + data.local_dataset +
                       '<div class="snapshots-list">';
                
                if (data.managed_by_this_instance.snapshots) {
                    data.managed_by_this_instance.snapshots.forEach(snap => {
                        html += '<div>• ' + snap + '</div>';
                    });
                }
                html += '</div></div>';
                
                // Show available datasets for restore
                if (data.available_for_restore.length > 0) {
                    html += '<div class="dataset remote">' +
                           '<strong>📥 Available for Restore:</strong><br>';
                    
                    data.available_for_restore.forEach(item => {
                        html += '<div style="margin: 10px 0;"><strong>' + item.dataset + '</strong> (' + 
                               item.snapshots.length + ' snapshots, latest: ' + item.latest_snapshot + ')' +
                               '<div class="snapshots-list">';
                        
                        item.snapshots.forEach(snap => {
                            html += '<div>• ' + snap + '</div>';
                        });
                        html += '</div></div>';
                    });
                    html += '</div>';
                }
                
                document.getElementById('remoteDatasets').innerHTML = html;
                
                // Populate restore dropdown
                const sourceSelect = document.getElementById('restoreSourceDataset');
                sourceSelect.innerHTML = '<option value="">Select source dataset...</option>';
                
                data.available_for_restore.forEach(item => {
                    const option = document.createElement('option');
                    option.value = item.dataset;
                    option.textContent = item.dataset + ' (' + item.snapshots.length + ' snapshots)';
                    sourceSelect.appendChild(option);
                });
                
            } catch (error) {
                document.getElementById('remoteDatasets').innerHTML = 'Failed to load remote datasets: ' + error.message;
            }
        }

        async function updateSnapshotList() {
            const sourceDataset = document.getElementById('restoreSourceDataset').value;
            const snapshotSelect = document.getElementById('restoreSnapshot');
            
            snapshotSelect.innerHTML = '<option value="">Select snapshot...</option>';
            
            if (!sourceDataset) return;
            
            try {
                const response = await fetch('/api/remote/dataset/' + encodeURIComponent(sourceDataset));
                const data = await response.json();
                
                data.snapshots.forEach(snapshot => {
                    const option = document.createElement('option');
                    option.value = snapshot;
                    option.textContent = snapshot;
                    snapshotSelect.appendChild(option);
                });
                
            } catch (error) {
                console.error('Failed to load snapshots:', error);
            }
        }

        async function restore() {
            const sourceDataset = document.getElementById('restoreSourceDataset').value;
            const snapshot = document.getElementById('restoreSnapshot').value;
            const targetDataset = document.getElementById('restoreTargetDataset').value;
            
            if (!sourceDataset || !snapshot || !targetDataset) {
                alert('Please select source dataset, snapshot, and enter target dataset');
                return;
            }
            
            if (!confirm('This will overwrite the target dataset. Are you sure?')) {
                return;
            }
            
            try {
                await fetch('/api/restore', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ 
                        snapshot: snapshot, 
                        dataset: targetDataset,
                        source_dataset: sourceDataset
                    })
                });
                alert('Restore started');
            } catch (error) {
                alert('Failed to start restore: ' + error.message);
            }
        }

        // Event listeners
        document.getElementById('restoreSourceDataset').addEventListener('change', updateSnapshotList);

        // Load initial data
        loadStatus();
        loadSnapshots();
        loadRemoteDatasets();
        
        // Refresh every 30 seconds
        setInterval(() => {
            loadStatus();
            loadSnapshots();
            loadRemoteDatasets();
        }, 60000); // Refresh remote datasets less frequently
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
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
		"healthy": healthy,
		"pools":   status["pools"],
		"disks":   status["disks"],
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
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

		response[i] = jobData
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
