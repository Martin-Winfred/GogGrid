package state

import (
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
)

func makeNodeState(nodeID string, version int64, cpu float64, updated time.Time) *models.NodeState {
	return &models.NodeState{
		NodeID:      nodeID,
		IPAddress:   "10.0.0.1",
		Status:      "active",
		CPUUsage:    cpu,
		MemoryUsage: 50.0,
		DiskUsage:   30.0,
		LastSeen:    updated,
		LastUpdated: updated,
		Version:     version,
	}
}

func TestNewStateManager(t *testing.T) {
	sm := NewStateManager("TestCluster", "local1")
	cs := sm.GetClusterState()
	if cs.ClusterName != "TestCluster" {
		t.Errorf("cluster name = %s", cs.ClusterName)
	}
	if cs.LocalNodeID != "local1" {
		t.Errorf("local node = %s", cs.LocalNodeID)
	}
	if len(cs.Nodes) != 0 {
		t.Errorf("node count = %d, want 0", len(cs.Nodes))
	}
}

func TestUpdateLocalNode(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	ns := makeNodeState("local1", 1, 50.0, time.Now())
	sm.UpdateLocalNode(ns)
	cs := sm.GetClusterState()
	if len(cs.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(cs.Nodes))
	}
	if cs.Nodes["local1"].Version != 1 {
		t.Errorf("version = %d", cs.Nodes["local1"].Version)
	}
}

func TestGetClusterStateDeepCopy(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	ns := makeNodeState("local1", 1, 50.0, time.Now())
	sm.UpdateLocalNode(ns)
	cs := sm.GetClusterState()
	// modifying copy should not affect internal state
	cs.Nodes["local1"].CPUUsage = 99.9
	cs2 := sm.GetClusterState()
	if cs2.Nodes["local1"].CPUUsage == 99.9 {
		t.Error("deep copy failed: modifying copy affected internal state")
	}
}

func TestMergeNewNode(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	remote := makeNodeState("remote1", 1, 80.0, time.Now())
	changed := sm.MergeNodeState(remote)
	if !changed {
		t.Error("new node should return changed=true")
	}
	cs := sm.GetClusterState()
	if _, ok := cs.Nodes["remote1"]; !ok {
		t.Error("remote node not added to cluster state")
	}
}

func TestMergeNewerVersion(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	now := time.Now()
	old := makeNodeState("remote1", 1, 50.0, now)
	sm.MergeNodeState(old)
	newer := makeNodeState("remote1", 2, 80.0, now.Add(time.Second))
	changed := sm.MergeNodeState(newer)
	if !changed {
		t.Error("higher version should trigger update")
	}
	cs := sm.GetClusterState()
	if cs.Nodes["remote1"].CPUUsage != 80.0 {
		t.Errorf("CPU = %v", cs.Nodes["remote1"].CPUUsage)
	}
}

func TestMergeOlderVersion(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	now := time.Now()
	new := makeNodeState("remote1", 2, 80.0, now)
	sm.MergeNodeState(new)
	older := makeNodeState("remote1", 1, 50.0, now.Add(-time.Hour))
	changed := sm.MergeNodeState(older)
	if changed {
		t.Error("lower version should not override higher version")
	}
	cs := sm.GetClusterState()
	if cs.Nodes["remote1"].CPUUsage != 80.0 {
		t.Errorf("CPU = %v, want 80.0", cs.Nodes["remote1"].CPUUsage)
	}
}

func TestResolveConflictLWW(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	now := time.Now()
	// insert node with same version
	existing := makeNodeState("remote1", 1, 50.0, now)
	sm.MergeNodeState(existing)
	// send same version but newer timestamp
	newerTime := makeNodeState("remote1", 1, 60.0, now.Add(time.Minute))
	changed := sm.MergeNodeState(newerTime)
	if !changed {
		t.Error("same version with newer timestamp should override")
	}
	cs := sm.GetClusterState()
	if cs.Nodes["remote1"].CPUUsage != 60.0 {
		t.Errorf("CPU = %v, want 60.0", cs.Nodes["remote1"].CPUUsage)
	}
	// send same version but older timestamp - should not overwrite
	olderTime := makeNodeState("remote1", 1, 70.0, now.Add(-time.Minute))
	changed2 := sm.MergeNodeState(olderTime)
	if changed2 {
		t.Error("same version with older timestamp should not override")
	}
}

func TestSubscribeNotify(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	ch := sm.Subscribe()
	defer sm.Unsubscribe(ch)
	ns := makeNodeState("local1", 1, 50.0, time.Now())
	sm.UpdateLocalNode(ns)
	select {
	case event := <-ch:
		if event.EventType != "update" {
			t.Errorf("event type = %s", event.EventType)
		}
		if event.NodeID != "local1" {
			t.Errorf("node ID = %s", event.NodeID)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestUnsubscribe(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	ch := sm.Subscribe()
	sm.Unsubscribe(ch)
	// verify channel is closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestMarkNodeInactive(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	sm.UpdateLocalNode(makeNodeState("local1", 1, 50.0, time.Now()))
	sm.MarkNodeInactive("local1")
	ns, ok := sm.GetNode("local1")
	if !ok {
		t.Fatal("node should exist")
	}
	if ns.Status != "inactive" {
		t.Errorf("status = %s, want inactive", ns.Status)
	}
}

func TestMarkNodeInactiveDeepCopy(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	sm.UpdateLocalNode(makeNodeState("local1", 1, 50.0, time.Now()))
	sm.MarkNodeInactive("local1")

	ns, ok := sm.GetNode("local1")
	if !ok {
		t.Fatal("node should exist")
	}
	// Mutate the returned copy's Status
	ns.Status = "active"
	// Re-fetch and verify internal state is still "inactive"
	ns2, ok := sm.GetNode("local1")
	if !ok {
		t.Fatal("node should exist")
	}
	if ns2.Status != "inactive" {
		t.Errorf("deep copy failed: returned copy mutation affected internal state, got status = %s, want inactive", ns2.Status)
	}
}

func TestRemoveNode(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	sm.UpdateLocalNode(makeNodeState("local1", 1, 50.0, time.Now()))
	sm.RemoveNode("local1")
	_, ok := sm.GetNode("local1")
	if ok {
		t.Error("deleted node should not exist")
	}
}

func TestGetNode(t *testing.T) {
	sm := NewStateManager("Test", "local1")
	sm.UpdateLocalNode(makeNodeState("local1", 1, 50.0, time.Now()))
	ns, ok := sm.GetNode("local1")
	if !ok {
		t.Fatal("node should exist")
	}
	if ns.CPUUsage != 50.0 {
		t.Errorf("CPU = %v", ns.CPUUsage)
	}
	_, ok = sm.GetNode("nonexistent")
	if ok {
		t.Error("non-existent node should return ok=false")
	}
}
