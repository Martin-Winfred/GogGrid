package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/api"
	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/Martin-Winfred/GogGrid/pkg/gossip"
	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/monitor"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

func main() {
	// Register CLI flags
	configPath := flag.String("config", "", "config file path")
	clusterName := flag.String("cluster-name", "", "cluster name")
	bindAddr := flag.String("bind", "", "Gossip bind address (ip:port)")
	apiBind := flag.String("api-bind", "", "API bind address (ip:port)")
	apiToken := flag.String("api-token", "", "API authentication token")
	seeds := flag.String("seeds", "", "comma-separated seed node addresses")
	discoveryEnabled := flag.String("discovery-enabled", "", "enable auto-discovery (true/false)")
	discoveryType := flag.String("discovery-type", "", "discovery protocol (udp, mdns)")
	discoveryPort := flag.String("discovery-port", "", "discovery port")
	discoveryInterval := flag.String("discovery-interval", "", "discovery interval (e.g. 3s, 5s)")
	flag.Parse()

	// 1. Load config (defaults → auto goggrid.yaml → env → CLI)
	cfg := config.DefaultConfig()
	if *configPath == "" {
		if _, err := os.Stat("goggrid.yaml"); err == nil {
			if err := config.LoadConfigFile(cfg, "goggrid.yaml"); err != nil {
				fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := config.GenerateDefault("goggrid.yaml"); err != nil {
				log.Printf("WARNING: failed to generate default config: %v", err)
			}
		}
	} else {
		if err := config.LoadConfigFile(cfg, *configPath); err != nil {
			fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
			os.Exit(1)
		}
	}
	config.ApplyEnv(cfg)
	config.ParseFlags(cfg, *clusterName, *bindAddr, *apiBind, *apiToken, *seeds, *discoveryEnabled, *discoveryType, *discoveryPort, *discoveryInterval)

	// 2. Initialize structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 3. Generate node ID (use hostname)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	nodeID := hostname

	slog.Info("GogGrid starting",
		"node_id", nodeID,
		"cluster", cfg.Cluster.Name,
		"gossip_port", cfg.Cluster.BindPort,
		"api_port", cfg.API.Port,
	)

	// 4. Initialize storage layer
	store, err := storage.New(cfg.Storage.DBPath)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}
	slog.Info("storage init complete", "db_path", cfg.Storage.DBPath)

	// 5. Create state manager
	stateMgr := state.NewStateManager(cfg.Cluster.Name, nodeID, store)
	slog.Info("state manager init complete")

	// 6. Start Gossip communication layer
	gossipMgr, err := gossip.New(cfg, stateMgr, store)
	if err != nil {
		slog.Error("gossip init failed", "error", err)
		store.Close()
		os.Exit(1)
	}
	if err := gossipMgr.Start(); err != nil {
		slog.Error("gossip start failed", "error", err)
		gossipMgr.Stop()
		store.Close()
		os.Exit(1)
	}
	slog.Info("gossip communication layer started", "members", gossipMgr.NumMembers())

	// Trigger history backfill when joining a cluster with existing members
	if gossipMgr.NumMembers() > 1 {
		go func() {
			slog.Info("starting history sync from existing peer")
			if err := gossipMgr.SyncHistoryOnJoin(); err != nil {
				slog.Warn("history sync failed", "error", err)
			}
		}()
	}

	// 7. Start API service
	var apiSrv *api.APIServer
	if *cfg.API.Enabled {
		apiSrv = api.New(cfg, stateMgr, store)
		go func() {
			if err := apiSrv.Start(); err != nil {
				slog.Error("API service error", "error", err)
			}
		}()
		slog.Info("API service started", "addr", cfg.API.BindAddr, "port", cfg.API.Port)
	}

	// 8. Start monitoring loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background CPU sampler so GetHostMonitor() does not block
	monitor.StartCPUSampler(ctx, cfg.Monitor.Interval)

	go monitorLoop(ctx, cfg, nodeID, stateMgr, gossipMgr, store)

	// 9. Start periodic cleanup loop
	go cleanupLoop(ctx, cfg, store)

	slog.Info("GogGrid ready")

	// 10. Wait for exit signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("signal received, starting graceful shutdown", "signal", sig.String())

	// Clean up in LIFO order
	cancel() // stop monitorLoop + cleanupLoop

	if apiSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := apiSrv.Stop(ctx); err != nil {
			slog.Warn("API shutdown failed", "error", err)
		}
		slog.Info("API service stopped")
	}

	if err := gossipMgr.Stop(); err != nil {
		slog.Warn("gossip shutdown failed", "error", err)
	}
	slog.Info("gossip stopped")

	if err := store.Close(); err != nil {
		slog.Warn("storage close failed", "error", err)
	}
	slog.Info("storage closed")

	slog.Info("GogGrid stopped")
}

