package state

import (
	"sync"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
)

// NodeChangeEvent represents a node change event
type NodeChangeEvent struct {
	NodeID    string            `json:"node_id"`
	EventType string            `json:"event_type"` // "join", "update", "leave"
	NodeState *models.NodeState `json:"node_state"`
}

// StateManager manages cluster state
type StateManager struct {
	mu           sync.RWMutex
	clusterState *models.ClusterState
	subscribers  map[chan NodeChangeEvent]struct{}
}

// NewStateManager creates a state manager
func NewStateManager(clusterName, localNodeID string) *StateManager {
	return &StateManager{
		clusterState: &models.ClusterState{
			ClusterName: clusterName,
			Nodes:       make(map[string]*models.NodeState),
			LocalNodeID: localNodeID,
			UpdatedAt:   time.Now(),
		},
		subscribers: make(map[chan NodeChangeEvent]struct{}),
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
	defer sm.mu.Unlock()
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
		return true
	}
	// Already exists, perform conflict resolution
	winner := sm.resolveConflict(local, remote)
	// Check if update is actually needed
	if winner == local {
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
	defer sm.mu.Unlock()
	if ns, ok := sm.clusterState.Nodes[nodeID]; ok {
		ns.Status = "inactive"
		sm.clusterState.UpdatedAt = time.Now()
		sm.broadcast(NodeChangeEvent{
			NodeID:    nodeID,
			EventType: "leave",
			NodeState: ns,
		})
	}
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
