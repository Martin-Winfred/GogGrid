package storage

import (
	"errors"
	"fmt"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Storage wraps GORM database operations for node state and history persistence
type Storage struct {
	db *gorm.DB
}

// New creates a storage instance, opens SQLite and auto-migrates tables
func New(dbPath string) (*Storage, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Auto-migrate NodeState and HistoryRecord tables
	if err := db.AutoMigrate(&models.NodeState{}, &models.HistoryRecord{}); err != nil {
		return nil, err
	}

	// SQLite single-writer config to avoid "database is locked" errors
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	// Enable WAL mode for concurrent reads during writes
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}
	// Set busy timeout so SQLite waits 5s instead of immediately failing
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// Migrate from old version-based unique index to timestamp-based.
	// The version-based index caused Source field loss: when the same
	// (node_id, version, event_type) arrived via multiple paths
	// (local, gossip, sync), ON CONFLICT DO NOTHING kept only the
	// first writer's Source. Using timestamp instead of version
	// avoids this because a node cannot generate two events of the
	// same type in the same microsecond.
	if _, err := sqlDB.Exec("DROP INDEX IF EXISTS idx_history_node_version_event"); err != nil {
		return nil, fmt.Errorf("drop old unique index: %w", err)
	}
	if _, err := sqlDB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_history_node_timestamp_event ON history_records(node_id, timestamp, event_type)"); err != nil {
		return nil, fmt.Errorf("create unique index: %w", err)
	}

	return &Storage{db: db}, nil
}

// Close closes the database connection
func (s *Storage) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SaveNodeState saves or updates node state (Upsert), updates all fields on conflict
func (s *Storage) SaveNodeState(ns *models.NodeState) error {
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(ns).Error
}

// GetNodeState retrieves node state by node ID
func (s *Storage) GetNodeState(nodeID string) (*models.NodeState, error) {
	var ns models.NodeState
	if err := s.db.Where("node_id = ?", nodeID).First(&ns).Error; err != nil {
		return nil, err
	}
	return &ns, nil
}

// GetAllNodeStates retrieves all node states
func (s *Storage) GetAllNodeStates() ([]*models.NodeState, error) {
	var nodes []*models.NodeState
	if err := s.db.Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

// DeleteNodeState deletes a node's state record
func (s *Storage) DeleteNodeState(nodeID string) error {
	return s.db.Where("node_id = ?", nodeID).Delete(&models.NodeState{}).Error
}

// SaveHistoryRecord saves a history record, silently skipping duplicates
// identified by the unique constraint on (node_id, timestamp, event_type).
func (s *Storage) SaveHistoryRecord(hr *models.HistoryRecord) error {
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(hr).Error
}

// GetNodeHistory retrieves node history with optional time range filter.
// Uses half-open interval [since, until): since inclusive, until exclusive.
// Zero value for since or until means no bound on that side.
func (s *Storage) GetNodeHistory(nodeID string, since, until time.Time) ([]*models.HistoryRecord, error) {
	var records []*models.HistoryRecord
	query := s.db.Where("node_id = ?", nodeID)

	if !since.IsZero() {
		query = query.Where("timestamp >= ?", since)
	}
	if !until.IsZero() {
		query = query.Where("timestamp < ?", until)
	}

	if err := query.Order("timestamp ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// CleanOldRecords deletes history records older than retention, returns count
func (s *Storage) CleanOldRecords(retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)
	result := s.db.Where("timestamp < ?", cutoff).Delete(&models.HistoryRecord{})
	return result.RowsAffected, result.Error
}

// GetAllHistorySince retrieves all history records across all nodes since a given time,
// ordered by timestamp ascending. Supports pagination via offset and limit.
// Pass time.Time{} for since to retrieve all records. Limit is capped at 500.
func (s *Storage) GetAllHistorySince(since time.Time, offset, limit int) ([]*models.HistoryRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var records []*models.HistoryRecord
	query := s.db.Model(&models.HistoryRecord{})
	if !since.IsZero() {
		query = query.Where("timestamp >= ?", since)
	}
	if err := query.Order("timestamp ASC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// GetLatestHistoryTime returns the timestamp of the most recent HistoryRecord.
// Returns time.Time{} if the table is empty.
func (s *Storage) GetLatestHistoryTime() (time.Time, error) {
	var hr models.HistoryRecord
	err := s.db.Order("timestamp DESC").Limit(1).First(&hr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return hr.Timestamp, nil
}

// ImportHistoryRecords bulk-imports history records pulled from a remote peer.
// Uses ON CONFLICT DO NOTHING to skip duplicates (same node_id + timestamp + event_type).
// Returns the number of rows actually inserted.
func (s *Storage) ImportHistoryRecords(records []*models.HistoryRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	var inserted int64
	for _, r := range records {
		result := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(r)
		if result.Error != nil {
			return inserted, result.Error
		}
		inserted += result.RowsAffected
	}
	return inserted, nil
}
