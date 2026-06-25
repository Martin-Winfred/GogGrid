package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVectorClockIncrement(t *testing.T) {
	vc := make(VectorClock)

	vc.Increment("n1")
	if vc["n1"] != 1 {
		t.Errorf("expected vc[\"n1\"]=1, got %d", vc["n1"])
	}

	vc.Increment("n1")
	if vc["n1"] != 2 {
		t.Errorf("expected vc[\"n1\"]=2, got %d", vc["n1"])
	}
}

func TestVectorClockCompare_LocalGreater(t *testing.T) {
	vc1 := VectorClock{"n1": 2}
	vc2 := VectorClock{"n1": 1}

	if vc1.Compare(vc2) != 1 {
		t.Errorf("expected Compare=1 (local greater), got %d", vc1.Compare(vc2))
	}
}

func TestVectorClockCompare_RemoteGreater(t *testing.T) {
	vc1 := VectorClock{"n1": 1}
	vc2 := VectorClock{"n1": 2}

	if vc1.Compare(vc2) != -1 {
		t.Errorf("expected Compare=-1 (remote greater), got %d", vc1.Compare(vc2))
	}
}

func TestVectorClockCompare_Concurrent(t *testing.T) {
	vc1 := VectorClock{"n1": 2}
	vc2 := VectorClock{"n2": 2}

	if vc1.Compare(vc2) != 0 {
		t.Errorf("expected Compare=0 (concurrent), got %d", vc1.Compare(vc2))
	}
}

func TestVectorClockMerge(t *testing.T) {
	vc1 := VectorClock{"n1": 1, "n2": 2}
	vc2 := VectorClock{"n2": 3, "n3": 1}

	vc1.Merge(vc2)

	if vc1["n1"] != 1 {
		t.Errorf("expected vc[\"n1\"]=1, got %d", vc1["n1"])
	}
	if vc1["n2"] != 3 {
		t.Errorf("expected vc[\"n2\"]=3, got %d", vc1["n2"])
	}
	if vc1["n3"] != 1 {
		t.Errorf("expected vc[\"n3\"]=1, got %d", vc1["n3"])
	}
}

func TestNodeStateJSONRoundtrip(t *testing.T) {
	ns := NodeState{
		NodeID:      "node-1",
		IPAddress:   "192.168.1.1",
		Status:      "active",
		SystemType:  "linux",
		CPUUsage:    45.5,
		MemoryUsage: 72.3,
		DiskUsage:   60.0,
		Clock:       VectorClock{"node-1": 5},
	}

	data, err := json.Marshal(ns)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded NodeState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.NodeID != ns.NodeID {
		t.Errorf("NodeID mismatch: got %q, want %q", decoded.NodeID, ns.NodeID)
	}
	if decoded.IPAddress != ns.IPAddress {
		t.Errorf("IPAddress mismatch: got %q, want %q", decoded.IPAddress, ns.IPAddress)
	}
	if decoded.Status != ns.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, ns.Status)
	}
	if decoded.SystemType != ns.SystemType {
		t.Errorf("SystemType mismatch: got %q, want %q", decoded.SystemType, ns.SystemType)
	}
	if decoded.CPUUsage != ns.CPUUsage {
		t.Errorf("CPUUsage mismatch: got %f, want %f", decoded.CPUUsage, ns.CPUUsage)
	}
	if decoded.MemoryUsage != ns.MemoryUsage {
		t.Errorf("MemoryUsage mismatch: got %f, want %f", decoded.MemoryUsage, ns.MemoryUsage)
	}
	if decoded.DiskUsage != ns.DiskUsage {
		t.Errorf("DiskUsage mismatch: got %f, want %f", decoded.DiskUsage, ns.DiskUsage)
	}
	if decoded.Clock["node-1"] != ns.Clock["node-1"] {
		t.Errorf("Clock[\"node-1\"] mismatch: got %d, want %d", decoded.Clock["node-1"], ns.Clock["node-1"])
	}
}

func TestHistoryRecordNewFields(t *testing.T) {
	hr := HistoryRecord{
		NodeID:    "node-1",
		EventType: "node_join",
		Status:    "active",
		Source:    "gossip",
		Timestamp: time.Now(),
		CPUUsage:  42.5,
	}

	data, err := json.Marshal(hr)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded HistoryRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.EventType != "node_join" {
		t.Errorf("EventType mismatch: got %q, want %q", decoded.EventType, "node_join")
	}
	if decoded.Status != "active" {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, "active")
	}
	if decoded.Source != "gossip" {
		t.Errorf("Source mismatch: got %q, want %q", decoded.Source, "gossip")
	}
	if decoded.CPUUsage != 42.5 {
		t.Errorf("CPUUsage mismatch: got %f, want 42.5", decoded.CPUUsage)
	}
}

func TestHistoryRecordEventTypes(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
	}{
		{"metric_update", "metric_update"},
		{"node_join", "node_join"},
		{"node_leave", "node_leave"},
	}

	for _, tt := range tests {
		hr := HistoryRecord{EventType: tt.eventType}
		if hr.EventType != tt.eventType {
			t.Errorf("%s: EventType = %q, want %q", tt.name, hr.EventType, tt.eventType)
		}
	}
}

func TestClusterStateJSONRoundtrip(t *testing.T) {
	cs := ClusterState{
		ClusterName: "test-cluster",
		LocalNodeID: "node-1",
		Nodes: map[string]*NodeState{
			"node-1": {
				NodeID:      "node-1",
				IPAddress:   "10.0.0.1",
				Status:      "active",
				SystemType:  "linux",
				CPUUsage:    30.0,
				MemoryUsage: 50.0,
				Clock:       VectorClock{"node-1": 3},
			},
			"node-2": {
				NodeID:      "node-2",
				IPAddress:   "10.0.0.2",
				Status:      "active",
				SystemType:  "darwin",
				CPUUsage:    20.0,
				MemoryUsage: 40.0,
				Clock:       VectorClock{"node-2": 1},
			},
		},
	}

	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ClusterState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ClusterName != cs.ClusterName {
		t.Errorf("ClusterName mismatch: got %q, want %q", decoded.ClusterName, cs.ClusterName)
	}
	if decoded.LocalNodeID != cs.LocalNodeID {
		t.Errorf("LocalNodeID mismatch: got %q, want %q", decoded.LocalNodeID, cs.LocalNodeID)
	}
	if len(decoded.Nodes) != len(cs.Nodes) {
		t.Errorf("Nodes count mismatch: got %d, want %d", len(decoded.Nodes), len(cs.Nodes))
	}
	for id, node := range cs.Nodes {
		decNode, ok := decoded.Nodes[id]
		if !ok {
			t.Errorf("missing node %q in decoded", id)
			continue
		}
		if decNode.NodeID != node.NodeID {
			t.Errorf("NodeID[%s] mismatch: got %q, want %q", id, decNode.NodeID, node.NodeID)
		}
		if decNode.IPAddress != node.IPAddress {
			t.Errorf("IPAddress[%s] mismatch: got %q, want %q", id, decNode.IPAddress, node.IPAddress)
		}
	}
}
