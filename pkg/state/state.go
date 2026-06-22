package state

import (
	"log/slog"
	"sync"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

// NodeChangeEvent represents a node change event
type NodeChangeEvent struct {
	NodeID    string            `json:"node_id"`
	EventType string            `json:"event_type"` // "join", "update", "leave"
	NodeState *models.NodeState `json:"node_state"`
}

// StateManager manages cluster state
type StateManager struct {
	mu              sync.RWMutex
	clusterState    *models.ClusterState
	subscribers     map[chan NodeChangeEvent]struct{}
	store           *storage.Storage
	historySyncTime time.Time
}

func NewStateManager(clusterName, localNodeID string, store *storage.Storage) *StateManager {
	return &StateManager{
		clusterState: &models.ClusterState{
			ClusterName: clusterName,
			Nodes:       make(map[string]*models.NodeState),
			LocalNodeID: localNodeID,
			UpdatedAt:   time.Now(),
		},
		subscribers: make(map[chan NodeChangeEvent]struct{}),
		store:       store,
	}
}

// GetClusterState returns a deep copy of cluster state (goroutine-safe)
func (sm *StateManager) GetClusterState() *models.ClusterState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	cs := &models.ClusterState{
		ClusterName: sm.clusterState.ClusterName,
		Nodes:       make(map[string]*models.NodeState, len(sm.clusterState.Nodes)),
		LocalNodeID: sm.clusterState.LocalNodeID,
		UpdatedAt:   sm.clusterState.UpdatedAt,
	}
	for k, v := range sm.clusterState.Nodes {
		// Deep copy each node
		copyNode := *v
		cs.Nodes[k] = &copyNode
	}
	return cs
}

// GetNode retrieves a node's state
func (sm *StateManager) GetNode(nodeID string) (*models.NodeState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ns, ok := sm.clusterState.Nodes[nodeID]
	if !ok {
		return nil, false
	}
	copyNode := *ns
	return &copyNode, true
}

// GetHistorySyncTime returns the timestamp of the last successful history sync.
func (sm *StateManager) GetHistorySyncTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.historySyncTime
}

// SetHistorySyncTime updates the history sync timestamp.
func (sm *StateManager) SetHistorySyncTime(t time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.historySyncTime = t
}

// UpdateLocalNode updates local node state
func (sm *StateManager) UpdateLocalNode(ns *models.NodeState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	copyNode := *ns
	sm.clusterState.Nodes[ns.NodeID] = &copyNode
	sm.clusterState.UpdatedAt = time.Now()
	sm.broadcast(NodeChangeEvent{
		NodeID:    ns.NodeID,
		EventType: "update",
		NodeState: &copyNode,
	})
}

// MergeNodeState merges remote node state, returns whether changed
func (sm *StateManager) MergeNodeState(remote *models.NodeState) bool {
	sm.mu.Lock()
	local, exists := sm.clusterState.Nodes[remote.NodeID]
	if !exists {
		// New node joined
		copyRemote := *remote
		sm.clusterState.Nodes[remote.NodeID] = &copyRemote
		sm.clusterState.UpdatedAt = time.Now()
		sm.broadcast(NodeChangeEvent{
			NodeID:    remote.NodeID,
			EventType: "join",
			NodeState: &copyRemote,
		})
		sm.mu.Unlock()
		sm.recordEvent(&copyRemote, "node_join", "gossip")
		return true
	}
	// Already exists, perform conflict resolution
	winner := sm.resolveConflict(local, remote)
	// Check if update is actually needed
	if winner == local {
		sm.mu.Unlock()
		return false
	}
	copyWinner := *winner
	sm.clusterState.Nodes[remote.NodeID] = &copyWinner
	sm.clusterState.UpdatedAt = time.Now()
	sm.broadcast(NodeChangeEvent{
		NodeID:    remote.NodeID,
		EventType: "update",
		NodeState: &copyWinner,
	})
	sm.mu.Unlock()
	sm.recordEvent(&copyWinner, "metric_update", "gossip")
	return true
}

// resolveConflict resolves conflicts: Version comparison → LastWriterWins
func (sm *StateManager) resolveConflict(local, remote *models.NodeState) *models.NodeState {
	// 1. Version comparison (happened-after relationship)
	if remote.Version > local.Version {
		return remote
	}
	if remote.Version < local.Version {
		return local
	}
	// 2. Versions equal, LWW: compare LastUpdated
	if remote.LastUpdated.After(local.LastUpdated) {
		return remote
	}
	return local
}

// MarkNodeInactive marks a node as inactive
func (sm *StateManager) MarkNodeInactive(nodeID string) {
	sm.mu.Lock()
	ns, ok := sm.clusterState.Nodes[nodeID]
	if !ok {
		sm.mu.Unlock()
		return
	}
	copyNode := *ns
	copyNode.Status = "inactive"
	sm.clusterState.Nodes[nodeID] = &copyNode
	sm.clusterState.UpdatedAt = time.Now()
	sm.broadcast(NodeChangeEvent{
		NodeID:    nodeID,
		EventType: "leave",
		NodeState: &copyNode,
	})
	sm.mu.Unlock()
	sm.recordEvent(&copyNode, "node_leave", "gossip")
}

// RemoveNode removes a node from cluster state
func (sm *StateManager) RemoveNode(nodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if ns, ok := sm.clusterState.Nodes[nodeID]; ok {
		delete(sm.clusterState.Nodes, nodeID)
		sm.clusterState.UpdatedAt = time.Now()
		sm.broadcast(NodeChangeEvent{
			NodeID:    nodeID,
			EventType: "leave",
			NodeState: ns,
		})
	}
}

// Subscribe subscribes to node change events, returns buffered channel
func (sm *StateManager) Subscribe() chan NodeChangeEvent {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	ch := make(chan NodeChangeEvent, 64)
	sm.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe unsubscribes and closes the channel
func (sm *StateManager) Unsubscribe(ch chan NodeChangeEvent) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.subscribers, ch)
	close(ch)
}

// broadcast sends event to all subscribers (non-blocking, drops if slow)
func (sm *StateManager) broadcast(event NodeChangeEvent) {
	for ch := range sm.subscribers {
		sm.sendEvent(ch, event)
	}
}

// sendEvent sends to a subscriber channel, recovering from panic if the channel
// was closed concurrently by Unsubscribe. Currently safe due to mutex ordering,
// but this guards against future lock-scope reductions.
func (sm *StateManager) sendEvent(ch chan NodeChangeEvent, event NodeChangeEvent) {
	defer func() { recover() }()
	select {
	case ch <- event:
	default:
	}
}

// recordEvent writes a HistoryRecord for a node state change.
func (sm *StateManager) recordEvent(ns *models.NodeState, eventType string, source string) {
	if sm.store == nil {
		return
	}
	hr := &models.HistoryRecord{
		NodeID:       ns.NodeID,
		Version:      ns.Version,
		EventType:    eventType,
		Source:       source,
		Timestamp:    ns.LastUpdated,
		CPUUsage:     ns.CPUUsage,
		MemoryUsage:  ns.MemoryUsage,
		DiskUsage:    ns.DiskUsage,
		NetInterface: ns.NetInterface,
		SystemLoad:   ns.SystemLoad,
		Status:       ns.Status,
	}
	if err := sm.store.SaveHistoryRecord(hr); err != nil {
		slog.Warn("failed to record history event", "node", ns.NodeID, "type", eventType, "error", err)
	}
}
