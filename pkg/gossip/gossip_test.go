package gossip

import (
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
)

func TestEncodeDecodeMessage(t *testing.T) {
	original := &GossipMessage{
		Type:      MsgNodeState,
		FromNode:  "node-1",
		Timestamp: time.Now().UnixNano(),
		Payload:   []byte("test-payload"),
	}
	data, err := EncodeMessage(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type expected %d, got %d", original.Type, decoded.Type)
	}
	if decoded.FromNode != original.FromNode {
		t.Errorf("FromNode expected %s, got %s", original.FromNode, decoded.FromNode)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload expected %s, got %s", original.Payload, decoded.Payload)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp expected %d, got %d", original.Timestamp, decoded.Timestamp)
	}
}

func TestNodeStatePayloadRoundtrip(t *testing.T) {
	ns := &models.NodeState{
		NodeID:      "node-1",
		IPAddress:   "192.168.0.1",
		Status:      "active",
		CPUUsage:    75.5,
		LastSeen:    time.Now().Truncate(time.Second),
		LastUpdated: time.Now().Truncate(time.Second),
		Version:     5,
	}

	payload, err := EncodePayload(&NodeStatePayload{State: ns})
	if err != nil {
		t.Fatalf("encode payload failed: %v", err)
	}

	var decoded NodeStatePayload
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}

	if decoded.State == nil {
		t.Fatal("decoded State should not be nil")
	}
	if decoded.State.NodeID != ns.NodeID {
		t.Errorf("NodeID expected %s, got %s", ns.NodeID, decoded.State.NodeID)
	}
	if decoded.State.CPUUsage != ns.CPUUsage {
		t.Errorf("CPUUsage expected %v, got %v", ns.CPUUsage, decoded.State.CPUUsage)
	}
	if decoded.State.Version != ns.Version {
		t.Errorf("Version expected %d, got %d", ns.Version, decoded.State.Version)
	}
	if decoded.State.IPAddress != ns.IPAddress {
		t.Errorf("IPAddress expected %s, got %s", ns.IPAddress, decoded.State.IPAddress)
	}
	if decoded.State.Status != ns.Status {
		t.Errorf("Status expected %s, got %s", ns.Status, decoded.State.Status)
	}
}

func TestClusterSyncPayloadRoundtrip(t *testing.T) {
	nodes := []*models.NodeState{
		{NodeID: "n1", CPUUsage: 50.0},
		{NodeID: "n2", CPUUsage: 80.0},
	}

	payload, err := EncodePayload(&ClusterSyncPayload{Nodes: nodes})
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded ClusterSyncPayload
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(decoded.Nodes) != 2 {
		t.Errorf("node count expected 2, got %d", len(decoded.Nodes))
	}
}

func TestNewGossipMessage(t *testing.T) {
	msg := NewGossipMessage(MsgHeartbeat, "sender-1", []byte{1, 2, 3})
	if msg.Type != MsgHeartbeat {
		t.Errorf("Type expected %d, got %d", MsgHeartbeat, msg.Type)
	}
	if msg.FromNode != "sender-1" {
		t.Errorf("FromNode expected sender-1, got %s", msg.FromNode)
	}
	if msg.Timestamp == 0 {
		t.Error("Timestamp should not be zero")
	}
	if len(msg.Payload) != 3 {
		t.Errorf("Payload length expected 3, got %d", len(msg.Payload))
	}
}

func TestSimpleBroadcast(t *testing.T) {
	b := &simpleBroadcast{msg: []byte("hello")}
	if string(b.Message()) != "hello" {
		t.Error("message mismatch")
	}
	if b.Invalidates(nil) {
		t.Error("should not invalidate other broadcasts")
	}
	// Finished should not panic
	b.Finished()
}

func TestMapToSlice(t *testing.T) {
	nodes := map[string]*models.NodeState{
		"a": {NodeID: "a"},
		"b": {NodeID: "b"},
	}
	result := mapToSlice(nodes)
	if len(result) != 2 {
		t.Errorf("length expected 2, got %d", len(result))
	}
}

func TestMessageTypeConstants(t *testing.T) {
	if MsgNodeState != 0 {
		t.Error("MsgNodeState should be 0")
	}
	if MsgClusterSync != 1 {
		t.Error("MsgClusterSync should be 1")
	}
	if MsgHeartbeat != 2 {
		t.Error("MsgHeartbeat should be 2")
	}
}
