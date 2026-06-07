package models

import (
	"encoding/json"
	"time"
)

// NetInterface represents network interface info
type NetInterface struct {
	InterfaceName string `json:"interface_name" gorm:"column:net_interface_name"`
	Speed         int64  `json:"speed"          gorm:"column:net_speed"`
	RxBytes       uint64 `json:"rx_bytes"       gorm:"column:net_rx_bytes"`
	TxBytes       uint64 `json:"tx_bytes"       gorm:"column:net_tx_bytes"`
}

// NetworkUsage represents network usage stats
type NetworkUsage struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	RxBytes   uint64    `json:"rx_bytes"`
	TxBytes   uint64    `json:"tx_bytes"`
}

// SystemLoad represents system load averages
type SystemLoad struct {
	LoadAvg1min  float64 `json:"load_avg_1min"  gorm:"column:load_avg_1min"`
	LoadAvg5min  float64 `json:"load_avg_5min"  gorm:"column:load_avg_5min"`
	LoadAvg15min float64 `json:"load_avg_15min" gorm:"column:load_avg_15min"`
}

// NodeState represents node state
type NodeState struct {
	NodeID       string       `json:"node_id"        gorm:"primaryKey"`
	IPAddress    string       `json:"ip_address"     gorm:"column:ip_address"`
	Status       string       `json:"status"`
	SystemType   string       `json:"system_type"    gorm:"column:system_type"`
	CPUUsage     float64      `json:"cpu_usage"      gorm:"column:cpu_usage"`
	MemoryUsage  float64      `json:"memory_usage"   gorm:"column:memory_usage"`
	DiskUsage    float64      `json:"disk_usage"     gorm:"column:disk_usage"`
	NetInterface NetInterface `json:"network_interface" gorm:"embedded;embeddedPrefix:net_"`
	SystemLoad   SystemLoad   `json:"system_load"    gorm:"embedded;embeddedPrefix:sys_"`
	LastSeen     time.Time    `json:"last_seen"      gorm:"column:last_seen"`
	LastUpdated  time.Time    `json:"last_updated"   gorm:"column:last_updated"`
	Version      int64        `json:"version"`
}

// HistoryRecord represents a historical metrics record
type HistoryRecord struct {
	ID           uint         `gorm:"primaryKey;autoIncrement"`
	NodeID       string       `gorm:"index;column:node_id"`
	Timestamp    time.Time    `gorm:"index;column:timestamp"`
	CPUUsage     float64      `gorm:"column:cpu_usage"`
	MemoryUsage  float64      `gorm:"column:memory_usage"`
	DiskUsage    float64      `gorm:"column:disk_usage"`
	NetInterface NetInterface `gorm:"embedded;embeddedPrefix:net_"`
	SystemLoad   SystemLoad   `gorm:"embedded;embeddedPrefix:sys_"`
}

// VectorClock represents a vector clock
type VectorClock map[string]int64

// Increment increments the version for a node
func (vc VectorClock) Increment(nodeID string) {
	vc[nodeID]++
}

// Compare compares vector clocks: -1=local<remote, 1=local>remote, 0=concurrent/equal
func (vc VectorClock) Compare(other VectorClock) int {
	localGreater := false
	remoteGreater := false

	for nodeID, localVer := range vc {
		remoteVer, ok := other[nodeID]
		if !ok || localVer > remoteVer {
			localGreater = true
		} else if localVer < remoteVer {
			remoteGreater = true
		}
	}

	for nodeID := range other {
		if _, ok := vc[nodeID]; !ok {
			remoteGreater = true
		}
	}

	if localGreater && !remoteGreater {
		return 1
	}
	if remoteGreater && !localGreater {
		return -1
	}
	return 0
}

// Merge merges vector clocks, taking the max for each node
func (vc VectorClock) Merge(other VectorClock) {
	for nodeID, ver := range other {
		if current, ok := vc[nodeID]; !ok || ver > current {
			vc[nodeID] = ver
		}
	}
}

// ClusterState represents the cluster state
type ClusterState struct {
	ClusterName string                `json:"cluster_name"`
	Nodes       map[string]*NodeState `json:"nodes"`
	LocalNodeID string                `json:"local_node_id"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

// Ensure json is used (imported for future JSON utilities)
var _ = json.Marshal
