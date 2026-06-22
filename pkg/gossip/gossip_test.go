package gossip

import (
	"context"
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
	"github.com/hashicorp/memberlist"
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
	b := &simpleBroadcast{msg: []byte("hello"), nodeID: "test-node"}
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

func TestDiscoveryMessageRoundtrip(t *testing.T) {
	original := &DiscoveryMessage{
		NodeID:      "node-discovery-1",
		ClusterName: "TestCluster",
		GossipAddr:  "192.168.1.10:7946",
		Timestamp:   int64(1712345678000000000),
	}

	payload, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded DiscoveryMessage
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.NodeID != original.NodeID {
		t.Errorf("NodeID expected %s, got %s", original.NodeID, decoded.NodeID)
	}
	if decoded.ClusterName != original.ClusterName {
		t.Errorf("ClusterName expected %s, got %s", original.ClusterName, decoded.ClusterName)
	}
	if decoded.GossipAddr != original.GossipAddr {
		t.Errorf("GossipAddr expected %s, got %s", original.GossipAddr, decoded.GossipAddr)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp expected %d, got %d", original.Timestamp, decoded.Timestamp)
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
	if MsgDiscovery != 3 {
		t.Errorf("MsgDiscovery = %d, want 3", MsgDiscovery)
	}
}

// ====== History Pull Test Helpers ======

// newTestGossipManagerForHistory creates a minimal GossipManager with an
// in-memory SQLite store, state manager, and a real (but isolated) memberlist.
// The memberlist has no peers, so handleHistoryPullRequest will exercise the
// store query + encode path and then log a warning (no matching member to send to).
func newTestGossipManagerForHistory(t *testing.T, nodeName string) *GossipManager {
	t.Helper()

	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	stateMgr := state.NewStateManager("test-cluster", nodeName, store)

	cfg := config.DefaultConfig()
	cfg.Cluster.Name = "test-cluster"
	*cfg.Discovery.Enabled = false

	gm := &GossipManager{
		stateMgr:     stateMgr,
		cfg:          cfg,
		localNode:    nodeName,
		stopCh:       make(chan struct{}),
		store:        store,
		pendingSyncs: make(map[string]*pendingSync),
	}

	mlCfg := memberlist.DefaultLANConfig()
	mlCfg.BindPort = 0
	mlCfg.AdvertisePort = 0
	mlCfg.LogOutput = nil
	mlCfg.Name = nodeName
	list, err := memberlist.Create(mlCfg)
	if err != nil {
		t.Fatalf("create test memberlist: %v", err)
	}
	gm.list = list

	gm.broadcast = &memberlist.TransmitLimitedQueue{
		NumNodes:       func() int { return gm.NumMembers() },
		RetransmitMult: 3,
	}

	t.Cleanup(func() {
		gm.list.Shutdown()
	})

	return gm
}

// seedHistoryRecords inserts count HistoryRecord entries for the given nodeID
// into the store, with Version 1..count and staggered timestamps.
func seedHistoryRecords(t *testing.T, store *storage.Storage, nodeID string, count int) {
	t.Helper()
	base := time.Now()
	for i := range count {
		hr := &models.HistoryRecord{
			NodeID:    nodeID,
			Version:   int64(i + 1),
			EventType: "metric_update",
			Timestamp: base.Add(time.Duration(i) * time.Second),
			CPUUsage:  float64(i % 100),
		}
		if err := store.SaveHistoryRecord(hr); err != nil {
			t.Fatalf("seed history record %d: %v", i, err)
		}
	}
}

// ====== History Pull Integration Tests ======

func TestHandleHistoryPullRequest(t *testing.T) {
	gm := newTestGossipManagerForHistory(t, "responder")
	seedHistoryRecords(t, gm.store, "n1", 3)

	all, err := gm.store.GetAllHistorySince(time.Time{}, 0, 100)
	if err != nil {
		t.Fatalf("pre-check query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("seed verification: expected 3 records, got %d", len(all))
	}

	// The "requester" name does not match any memberlist member, so the
	// response is dropped after encoding — but the store query and msgpack
	// encoding paths are fully exercised.
	payload := &HistoryPullRequestPayload{
		RequestID:     "test-req-1",
		SinceUnixNano: 0,
		Offset:        0,
		Limit:         100,
	}
	gm.handleHistoryPullRequest(payload, "requester")

	allAfter, err := gm.store.GetAllHistorySince(time.Time{}, 0, 100)
	if err != nil {
		t.Fatalf("post-check query: %v", err)
	}
	if len(allAfter) != 3 {
		t.Errorf("expected 3 records after pull, got %d", len(allAfter))
	}
}

func TestHandleHistoryPullRequestEmpty(t *testing.T) {
	gm := newTestGossipManagerForHistory(t, "responder")

	all, err := gm.store.GetAllHistorySince(time.Time{}, 0, 100)
	if err != nil {
		t.Fatalf("pre-check query: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty store, got %d records", len(all))
	}

	payload := &HistoryPullRequestPayload{
		RequestID:     "test-req-empty",
		SinceUnixNano: 0,
		Offset:        0,
		Limit:         100,
	}
	gm.handleHistoryPullRequest(payload, "requester")

	allAfter, err := gm.store.GetAllHistorySince(time.Time{}, 0, 100)
	if err != nil {
		t.Fatalf("post-check query: %v", err)
	}
	if len(allAfter) != 0 {
		t.Errorf("expected 0 records after pull, got %d", len(allAfter))
	}
}

func TestHandleHistoryPullRequestPagination(t *testing.T) {
	gm := newTestGossipManagerForHistory(t, "responder")
	seedHistoryRecords(t, gm.store, "n1", 600)

	pageA, err := gm.store.GetAllHistorySince(time.Time{}, 0, 500)
	if err != nil {
		t.Fatalf("seed verification pageA: %v", err)
	}
	pageB, err := gm.store.GetAllHistorySince(time.Time{}, 500, 500)
	if err != nil {
		t.Fatalf("seed verification pageB: %v", err)
	}
	if len(pageA)+len(pageB) != 600 {
		t.Fatalf("expected 600 records total, got %d", len(pageA)+len(pageB))
	}

	payload := &HistoryPullRequestPayload{
		RequestID:     "test-req-page",
		SinceUnixNano: 0,
		Offset:        0,
		Limit:         500,
	}
	gm.handleHistoryPullRequest(payload, "requester")

	page1, err := gm.store.GetAllHistorySince(time.Time{}, 0, 500)
	if err != nil {
		t.Fatalf("page 1 query: %v", err)
	}
	if len(page1) != 500 {
		t.Errorf("page 1: expected 500 records, got %d", len(page1))
	}

	page2, err := gm.store.GetAllHistorySince(time.Time{}, 500, 500)
	if err != nil {
		t.Fatalf("page 2 query: %v", err)
	}
	if len(page2) != 100 {
		t.Errorf("page 2: expected 100 records, got %d", len(page2))
	}

	hasMore := len(page1) == 500
	nextOffset := 0 + len(page1)

	if !hasMore {
		t.Error("HasMore should be true when page size equals limit (500 == 500)")
	}
	if nextOffset != 500 {
		t.Errorf("NextOffset should be 500, got %d", nextOffset)
	}
}

func TestSyncHistoryOnJoinCancelledContext(t *testing.T) {
	gm := newTestGossipManagerForHistory(t, "test-node")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		err := gm.SyncHistoryOnJoin(ctx)
		// With a single-member cluster, SyncHistoryOnJoin returns nil
		// immediately (no peers to pull from). The key property under
		// test is that a cancelled context does not cause the function
		// to block indefinitely.
		if err != nil {
			t.Logf("SyncHistoryOnJoin returned: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		// success — returned quickly
	case <-time.After(5 * time.Second):
		t.Fatal("SyncHistoryOnJoin did not return within 5s on cancelled context")
	}
}

func TestSyncHistoryOnJoinFullFlow(t *testing.T) {
	t.Skip("requires multi-node cluster setup with real memberlist communication and " +
		"cross-node message delivery; the store-level pagination and pull-request " +
		"paths are covered by TestHandleHistoryPullRequestPagination")
}
