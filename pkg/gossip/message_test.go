package gossip

import (
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
)

func TestHistoryPullPayloadRoundtrip(t *testing.T) {
	original := &HistoryPullRequestPayload{
		RequestID:     "node-a-1718234567890123456",
		SinceUnixNano: 0,
		Offset:        0,
		Limit:         500,
	}

	payload, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded HistoryPullRequestPayload
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID expected %q, got %q", original.RequestID, decoded.RequestID)
	}
	if decoded.SinceUnixNano != original.SinceUnixNano {
		t.Errorf("SinceUnixNano expected %d, got %d", original.SinceUnixNano, decoded.SinceUnixNano)
	}
	if decoded.Offset != original.Offset {
		t.Errorf("Offset expected %d, got %d", original.Offset, decoded.Offset)
	}
	if decoded.Limit != original.Limit {
		t.Errorf("Limit expected %d, got %d", original.Limit, decoded.Limit)
	}
}

func TestHistoryPullResponsePayloadRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := &HistoryPullResponsePayload{
		RequestID: "node-a-1718234567890123456",
		Records: []*models.HistoryRecord{
			{NodeID: "n1", EventType: "metric_update", Timestamp: now, CPUUsage: 42.0},
			{NodeID: "n1", EventType: "metric_update", Timestamp: now.Add(5 * time.Second), CPUUsage: 45.0},
			{NodeID: "n2", EventType: "node_join", Timestamp: now, CPUUsage: 0.0},
			{NodeID: "n2", EventType: "metric_update", Timestamp: now.Add(1 * time.Second), CPUUsage: 30.0},
			{NodeID: "n3", EventType: "metric_update", Timestamp: now.Add(10 * time.Second), CPUUsage: 80.0},
		},
		HasMore:    true,
		NextOffset: 500,
		TotalCount: 1500,
	}

	payload, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded HistoryPullResponsePayload
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID expected %q, got %q", original.RequestID, decoded.RequestID)
	}
	if len(decoded.Records) != 5 {
		t.Errorf("Records count expected 5, got %d", len(decoded.Records))
	}
	for i, rec := range decoded.Records {
		if rec.NodeID != original.Records[i].NodeID {
			t.Errorf("Record[%d] NodeID expected %q, got %q", i, original.Records[i].NodeID, rec.NodeID)
		}
		if rec.EventType != original.Records[i].EventType {
			t.Errorf("Record[%d] EventType expected %q, got %q", i, original.Records[i].EventType, rec.EventType)
		}
	}
	if decoded.HasMore != original.HasMore {
		t.Errorf("HasMore expected %v, got %v", original.HasMore, decoded.HasMore)
	}
	if decoded.NextOffset != original.NextOffset {
		t.Errorf("NextOffset expected %d, got %d", original.NextOffset, decoded.NextOffset)
	}
	if decoded.TotalCount != original.TotalCount {
		t.Errorf("TotalCount expected %d, got %d", original.TotalCount, decoded.TotalCount)
	}
}

func TestHistoryPullPaginationFlags(t *testing.T) {
	original := &HistoryPullResponsePayload{
		RequestID:  "req-1",
		Records:    []*models.HistoryRecord{},
		HasMore:    true,
		NextOffset: 500,
		TotalCount: 1500,
	}

	payload, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded HistoryPullResponsePayload
	if err := DecodePayload(payload, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !decoded.HasMore {
		t.Error("HasMore should be true after roundtrip")
	}
	if decoded.NextOffset != 500 {
		t.Errorf("NextOffset expected 500, got %d", decoded.NextOffset)
	}
	if decoded.TotalCount != 1500 {
		t.Errorf("TotalCount expected 1500, got %d", decoded.TotalCount)
	}
}
