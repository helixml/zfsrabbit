package slack

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/monitor"
	"zfsrabbit/internal/zfs"
	"zfsrabbit/internal/restore"
)

type CommandHandler struct {
	config         *config.SlackConfig
	scheduler      *scheduler.Scheduler
	monitor        *monitor.Monitor
	zfsManager     *zfs.Manager
	restoreManager *restore.RestoreManager
}

type SlashCommandRequest struct {
	Token       string `json:"token"`
	TeamID      string `json:"team_id"`
	TeamDomain  string `json:"team_domain"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	Command     string `json:"command"`
	Text        string `json:"text"`
	ResponseURL string `json:"response_url"`
}

type SlashCommandResponse struct {
	ResponseType string            `json:"response_type,omitempty"`
	Text         string            `json:"text,omitempty"`
	Blocks       []SlackBlock      `json:"blocks,omitempty"`
	Attachments  []SlackAttachment `json:"attachments,omitempty"`
}

type SlackAttachment struct {
	Color  string       `json:"color,omitempty"`
	Fields []SlackField `json:"fields,omitempty"`
}

type SlackBlock struct {
	Type   string       `json:"type"`
	Text   *SlackText   `json:"text,omitempty"`
	Fields []SlackField `json:"fields,omitempty"`
}

type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackField struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Short bool   `json:"short,omitempty"`
}

func NewCommandHandler(cfg *config.SlackConfig, sched *scheduler.Scheduler, mon *monitor.Monitor, zfsMgr *zfs.Manager, restoreMgr *restore.RestoreManager) *CommandHandler {
	return &CommandHandler{
		config:         cfg,
		scheduler:      sched,
		monitor:        mon,
		zfsManager:     zfsMgr,
		restoreManager: restoreMgr,
	}
}

func (h *CommandHandler) HandleSlashCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	req := SlashCommandRequest{
		Token:       r.FormValue("token"),
		TeamID:      r.FormValue("team_id"),
		TeamDomain:  r.FormValue("team_domain"),
		ChannelID:   r.FormValue("channel_id"),
		ChannelName: r.FormValue("channel_name"),
		UserID:      r.FormValue("user_id"),
		UserName:    r.FormValue("user_name"),
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		ResponseURL: r.FormValue("response_url"),
	}

	// Verify token
	if req.Token != h.config.SlashToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	response := h.processCommand(req)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *CommandHandler) processCommand(req SlashCommandRequest) SlashCommandResponse {
	args := strings.Fields(strings.TrimSpace(req.Text))
	if len(args) == 0 {
		return h.showHelp()
	}

	command := strings.ToLower(args[0])

	switch command {
	case "status":
		return h.getSystemStatus()
	case "snapshot":
		return h.triggerSnapshot()
	case "scrub":
		return h.triggerScrub()
	case "snapshots":
		return h.listSnapshots()
	case "pools":
		return h.getPoolStatus()
	case "disks":
		return h.getDiskStatus()
	case "restore":
		if len(args) < 3 {
			return SlashCommandResponse{
				ResponseType: "ephemeral",
				Text:         "Usage: restore <snapshot_name> <target_dataset>",
			}
		}
		return h.triggerRestore(args[1], args[2])
	case "jobs":
		return h.getRestoreJobs()
	case "help":
		return h.showHelp()
	default:
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("Unknown command: %s. Type `help` for available commands.", command),
		}
	}
}

func (h *CommandHandler) showHelp() SlashCommandResponse {
	helpText := `*ZFSRabbit Commands:*

• *status* - Show overall system status
• *snapshot* - Create a new snapshot immediately
• *scrub* - Start ZFS pool scrub
• *snapshots* - List recent snapshots
• *pools* - Show ZFS pool status
• *disks* - Show disk health status
• *restore <snapshot> <dataset>* - Restore a snapshot
• *jobs* - Show active restore jobs
• *help* - Show this help message

Example: ` + "`/zfsrabbit status`"

	return SlashCommandResponse{
		ResponseType: "ephemeral",
		Text:         helpText,
	}
}

func (h *CommandHandler) getSystemStatus() SlashCommandResponse {
	status := h.monitor.GetSystemStatus()
	
	blocks := []SlackBlock{
		{
			Type: "header",
			Text: &SlackText{
				Type: "plain_text",
				Text: "🐰 ZFSRabbit System Status",
			},
		},
	}

	// System health summary
	healthy := true
	if pools, ok := status["pools"].(map[string]interface{}); ok {
		for _, pool := range pools {
			if poolData, ok := pool.(map[string]interface{}); ok {
				if state, ok := poolData["State"].(string); ok && state != "ONLINE" {
					healthy = false
					break
				}
			}
		}
	}

	healthEmoji := "✅"
	healthText := "All systems healthy"
	if !healthy {
		healthEmoji = "⚠️"
		healthText = "Issues detected"
	}

	blocks = append(blocks, SlackBlock{
		Type: "section",
		Text: &SlackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("%s *Overall Status:* %s", healthEmoji, healthText),
		},
	})

	return SlashCommandResponse{
		ResponseType: "in_channel",
		Blocks:       blocks,
	}
}

func (h *CommandHandler) triggerSnapshot() SlashCommandResponse {
	if err := h.scheduler.TriggerSnapshot(); err != nil {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("❌ Failed to trigger snapshot: %s", err.Error()),
		}
	}

	return SlashCommandResponse{
		ResponseType: "in_channel",
		Text:         "📸 Snapshot creation started! Check back in a few minutes for completion status.",
	}
}

func (h *CommandHandler) triggerScrub() SlashCommandResponse {
	if err := h.scheduler.TriggerScrub(); err != nil {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("❌ Failed to trigger scrub: %s", err.Error()),
		}
	}

	return SlashCommandResponse{
		ResponseType: "in_channel",
		Text:         "🔍 Pool scrub started! This may take several hours to complete.",
	}
}

func (h *CommandHandler) listSnapshots() SlashCommandResponse {
	snapshots, err := h.zfsManager.ListSnapshots()
	if err != nil {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("❌ Failed to list snapshots: %s", err.Error()),
		}
	}

	if len(snapshots) == 0 {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         "No snapshots found.",
		}
	}

	// Show last 10 snapshots
	limit := 10
	if len(snapshots) < limit {
		limit = len(snapshots)
	}

	text := "*Recent Snapshots:*\n"
	for i := len(snapshots) - limit; i < len(snapshots); i++ {
		snap := snapshots[i]
		text += fmt.Sprintf("• `%s` - %s (%s)\n", 
			snap.Name, 
			snap.Created.Format("Jan 02 15:04"), 
			snap.Used)
	}

	return SlashCommandResponse{
		ResponseType: "ephemeral",
		Text:         text,
	}
}

func (h *CommandHandler) getPoolStatus() SlashCommandResponse {
	pools, err := zfs.GetPools()
	if err != nil {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("❌ Failed to get pools: %s", err.Error()),
		}
	}

	if len(pools) == 0 {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         "No ZFS pools found.",
		}
	}

	fields := []SlackField{}
	for _, pool := range pools {
		status, err := zfs.GetPoolStatus(pool)
		if err != nil {
			continue
		}

		emoji := "✅"
		if status.State != "ONLINE" {
			emoji = "⚠️"
		}

		fields = append(fields, SlackField{
			Type:  "mrkdwn",
			Text:  fmt.Sprintf("*%s %s:*\n%s", emoji, pool, status.State),
			Short: true,
		})
	}

	return SlashCommandResponse{
		ResponseType: "ephemeral",
		Blocks: []SlackBlock{
			{
				Type:   "section",
				Fields: fields,
			},
		},
	}
}

func (h *CommandHandler) getDiskStatus() SlashCommandResponse {
	status := h.monitor.GetSystemStatus()
	
	disks, ok := status["disks"].(map[string]interface{})
	if !ok || len(disks) == 0 {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         "No disk status available.",
		}
	}

	fields := []SlackField{}
	for disk, diskStatus := range disks {
		if ds, ok := diskStatus.(map[string]interface{}); ok {
			healthy, _ := ds["Healthy"].(bool)
			temp, _ := ds["Temperature"].(int)
			
			emoji := "✅"
			if !healthy {
				emoji = "❌"
			} else if temp > 50 {
				emoji = "🔥"
			}
			
			fields = append(fields, SlackField{
				Type:  "mrkdwn",
				Text:  fmt.Sprintf("*%s %s:*\n%s %d°C", emoji, disk, 
					map[bool]string{true: "Healthy", false: "Issues"}[healthy], temp),
				Short: true,
			})
		}
	}

	return SlashCommandResponse{
		ResponseType: "ephemeral",
		Blocks: []SlackBlock{
			{
				Type:   "section",
				Fields: fields,
			},
		},
	}
}

func (h *CommandHandler) triggerRestore(snapshot, dataset string) SlashCommandResponse {
	job, err := h.restoreManager.StartRestoreWithTracking(snapshot, dataset)
	if err != nil {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("❌ Failed to start restore: %s", err.Error()),
		}
	}

	return SlashCommandResponse{
		ResponseType: "in_channel",
		Text:         fmt.Sprintf("🔄 Restore job `%s` started!\nRestoring `%s` to `%s`", job.ID, snapshot, dataset),
	}
}

func (h *CommandHandler) getRestoreJobs() SlashCommandResponse {
	jobs := h.restoreManager.ListJobs()
	
	if len(jobs) == 0 {
		return SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         "No active restore jobs.",
		}
	}

	text := "*Active Restore Jobs:*\n"
	for _, job := range jobs {
		emoji := "🔄"
		switch job.Status {
		case "completed":
			emoji = "✅"
		case "failed":
			emoji = "❌"
		}

		text += fmt.Sprintf("• %s `%s` - %s (%d%%)\n", emoji, job.ID, job.Status, job.Progress)
		if job.Error != nil {
			text += fmt.Sprintf("  Error: %s\n", job.Error.Error())
		}
	}

	return SlashCommandResponse{
		ResponseType: "ephemeral",
		Text:         text,
	}
}