package storage

import (
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

// SaveHistoryRecord saves a history record
func (s *Storage) SaveHistoryRecord(hr *models.HistoryRecord) error {
	return s.db.Create(hr).Error
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
