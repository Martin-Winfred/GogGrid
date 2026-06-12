package storage

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
)

// TestNew verifies creating Storage instance with in-memory DB and auto-migration
func TestNew(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("storage create failed: %v", err)
	}
	defer s.Close()
}

// TestSaveAndGetNodeState verifies saving (including Upsert) and reading node state
func TestSaveAndGetNodeState(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	now := time.Now()
	ns := &models.NodeState{
		NodeID: "test1", IPAddress: "192.168.0.1", Status: "active",
		CPUUsage: 50.0, LastSeen: now, LastUpdated: now,
	}
	if err := s.SaveNodeState(ns); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	got, err := s.GetNodeState("test1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.CPUUsage != 50.0 {
		t.Errorf("CPUUsage = %v, want 50.0", got.CPUUsage)
	}
	if got.IPAddress != "192.168.0.1" {
		t.Errorf("IPAddress = %v, want 192.168.0.1", got.IPAddress)
	}

	// Upsert: update then re-read
	ns.CPUUsage = 80.0
	if err := s.SaveNodeState(ns); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got2, _ := s.GetNodeState("test1")
	if got2.CPUUsage != 80.0 {
		t.Errorf("after update CPUUsage = %v, want 80.0", got2.CPUUsage)
	}
}

// TestGetAllNodeStates verifies retrieving all node states
func TestGetAllNodeStates(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	now := time.Now()
	s.SaveNodeState(&models.NodeState{NodeID: "n1", LastSeen: now, LastUpdated: now})
	s.SaveNodeState(&models.NodeState{NodeID: "n2", LastSeen: now, LastUpdated: now})

	nodes, err := s.GetAllNodeStates()
	if err != nil {
		t.Fatalf("get all failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("node count = %d, want 2", len(nodes))
	}
}

// TestSaveAndGetHistoryRecord verifies saving and basic query of history records
func TestSaveAndGetHistoryRecord(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	hr := &models.HistoryRecord{NodeID: "n1", Timestamp: time.Now(), CPUUsage: 30.0}
	if err := s.SaveHistoryRecord(hr); err != nil {
		t.Fatalf("save history failed: %v", err)
	}

	records, err := s.GetNodeHistory("n1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("record count = %d, want 1", len(records))
	}
}

// TestGetNodeHistoryTimeRange verifies time-range filtered queries
func TestGetNodeHistoryTimeRange(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	base := time.Now()
	for i := range 3 {
		s.SaveHistoryRecord(&models.HistoryRecord{
			NodeID: "n1", Timestamp: base.Add(time.Duration(i) * time.Hour),
			CPUUsage: float64(i * 10),
		})
	}

	records, err := s.GetNodeHistory("n1", base, base.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("time range query failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("records in range = %d, want 2", len(records))
	}
}

// TestDeleteNodeState verifies node state deletion
func TestDeleteNodeState(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	now := time.Now()
	s.SaveNodeState(&models.NodeState{NodeID: "n1", LastSeen: now, LastUpdated: now})

	if err := s.DeleteNodeState("n1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := s.GetNodeState("n1")
	if err == nil {
		t.Error("should return record not found error")
	}
}

// TestCleanOldRecords verifies expired history record cleanup
func TestCleanOldRecords(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	oldTime := time.Now().Add(-2 * time.Hour)
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Timestamp: oldTime, CPUUsage: 10.0})
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Timestamp: time.Now(), CPUUsage: 20.0})

	n, err := s.CleanOldRecords(1 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted rows = %d, want 1", n)
	}

	records, _ := s.GetNodeHistory("n1", time.Time{}, time.Time{})
	if len(records) != 1 {
		t.Errorf("records after cleanup = %d, want 1", len(records))
	}
}

// TestConcurrentReadWrite verifies concurrent reads and writes do not deadlock
// or produce "database is locked" errors when WAL mode and busy_timeout are enabled
func TestConcurrentReadWrite(t *testing.T) {
	// Use a file-based DB so WAL mode is effective
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("storage create failed: %v", err)
	}
	defer s.Close()

	now := time.Now()
	var wg sync.WaitGroup
	writeErrs := make([]error, 0, 20)
	readErrs := make([]error, 0, 20)
	var mu sync.Mutex

	// Writer goroutine: save node states in a loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 20 {
			ns := &models.NodeState{
				NodeID: "concurrent-node", CPUUsage: float64(i),
				LastSeen: now, LastUpdated: now,
			}
			if err := s.SaveNodeState(ns); err != nil {
				mu.Lock()
				writeErrs = append(writeErrs, err)
				mu.Unlock()
			}
		}
	}()

	// Reader goroutine: read node states in a loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			if _, err := s.GetNodeState("concurrent-node"); err != nil {
				mu.Lock()
				readErrs = append(readErrs, err)
				mu.Unlock()
			}
		}
	}()

	wg.Wait()

	if len(writeErrs) > 0 {
		t.Errorf("write errors: %v", writeErrs)
	}
	if len(readErrs) > 0 {
		t.Errorf("read errors: %v", readErrs)
	}
}
