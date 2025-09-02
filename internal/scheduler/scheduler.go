package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"zfsrabbit/internal/config"
	"zfsrabbit/internal/transport"
	"zfsrabbit/internal/zfs"
)

type Scheduler struct {
	cron         *cron.Cron
	config       *config.Config
	zfsManager   *zfs.Manager
	transport    *transport.SSHTransport
	alerter      SyncAlerter
	ctx          context.Context
	cancel       context.CancelFunc
	pendingSends []string // Snapshots that failed to send and need retry
	sendMutex    sync.Mutex // Prevents concurrent sends to same backup server
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

	if _, err := s.cron.AddFunc(s.config.Schedule.RetryCron, s.performRetry); err != nil {
		return fmt.Errorf("failed to add retry job: %w", err)
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
	// Use mutex to prevent concurrent sends to same backup server
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	
	log.Println("Starting scheduled snapshot")
	
	// First, try to send any pending snapshots from previous failures
	if len(s.pendingSends) > 0 {
		log.Printf("Attempting to retry %d pending snapshots", len(s.pendingSends))
		s.retryPendingSendsUnsafe() // Don't fail if retry fails, just log
	}
	
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
		
		// Add to pending sends for retry
		s.pendingSends = append(s.pendingSends, snapshotName)
		log.Printf("Added snapshot %s to retry queue (%d pending)", snapshotName, len(s.pendingSends))
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
		return fmt.Errorf("failed to list remote snapshots, aborting sync to prevent data loss: %w", err)
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
	// Check if a send is already in progress
	if !s.sendMutex.TryLock() {
		return fmt.Errorf("snapshot operation already in progress")
	}
	s.sendMutex.Unlock() // Release immediately since performSnapshot will acquire it
	
	go s.performSnapshot()
	return nil
}

func (s *Scheduler) TriggerScrub() error {
	go s.performScrub()
	return nil
}

// performRetry runs on scheduled basis to retry failed snapshot sends
func (s *Scheduler) performRetry() {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	
	if len(s.pendingSends) == 0 {
		return // Nothing to retry
	}
	
	log.Printf("Scheduled retry: attempting to send %d pending snapshots", len(s.pendingSends))
	s.retryPendingSendsUnsafe()
}

// RetryPendingSends attempts to send any snapshots that failed to send previously (thread-safe)
func (s *Scheduler) RetryPendingSends() error {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	
	return s.retryPendingSendsUnsafe()
}

// retryPendingSendsUnsafe does the actual retry work (assumes caller holds sendMutex)
func (s *Scheduler) retryPendingSendsUnsafe() error {
	if len(s.pendingSends) == 0 {
		log.Printf("No pending snapshots to retry")
		return nil
	}
	
	log.Printf("Retrying %d pending snapshot sends", len(s.pendingSends))
	
	// Process pending sends
	var stillPending []string
	for _, snapshotName := range s.pendingSends {
		log.Printf("Retrying send for snapshot: %s", snapshotName)
		
		if err := s.sendSnapshot(snapshotName); err != nil {
			log.Printf("Retry failed for snapshot %s: %v", snapshotName, err)
			stillPending = append(stillPending, snapshotName)
		} else {
			log.Printf("Successfully sent snapshot on retry: %s", snapshotName)
			s.alerter.SendSyncSuccess(snapshotName, s.config.ZFS.Dataset, 0)
		}
	}
	
	// Update pending list with only failed retries
	s.pendingSends = stillPending
	
	if len(s.pendingSends) > 0 {
		log.Printf("%d snapshots still pending after retry", len(s.pendingSends))
		return fmt.Errorf("%d snapshots still failed to send", len(s.pendingSends))
	}
	
	log.Printf("All pending snapshots successfully sent")
	return nil
}

// GetPendingSends returns the list of snapshots that failed to send
func (s *Scheduler) GetPendingSends() []string {
	return s.pendingSends
}
