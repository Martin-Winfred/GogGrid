package gossip

import (
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/vmihailenco/msgpack/v5"
)

// Message type constants
const (
	MsgNodeState           = 0 // node state update
	MsgClusterSync         = 1 // cluster full sync
	MsgHeartbeat           = 2 // heartbeat
	MsgDiscovery           = 3 // discovery broadcast
	MsgHistoryPullRequest  = 4 // history data pull request
	MsgHistoryPullResponse = 5 // history data pull response
)

// GossipMessage is a generic message wrapper
type GossipMessage struct {
	Type      uint8  `msgpack:"type"`
	FromNode  string `msgpack:"from"`
	Timestamp int64  `msgpack:"ts"`
	Payload   []byte `msgpack:"payload"`
}

// NodeStatePayload carries node state data
type NodeStatePayload struct {
	State *models.NodeState `msgpack:"state"`
}

// ClusterSyncPayload carries full cluster sync data
type ClusterSyncPayload struct {
	Nodes []*models.NodeState `msgpack:"nodes"`
}

// HistoryPullRequestPayload requests history records from a peer.
type HistoryPullRequestPayload struct {
	RequestID     string `msgpack:"req_id"`
	SinceUnixNano int64  `msgpack:"since"`
	Offset        int    `msgpack:"offset"`
	Limit         int    `msgpack:"limit"`
}

// HistoryPullResponsePayload carries a batch of history records.
type HistoryPullResponsePayload struct {
	RequestID  string                  `msgpack:"req_id"`
	Records    []*models.HistoryRecord `msgpack:"records"`
	HasMore    bool                    `msgpack:"has_more"`
	NextOffset int                     `msgpack:"next_offset"`
	TotalCount int                     `msgpack:"total_count"`
}

type DiscoveryMessage struct {
	NodeID      string `msgpack:"node_id"`
	ClusterName string `msgpack:"cluster_name"`
	GossipAddr  string `msgpack:"gossip_addr"`
	Timestamp   int64  `msgpack:"ts"`
}

// EncodeMessage encodes a GossipMessage
func EncodeMessage(msg *GossipMessage) ([]byte, error) {
	return msgpack.Marshal(msg)
}

// DecodeMessage decodes a GossipMessage
func DecodeMessage(data []byte) (*GossipMessage, error) {
	var msg GossipMessage
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// EncodePayload encodes a payload
func EncodePayload(v interface{}) ([]byte, error) {
	return msgpack.Marshal(v)
}

// DecodePayload decodes a payload
func DecodePayload(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

// NewGossipMessage creates a new message
func NewGossipMessage(msgType uint8, fromNode string, payload []byte) *GossipMessage {
	return &GossipMessage{
		Type:      msgType,
		FromNode:  fromNode,
		Timestamp: time.Now().UnixNano(),
		Payload:   payload,
	}
}
