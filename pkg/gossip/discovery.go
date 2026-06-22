package gossip

import (
	"log/slog"
	"sync"
	"time"
)

// Discovery defines the interface for node auto-discovery mechanisms.
type Discovery interface {
	Start(gm *GossipManager) error
	Stop() error
}

// discoveryBase provides shared deduplication and cluster-name filtering logic
// for discovery implementations (UDP broadcast, mDNS, etc.).
type discoveryBase struct {
	mu          sync.Mutex
	seenAddrs   map[string]time.Time
	cooldown    time.Duration
	clusterName string
}

func newDiscoveryBase(clusterName string) *discoveryBase {
	return &discoveryBase{
		seenAddrs:   make(map[string]time.Time),
		cooldown:    30 * time.Second,
		clusterName: clusterName,
	}
}

// isNew checks if an address has not been seen or if the cooldown has expired.
// Returns true and records the current timestamp if the address is new or the
// previous record has expired. Thread-safe.
func (db *discoveryBase) isNew(addr string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now()
	// Clean up stale entries older than 5x cooldown to prevent unbounded growth
	// of the seenAddrs map over long-running processes.
	for a, t := range db.seenAddrs {
		if now.Sub(t) > 5*db.cooldown {
			delete(db.seenAddrs, a)
		}
	}

	lastSeen, exists := db.seenAddrs[addr]
	if !exists || now.Sub(lastSeen) > db.cooldown {
		db.seenAddrs[addr] = now
		return true
	}
	return false
}

// tryJoin attempts to join a discovered peer if the cluster name matches and
// the address has not been seen recently (within the cooldown period).
// The cluster name check prevents cross-cluster joins.
func (db *discoveryBase) tryJoin(gm *GossipManager, addr string, remoteCluster string) {
	if remoteCluster != db.clusterName {
		slog.Debug("discovery: cluster name mismatch, skipping", "remote", remoteCluster, "local", db.clusterName)
		return
	}
	if !db.isNew(addr) {
		return
	}
	slog.Info("discovery: joining discovered peer", "addr", addr)
	n, err := gm.list.Join([]string{addr})
	if err != nil {
		slog.Warn("discovery: join failed", "addr", addr, "error", err)
	} else if n > 0 {
		slog.Info("discovery: successfully joined", "addr", addr, "count", n)
	}
}
