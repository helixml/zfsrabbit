package server

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"zfsrabbit/internal/alert"
	"zfsrabbit/internal/config"
	"zfsrabbit/internal/monitor"
	"zfsrabbit/internal/restore"
	"zfsrabbit/internal/scheduler"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/web"
	"zfsrabbit/internal/zfs"
)

type Server struct {
	config         *config.Config
	zfsManager     *zfs.Manager
	transport      *transport.SSHTransport
	scheduler      *scheduler.Scheduler
	monitor        *monitor.Monitor
	multiAlerter   *alert.MultiAlerter
	webServer      *web.Server
	restoreManager *restore.RestoreManager
	ctx            context.Context
	cancel         context.CancelFunc
}

func New(cfg *config.Config) (*Server, error) {
	// Check system dependencies before starting
	if err := checkSystemDependencies(); err != nil {
		return nil, fmt.Errorf("system dependency check failed: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	zfsManager := zfs.New(cfg.ZFS.Dataset, cfg.ZFS.SendCompression, cfg.ZFS.Recursive)

	transport := transport.NewSSHTransport(&cfg.SSH)

	multiAlerter := alert.NewMultiAlerter(&cfg.Email, &cfg.Slack)

	monitor := monitor.New(cfg, multiAlerter)

	scheduler := scheduler.New(cfg, zfsManager, transport, multiAlerter)

	restoreManager := restore.New(transport, zfsManager)

	webServer := web.NewServer(cfg, scheduler, monitor, zfsManager, restoreManager, transport)

	return &Server{
		config:         cfg,
		zfsManager:     zfsManager,
		transport:      transport,
		scheduler:      scheduler,
		monitor:        monitor,
		multiAlerter:   multiAlerter,
		webServer:      webServer,
		restoreManager: restoreManager,
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

func (s *Server) Start() error {
	log.Println("Starting ZFSRabbit server")

	if err := s.scheduler.Start(); err != nil {
		return err
	}

	go s.monitor.Start()

	log.Printf("ZFSRabbit started - Web interface available at http://localhost:%d", s.config.Server.Port)
	if s.config.GetAdminPassword() == "" {
		log.Printf("WARNING: Admin password not set in environment variable %s", s.config.Server.AdminPassEnv)
	} else {
		log.Printf("Admin authentication enabled (password from %s)", s.config.Server.AdminPassEnv)
	}

	return s.webServer.Start()
}

func (s *Server) Stop() {
	log.Println("Stopping ZFSRabbit server")

	// Create context with timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop components in reverse order
	s.scheduler.Stop()
	s.monitor.Stop()

	// Gracefully shutdown web server
	if err := s.webServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Web server shutdown error: %v", err)
	}

	s.transport.Close()
	s.cancel()
}

func checkSystemDependencies() error {
	requiredCommands := []string{"zfs", "zpool", "mbuffer", "pv", "nvme", "smartctl"}

	for _, cmd := range requiredCommands {
		if _, err := exec.LookPath(cmd); err != nil {
			return fmt.Errorf("required command not found: %s (please install %s)", cmd, getInstallHint(cmd))
		}
	}

	log.Printf("All system dependencies verified: %v", requiredCommands)
	return nil
}

func getInstallHint(cmd string) string {
	hints := map[string]string{
		"zfs":      "zfsutils-linux package",
		"zpool":    "zfsutils-linux package",
		"mbuffer":  "mbuffer package",
		"pv":       "pv package (pipe viewer for progress tracking)",
		"nvme":     "nvme-cli package",
		"smartctl": "smartmontools package",
	}

	if hint, exists := hints[cmd]; exists {
		return hint
	}
	return "appropriate package"
}
