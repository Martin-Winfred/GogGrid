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

	hr := &models.HistoryRecord{NodeID: "n1", Timestamp: time.Now(), CPUUsage: 30.0, Version: 1, EventType: "metric_update"}
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
			CPUUsage: float64(i * 10), Version: int64(i + 1), EventType: "metric_update",
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
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Timestamp: oldTime, CPUUsage: 10.0, Version: 1, EventType: "metric_update"})
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Timestamp: time.Now(), CPUUsage: 20.0, Version: 2, EventType: "metric_update"})

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

func TestGetAllHistorySince(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	base := time.Now()
	// Insert 15 records: 3 nodes × 5 each, timestamps increment
	for nodeIdx := range 3 {
		nodeID := string(rune('a' + nodeIdx))
		for i := range 5 {
			hr := &models.HistoryRecord{
				NodeID:    nodeID,
				Version:   int64(i + 1),
				EventType: "metric_update",
				Timestamp: base.Add(time.Duration(nodeIdx*5+i) * time.Second),
				CPUUsage:  float64(nodeIdx*5 + i),
			}
			if err := s.SaveHistoryRecord(hr); err != nil {
				t.Fatalf("seed failed: %v", err)
			}
		}
	}

	// Scenario 1: first page (offset 0, limit 5)
	records, err := s.GetAllHistorySince(time.Time{}, 0, 5)
	if err != nil {
		t.Fatalf("page 1 failed: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("page 1 count = %d, want 5", len(records))
	}

	// Scenario 2: second page (offset 5, limit 5)
	records, err = s.GetAllHistorySince(time.Time{}, 5, 5)
	if err != nil {
		t.Fatalf("page 2 failed: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("page 2 count = %d, want 5", len(records))
	}

	// Scenario 3: third page (offset 10, limit 5)
	records, err = s.GetAllHistorySince(time.Time{}, 10, 5)
	if err != nil {
		t.Fatalf("page 3 failed: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("page 3 count = %d, want 5", len(records))
	}

	// Scenario 4: since filter (only records after t5)
	t5 := base.Add(5 * time.Second)
	records, err = s.GetAllHistorySince(t5, 0, 100)
	if err != nil {
		t.Fatalf("since filter failed: %v", err)
	}
	if len(records) != 10 {
		t.Errorf("since filter count = %d, want 10", len(records))
	}

	// Scenario 5: empty table
	s2, _ := New(":memory:")
	defer s2.Close()
	records, err = s2.GetAllHistorySince(time.Time{}, 0, 10)
	if err != nil {
		t.Fatalf("empty table failed: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("empty table count = %d, want 0", len(records))
	}

	// Scenario 6: limit cap at 500
	records, err = s.GetAllHistorySince(time.Time{}, 0, 9999)
	if err != nil {
		t.Fatalf("limit cap failed: %v", err)
	}
	if len(records) != 15 {
		t.Errorf("limit cap count = %d, want 15", len(records))
	}
}

func TestGetLatestHistoryTime(t *testing.T) {
	// Scenario 1: empty table returns zero time
	s, _ := New(":memory:")
	defer s.Close()
	ts, err := s.GetLatestHistoryTime()
	if err != nil {
		t.Fatalf("empty table failed: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("empty table got %v, want zero time", ts)
	}

	// Scenario 2: returns max timestamp
	base := time.Now()
	t1 := base
	t2 := base.Add(1 * time.Hour)
	t3 := base.Add(2 * time.Hour)
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Version: 1, EventType: "metric_update", Timestamp: t1})
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Version: 2, EventType: "metric_update", Timestamp: t2})
	s.SaveHistoryRecord(&models.HistoryRecord{NodeID: "n1", Version: 3, EventType: "metric_update", Timestamp: t3})

	ts, err = s.GetLatestHistoryTime()
	if err != nil {
		t.Fatalf("max query failed: %v", err)
	}
	if !ts.Equal(t3) {
		t.Errorf("latest time = %v, want %v", ts, t3)
	}
}

func TestImportHistoryRecords(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	now := time.Now()

	// Scenario 1: import 5 records → 5 inserted
	batch1 := make([]*models.HistoryRecord, 5)
	for i := range 5 {
		batch1[i] = &models.HistoryRecord{
			NodeID: "n1", Version: int64(i + 1), EventType: "metric_update",
			Timestamp: now.Add(time.Duration(i) * time.Second), CPUUsage: float64(i * 10),
		}
	}
	n, err := s.ImportHistoryRecords(batch1)
	if err != nil {
		t.Fatalf("import batch1 failed: %v", err)
	}
	if n != 5 {
		t.Errorf("batch1 inserted = %d, want 5", n)
	}

	// Scenario 2: import same 5 + 2 new → 2 inserted, DB has 7
	batch2 := make([]*models.HistoryRecord, 7)
	copy(batch2[:5], batch1)
	batch2[5] = &models.HistoryRecord{
		NodeID: "n1", Version: 6, EventType: "metric_update",
		Timestamp: now.Add(5 * time.Second), CPUUsage: 50.0,
	}
	batch2[6] = &models.HistoryRecord{
		NodeID: "n1", Version: 7, EventType: "metric_update",
		Timestamp: now.Add(6 * time.Second), CPUUsage: 60.0,
	}
	n, err = s.ImportHistoryRecords(batch2)
	if err != nil {
		t.Fatalf("import batch2 failed: %v", err)
	}
	if n != 2 {
		t.Errorf("batch2 inserted = %d, want 2", n)
	}

	records, _ := s.GetNodeHistory("n1", time.Time{}, time.Time{})
	if len(records) != 7 {
		t.Errorf("total records = %d, want 7", len(records))
	}

	// Scenario 3: import empty slice → 0 inserted
	n, err = s.ImportHistoryRecords(nil)
	if err != nil {
		t.Fatalf("empty import failed: %v", err)
	}
	if n != 0 {
		t.Errorf("empty import inserted = %d, want 0", n)
	}

	// Scenario 4: first-write-wins on conflict (original value preserved)
	hrUpdated := &models.HistoryRecord{
		NodeID: "n1", Version: 1, EventType: "metric_update",
		Timestamp: now, CPUUsage: 999.0,
	}
	n, _ = s.ImportHistoryRecords([]*models.HistoryRecord{hrUpdated})
	if n != 0 {
		t.Errorf("conflict insert count = %d, want 0", n)
	}
	records, _ = s.GetNodeHistory("n1", time.Time{}, time.Time{})
	var firstRecord *models.HistoryRecord
	for _, r := range records {
		if r.Version == 1 {
			firstRecord = r
			break
		}
	}
	if firstRecord != nil && firstRecord.CPUUsage != 0.0 {
		t.Errorf("first-write-wins violated: CPUUsage = %f, want 0.0", firstRecord.CPUUsage)
	}
}

func TestImportHistoryRecordsUniqueConstraint(t *testing.T) {
	s, _ := New(":memory:")
	defer s.Close()

	t1 := time.Now()
	t2 := t1.Add(1 * time.Hour)

	// (A, t1, metric_update) → inserted
	n, err := s.ImportHistoryRecords([]*models.HistoryRecord{{
		NodeID: "A", Version: 1, EventType: "metric_update", Timestamp: t1,
	}})
	if err != nil {
		t.Fatalf("case 1 failed: %v", err)
	}
	if n != 1 {
		t.Errorf("case 1 inserted = %d, want 1", n)
	}

	// (A, t1, node_leave) → inserted (different EventType, same timestamp)
	n, err = s.ImportHistoryRecords([]*models.HistoryRecord{{
		NodeID: "A", Version: 1, EventType: "node_leave", Timestamp: t1,
	}})
	if err != nil {
		t.Fatalf("case 2 failed: %v", err)
	}
	if n != 1 {
		t.Errorf("case 2 inserted = %d, want 1", n)
	}

	// (A, t1, metric_update) again → skipped (duplicate on node_id+timestamp+event_type)
	n, err = s.ImportHistoryRecords([]*models.HistoryRecord{{
		NodeID: "A", Version: 2, EventType: "metric_update", Timestamp: t1,
	}})
	if err != nil {
		t.Fatalf("case 3 failed: %v", err)
	}
	if n != 0 {
		t.Errorf("case 3 inserted = %d, want 0", n)
	}

	// (A, t2, metric_update) → inserted (different timestamp)
	n, err = s.ImportHistoryRecords([]*models.HistoryRecord{{
		NodeID: "A", Version: 3, EventType: "metric_update", Timestamp: t2,
	}})
	if err != nil {
		t.Fatalf("case 4 failed: %v", err)
	}
	if n != 1 {
		t.Errorf("case 4 inserted = %d, want 1", n)
	}

	// (B, t1, metric_update) → inserted (different NodeID)
	n, err = s.ImportHistoryRecords([]*models.HistoryRecord{{
		NodeID: "B", Version: 1, EventType: "metric_update", Timestamp: t1,
	}})
	if err != nil {
		t.Fatalf("case 5 failed: %v", err)
	}
	if n != 1 {
		t.Errorf("case 5 inserted = %d, want 1", n)
	}
}
