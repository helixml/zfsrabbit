package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"
	"zfsrabbit/internal/config"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type Scheduler struct {
	cron       *cron.Cron
	config     *config.Config
	zfsManager *zfs.Manager
	transport  *transport.SSHTransport
	alerter    SyncAlerter
	ctx        context.Context
	cancel     context.CancelFunc
}

type SyncAlerter interface {
	SendSyncSuccess(snapshot, dataset string, duration time.Duration) error
	SendSyncFailure(snapshot, dataset string, err error) error
}

func New(cfg *config.Config, zfsManager *zfs.Manager, transport *transport.SSHTransport, alerter SyncAlerter) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		cron:       cron.New(),
		config:     cfg,
		zfsManager: zfsManager,
		transport:  transport,
		alerter:    alerter,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (s *Scheduler) Start() error {
	if _, err := s.cron.AddFunc(s.config.Schedule.SnapshotCron, s.performSnapshot); err != nil {
		return fmt.Errorf("failed to add snapshot job: %w", err)
	}

	if _, err := s.cron.AddFunc(s.config.Schedule.ScrubCron, s.performScrub); err != nil {
		return fmt.Errorf("failed to add scrub job: %w", err)
	}

	s.cron.Start()
	log.Println("Scheduler started")
	return nil
}

func (s *Scheduler) Stop() {
	s.cancel()
	s.cron.Stop()
	log.Println("Scheduler stopped")
}

func (s *Scheduler) performSnapshot() {
	log.Println("Starting scheduled snapshot")
	startTime := time.Now()

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	snapshotName := fmt.Sprintf("autosnap_%s", timestamp)

	if err := s.zfsManager.CreateSnapshot(snapshotName); err != nil {
		log.Printf("Failed to create snapshot: %v", err)
		s.alerter.SendSyncFailure(snapshotName, s.config.ZFS.Dataset, err)
		return
	}

	log.Printf("Created snapshot: %s", snapshotName)

	if err := s.sendSnapshot(snapshotName); err != nil {
		log.Printf("Failed to send snapshot: %v", err)
		s.alerter.SendSyncFailure(snapshotName, s.config.ZFS.Dataset, err)
		return
	}

	duration := time.Since(startTime)
	log.Printf("Successfully sent snapshot: %s (took %s)", snapshotName, duration)
	s.alerter.SendSyncSuccess(snapshotName, s.config.ZFS.Dataset, duration)

	if err := s.cleanupOldSnapshots(); err != nil {
		log.Printf("Failed to cleanup old snapshots: %v", err)
	}
}

func (s *Scheduler) sendSnapshot(snapshotName string) error {
	remoteSnapshots, err := s.transport.ListRemoteSnapshots()
	if err != nil {
		log.Printf("Failed to list remote snapshots, performing full send: %v", err)
		return s.sendFullSnapshot(snapshotName)
	}

	if len(remoteSnapshots) == 0 {
		return s.sendFullSnapshot(snapshotName)
	}

	localSnapshots, err := s.zfsManager.ListSnapshots()
	if err != nil {
		return fmt.Errorf("failed to list local snapshots: %w", err)
	}

	var lastCommon string
	for _, remote := range remoteSnapshots {
		for _, local := range localSnapshots {
			if local.Name == remote {
				lastCommon = remote
			}
		}
	}

	if lastCommon == "" {
		return s.sendFullSnapshot(snapshotName)
	}

	return s.sendIncrementalSnapshot(lastCommon, snapshotName)
}

func (s *Scheduler) sendFullSnapshot(snapshotName string) error {
	sendCmd, err := s.zfsManager.SendSnapshot(snapshotName)
	if err != nil {
		return err
	}

	stdout, err := sendCmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := sendCmd.Start(); err != nil {
		return err
	}

	if err := s.transport.SendSnapshot(stdout, false); err != nil {
		sendCmd.Process.Kill()
		return err
	}

	return sendCmd.Wait()
}

func (s *Scheduler) sendIncrementalSnapshot(fromSnapshot, toSnapshot string) error {
	sendCmd, err := s.zfsManager.SendIncremental(fromSnapshot, toSnapshot)
	if err != nil {
		return err
	}

	stdout, err := sendCmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := sendCmd.Start(); err != nil {
		return err
	}

	if err := s.transport.SendSnapshot(stdout, true); err != nil {
		sendCmd.Process.Kill()
		return err
	}

	return sendCmd.Wait()
}

func (s *Scheduler) cleanupOldSnapshots() error {
	snapshots, err := s.zfsManager.ListSnapshots()
	if err != nil {
		return err
	}

	const maxSnapshots = 30
	if len(snapshots) <= maxSnapshots {
		return nil
	}

	toDelete := snapshots[:len(snapshots)-maxSnapshots]
	for _, snapshot := range toDelete {
		if err := s.zfsManager.DestroySnapshot(snapshot.Name); err != nil {
			log.Printf("Failed to delete old snapshot %s: %v", snapshot.Name, err)
		} else {
			log.Printf("Deleted old snapshot: %s", snapshot.Name)
		}
	}

	return nil
}

func (s *Scheduler) performScrub() {
	log.Println("Starting scheduled scrub")

	pools, err := zfs.GetPools()
	if err != nil {
		log.Printf("Failed to get pools: %v", err)
		return
	}

	for _, pool := range pools {
		log.Printf("Starting scrub for pool: %s", pool)
		if err := zfs.ScrubPool(pool); err != nil {
			log.Printf("Failed to start scrub for pool %s: %v", pool, err)
		}
	}
}

func (s *Scheduler) TriggerSnapshot() error {
	go s.performSnapshot()
	return nil
}

func (s *Scheduler) TriggerScrub() error {
	go s.performScrub()
	return nil
}