// monitorLoop periodically collects metrics → updates state → broadcasts → persists
func monitorLoop(ctx context.Context, cfg *config.Config, nodeID string,
	stateMgr *state.StateManager, gossipMgr *gossip.GossipManager, store *storage.Storage) {

	ticker := time.NewTicker(cfg.Monitor.Interval)
	defer ticker.Stop()

	// Execute immediately on first run
	collectAndPublish(nodeID, stateMgr, gossipMgr, store)

	for {
		select {
		case <-ctx.Done():
			slog.Info("monitor loop stopped")
			return
		case <-ticker.C:
			collectAndPublish(nodeID, stateMgr, gossipMgr, store)
		}
	}
}

// collectAndPublish collects metrics once and publishes
func collectAndPublish(nodeID string, stateMgr *state.StateManager,
	gossipMgr *gossip.GossipManager, store *storage.Storage) {

	hm, err := monitor.GetHostMonitor()
	if err != nil {
		slog.Warn("metrics collection failed", "error", err)
		return
	}

	ns := hm.ToNodeState(nodeID)

	// Recover and increment Version: memory → SQLite → default 1
	if existing, exists := stateMgr.GetNode(nodeID); exists {
		ns.Version = existing.Version + 1
	} else if persisted, err := store.GetNodeState(nodeID); err == nil {
		ns.Version = persisted.Version + 1
	} else {
		ns.Version = 1
	}
	ns.LastUpdated = time.Now()
	ns.LastSeen = time.Now()
	ns.Status = "active"

	// Update local state (triggers subscriber notification)
	stateMgr.UpdateLocalNode(ns)

	// Broadcast to cluster
	if err := gossipMgr.BroadcastLocalState(ns); err != nil {
		slog.Warn("state broadcast failed", "error", err)
	}

	// Persist current state
	if err := store.SaveNodeState(ns); err != nil {
		slog.Warn("state persist failed", "error", err)
	}

	// Save history record with event metadata for dedup and audit
	hr := &models.HistoryRecord{
		NodeID:       nodeID,
		Version:      ns.Version,
		EventType:    "metric_update",
		Source:       "local",
		Timestamp:    time.Now(),
		CPUUsage:     ns.CPUUsage,
		MemoryUsage:  ns.MemoryUsage,
		DiskUsage:    ns.DiskUsage,
		NetInterface: ns.NetInterface,
		SystemLoad:   ns.SystemLoad,
	}
	if err := store.SaveHistoryRecord(hr); err != nil {
		slog.Warn("history save failed", "error", err)
	}
}

// cleanupLoop periodically cleans expired history data
func cleanupLoop(ctx context.Context, cfg *config.Config, store *storage.Storage) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("cleanup loop stopped")
			return
		case <-ticker.C:
			n, err := store.CleanOldRecords(cfg.Storage.Retention)
			if err != nil {
				slog.Warn("expired data cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("cleaned expired history data", "count", n)
			}
		}
	}
}
