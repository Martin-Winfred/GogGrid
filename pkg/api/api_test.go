package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

func setupTestServer(t *testing.T) (*APIServer, *state.StateManager, *storage.Storage) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.API.Port = 0 // random port
	stateMgr := state.NewStateManager("TestCluster", "test-node-1", nil)
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("storage create failed: %v", err)
	}
	server := New(cfg, stateMgr, store)
	return server, stateMgr, store
}

func TestHealthEndpoint(t *testing.T) {
	srv, _, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %s", resp["status"])
	}
	if resp["node_id"] != "test-node-1" {
		t.Errorf("node_id = %s", resp["node_id"])
	}
}

func TestClusterEndpoint(t *testing.T) {
	srv, stateMgr, store := setupTestServer(t)
	defer store.Close()

	now := time.Now()
	stateMgr.UpdateLocalNode(&models.NodeState{
		NodeID:      "test-node-1",
		Status:      "active",
		CPUUsage:    50.0,
		LastSeen:    now,
		LastUpdated: now,
	})

	req := httptest.NewRequest("GET", "/api/cluster", nil)
	w := httptest.NewRecorder()
	srv.handleCluster(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}
}

func TestNodesEndpoint(t *testing.T) {
	srv, stateMgr, store := setupTestServer(t)
	defer store.Close()

	now := time.Now()
	stateMgr.UpdateLocalNode(&models.NodeState{
		NodeID:      "n1",
		Status:      "active",
		LastSeen:    now,
		LastUpdated: now,
	})
	stateMgr.MergeNodeState(&models.NodeState{
		NodeID:      "n2",
		Status:      "active",
		LastSeen:    now,
		LastUpdated: now,
	})

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	srv.handleNodes(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}

	var nodes []map[string]any
	json.NewDecoder(w.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("node count = %d, want 2", len(nodes))
	}
}

func TestNodeDetailFound(t *testing.T) {
	srv, stateMgr, store := setupTestServer(t)
	defer store.Close()

	now := time.Now()
	stateMgr.UpdateLocalNode(&models.NodeState{
		NodeID:      "n1",
		Status:      "active",
		CPUUsage:    75.0,
		LastSeen:    now,
		LastUpdated: now,
	})

	req := httptest.NewRequest("GET", "/api/nodes/n1", nil)
	w := httptest.NewRecorder()
	srv.handleNodeDetail(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}
}

func TestNodeDetailNotFound(t *testing.T) {
	srv, _, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("GET", "/api/nodes/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.handleNodeDetail(w, req)

	if w.Code != 404 {
		t.Errorf("status code = %d, want 404", w.Code)
	}
}

func TestNodeHistory(t *testing.T) {
	srv, _, store := setupTestServer(t)
	defer store.Close()

	hr := &models.HistoryRecord{
		NodeID:    "n1",
		Version:   1,
		EventType: "metric_update",
		Timestamp: time.Now(),
		CPUUsage:  30.0,
	}
	store.SaveHistoryRecord(hr)

	req := httptest.NewRequest("GET", "/api/nodes/n1/history", nil)
	w := httptest.NewRecorder()
	srv.handleNodeDetail(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}

	var records []map[string]any
	json.NewDecoder(w.Body).Decode(&records)
	if len(records) != 1 {
		t.Errorf("record count = %d, want 1", len(records))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, _, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("POST", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	if w.Code != 405 {
		t.Errorf("status code = %d, want 405", w.Code)
	}
}
