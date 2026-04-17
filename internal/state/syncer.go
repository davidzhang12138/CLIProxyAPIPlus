package state

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

// SyncerConfig holds configuration for the state syncer.
type SyncerConfig struct {
	FlushInterval time.Duration
}

// Syncer manages loading state on startup and periodic flushing to a StateStore.
type Syncer struct {
	store  StateStore
	cfg    SyncerConfig
	stopCh chan struct{}
	doneCh chan struct{}

	// Adapter functions — set by the caller to bridge in-memory managers.
	ExportCooldowns     func() []CooldownEntry
	ImportCooldowns     func([]CooldownEntry)
	ExportMetrics       func() []TokenMetricsEntry
	ImportMetrics       func([]TokenMetricsEntry)
	ExportUsage         func() []byte   // full usage snapshot as JSON
	ImportUsage         func([]byte)    // restore from JSON snapshot
	ExportAuthCooldowns func() []byte   // auth cooldown state as JSON
	ImportAuthCooldowns func([]byte)    // restore auth cooldown state
}

// NewSyncer creates a new state syncer.
func NewSyncer(store StateStore, cfg SyncerConfig) *Syncer {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 30 * time.Second
	}
	return &Syncer{
		store:  store,
		cfg:    cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// LoadState restores persisted state into the in-memory managers.
// Errors are logged as warnings but never block startup.
func (s *Syncer) LoadState(ctx context.Context) {
	if s.ImportCooldowns != nil {
		entries, err := s.store.LoadCooldowns(ctx)
		if err != nil {
			log.Warnf("state syncer: failed to load cooldowns: %v", err)
		} else if len(entries) > 0 {
			s.ImportCooldowns(entries)
			log.Infof("state syncer: restored %d cooldown entries", len(entries))
		}
	}

	if s.ImportMetrics != nil {
		entries, err := s.store.LoadTokenMetrics(ctx)
		if err != nil {
			log.Warnf("state syncer: failed to load token metrics: %v", err)
		} else if len(entries) > 0 {
			s.ImportMetrics(entries)
			log.Infof("state syncer: restored %d token metrics entries", len(entries))
		}
	}

	if s.ImportUsage != nil {
		data, err := s.store.LoadUsageSnapshot(ctx)
		if err != nil {
			log.Warnf("state syncer: failed to load usage snapshot: %v", err)
		} else if len(data) > 0 {
			s.ImportUsage(data)
			log.Infof("state syncer: restored usage snapshot (%d bytes)", len(data))
		}
	}

	if s.ImportAuthCooldowns != nil {
		data, err := s.store.LoadAuthCooldowns(ctx)
		if err != nil {
			log.Warnf("state syncer: failed to load auth cooldowns: %v", err)
		} else if len(data) > 0 {
			s.ImportAuthCooldowns(data)
			log.Infof("state syncer: restored auth cooldown state (%d bytes)", len(data))
		}
	}
}

// Store returns the underlying StateStore.
func (s *Syncer) Store() StateStore { return s.store }

// Start begins the periodic flush goroutine.
func (s *Syncer) Start() {
	go s.run()
	log.Infof("state syncer: started with flush interval %s", s.cfg.FlushInterval)
}

func (s *Syncer) run() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.stopCh:
			s.flush() // final flush before exit
			return
		}
	}
}

// Stop signals the syncer to perform a final flush and stop.
func (s *Syncer) Stop() {
	close(s.stopCh)
	<-s.doneCh
	log.Info("state syncer: stopped")
}

func (s *Syncer) flush() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if s.ExportCooldowns != nil {
		entries := s.ExportCooldowns()
		if err := s.store.SaveCooldowns(ctx, entries); err != nil {
			log.Warnf("state syncer: flush cooldowns failed: %v", err)
		}
	}

	if s.ExportMetrics != nil {
		entries := s.ExportMetrics()
		if err := s.store.SaveTokenMetrics(ctx, entries); err != nil {
			log.Warnf("state syncer: flush token metrics failed: %v", err)
		}
	}

	if s.ExportUsage != nil {
		data := s.ExportUsage()
		if err := s.store.SaveUsageSnapshot(ctx, data); err != nil {
			log.Warnf("state syncer: flush usage snapshot failed: %v", err)
		}
	}

	if s.ExportAuthCooldowns != nil {
		data := s.ExportAuthCooldowns()
		if err := s.store.SaveAuthCooldowns(ctx, data); err != nil {
			log.Warnf("state syncer: flush auth cooldowns failed: %v", err)
		}
	}
}
