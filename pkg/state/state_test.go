package state

import (
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

func makeNodeState(nodeID string, clock models.VectorClock, cpu float64, updated time.Time) *models.NodeState {
	return &models.NodeState{
		NodeID:      nodeID,
		IPAddress:   "10.0.0.1",
		Status:      "active",
		CPUUsage:    cpu,
		MemoryUsage: 50.0,
		DiskUsage:   30.0,
		LastSeen:    updated,
		LastUpdated: updated,
		Clock:       clock,
	}
}

func TestNewStateManager(t *testing.T) {
	sm := NewStateManager("TestCluster", "local1", nil)
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
	sm := NewStateManager("Test", "local1", nil)
	ns := makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now())
	sm.UpdateLocalNode(ns)
	cs := sm.GetClusterState()
	if len(cs.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(cs.Nodes))
	}
	if cs.Nodes["local1"].Clock["local1"] != 1 {
		t.Errorf("clock entry = %d", cs.Nodes["local1"].Clock["local1"])
	}
}

func TestGetClusterStateDeepCopy(t *testing.T) {
	sm := NewStateManager("Test", "local1", nil)
	ns := makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now())
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
	sm := NewStateManager("Test", "local1", nil)
	remote := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 80.0, time.Now())
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
	sm := NewStateManager("Test", "local1", nil)
	now := time.Now()
	old := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 50.0, now)
	sm.MergeNodeState(old)
	newer := makeNodeState("remote1", models.VectorClock{"remote1": 2}, 80.0, now.Add(time.Second))
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
	sm := NewStateManager("Test", "local1", nil)
	now := time.Now()
	new := makeNodeState("remote1", models.VectorClock{"remote1": 2}, 80.0, now)
	sm.MergeNodeState(new)
	older := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 50.0, now.Add(-time.Hour))
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
	sm := NewStateManager("Test", "local1", nil)
	now := time.Now()
	// insert node with same version
	existing := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 50.0, now)
	sm.MergeNodeState(existing)
	// send same version but newer timestamp
	newerTime := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 60.0, now.Add(time.Minute))
	changed := sm.MergeNodeState(newerTime)
	if !changed {
		t.Error("same version with newer timestamp should override")
	}
	cs := sm.GetClusterState()
	if cs.Nodes["remote1"].CPUUsage != 60.0 {
		t.Errorf("CPU = %v, want 60.0", cs.Nodes["remote1"].CPUUsage)
	}
	// send same version but older timestamp - should not overwrite
	olderTime := makeNodeState("remote1", models.VectorClock{"remote1": 1}, 70.0, now.Add(-time.Minute))
	changed2 := sm.MergeNodeState(olderTime)
	if changed2 {
		t.Error("same version with older timestamp should not override")
	}
}

func TestSubscribeNotify(t *testing.T) {
	sm := NewStateManager("Test", "local1", nil)
	ch := sm.Subscribe()
	defer sm.Unsubscribe(ch)
	ns := makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now())
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
	sm := NewStateManager("Test", "local1", nil)
	ch := sm.Subscribe()
	sm.Unsubscribe(ch)
	// verify channel is closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestMarkNodeInactive(t *testing.T) {
	sm := NewStateManager("Test", "local1", nil)
	sm.UpdateLocalNode(makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now()))
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
	sm := NewStateManager("Test", "local1", nil)
	sm.UpdateLocalNode(makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now()))
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
	sm := NewStateManager("Test", "local1", nil)
	sm.UpdateLocalNode(makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now()))
	sm.RemoveNode("local1")
	_, ok := sm.GetNode("local1")
	if ok {
		t.Error("deleted node should not exist")
	}
}

func TestGetNode(t *testing.T) {
	sm := NewStateManager("Test", "local1", nil)
	sm.UpdateLocalNode(makeNodeState("local1", models.VectorClock{"local1": 1}, 50.0, time.Now()))
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

func TestMergeNodeStateRecordsJoinEvent(t *testing.T) {
	s, _ := storage.New(":memory:")
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	remote := makeNodeState("new-node", models.VectorClock{"new-node": 1}, 30.0, time.Now())
	sm.MergeNodeState(remote)

	records, err := s.GetNodeHistory("new-node", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("history count = %d, want 1", len(records))
	}
	if records[0].EventType != "node_join" {
		t.Errorf("EventType = %q, want node_join", records[0].EventType)
	}
}

func TestMergeNodeStateRecordsMetricEvent(t *testing.T) {
	s, _ := storage.New(":memory:")
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	now := time.Now()
	sm.MergeNodeState(makeNodeState("remote", models.VectorClock{"remote": 1}, 30.0, now))
	sm.MergeNodeState(makeNodeState("remote", models.VectorClock{"remote": 2}, 50.0, now.Add(time.Second)))

	records, err := s.GetNodeHistory("remote", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("history count = %d, want 2", len(records))
	}
	metricCount := 0
	for _, r := range records {
		if r.EventType == "metric_update" {
			metricCount++
		}
	}
	if metricCount != 1 {
		t.Errorf("metric_update count = %d, want 1", metricCount)
	}
}

func TestMarkNodeInactiveRecordsLeaveEvent(t *testing.T) {
	s, _ := storage.New(":memory:")
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	sm.UpdateLocalNode(makeNodeState("local", models.VectorClock{"local": 1}, 30.0, time.Now()))
	sm.MarkNodeInactive("local")

	records, err := s.GetNodeHistory("local", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	found := false
	for _, r := range records {
		if r.EventType == "node_leave" && r.Status == "inactive" {
			found = true
		}
	}
	if !found {
		t.Error("node_leave event not recorded")
	}
}

func TestMergeNodeStateDuplicateVersionIgnored(t *testing.T) {
	s, _ := storage.New(":memory:")
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	now := time.Now()
	remote := makeNodeState("dup-node", models.VectorClock{"dup-node": 1}, 30.0, now)
	sm.MergeNodeState(remote)
	sm.MergeNodeState(remote)

	records, err := s.GetNodeHistory("dup-node", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("history count = %d, want 1 (duplicate should be skipped)", len(records))
	}
}

func TestMergeNodeStateDoesNotBlockReads(t *testing.T) {
	s, err := storage.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	done := make(chan struct{})

	// Writer goroutine: continuously merge node state to provoke I/O under load
	go func() {
		defer close(done)
		for i := int64(0); i < 200; i++ {
			remote := makeNodeState("writer-node", models.VectorClock{"writer-node": i}, float64(i), time.Now())
			sm.MergeNodeState(remote)
		}
	}()

	// Reader: repeatedly call GetClusterState, verify it completes within 50ms
	for i := 0; i < 60; i++ {
		resultCh := make(chan struct{}, 1)
		go func() {
			sm.GetClusterState()
			resultCh <- struct{}{}
		}()
		select {
		case <-resultCh:
			// read completed in time
		case <-time.After(50 * time.Millisecond):
			t.Fatal("GetClusterState blocked for more than 50ms during MergeNodeState writes")
		}
	}
	<-done
}

func TestGetSetHistorySyncTime(t *testing.T) {
	s, _ := storage.New(":memory:")
	defer s.Close()
	sm := NewStateManager("test", "local", s)

	initial := sm.GetHistorySyncTime()
	if !initial.IsZero() {
		t.Errorf("initial sync time should be zero, got %v", initial)
	}

	now := time.Now()
	sm.SetHistorySyncTime(now)
	got := sm.GetHistorySyncTime()
	if !got.Equal(now) {
		t.Errorf("sync time = %v, want %v", got, now)
	}
}
