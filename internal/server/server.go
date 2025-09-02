package server

import (
	"context"
	"log"

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
	log.Printf("Login with username 'admin' and password from environment variable %s", s.config.Server.AdminPassEnv)

	return s.webServer.Start()
}

func (s *Server) Stop() {
	log.Println("Stopping ZFSRabbit server")

	s.scheduler.Stop()
	s.monitor.Stop()
	s.transport.Close()

	s.cancel()
}
