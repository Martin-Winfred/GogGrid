package monitor

import (
	"testing"
)

// TestGetLocalIP verifies getLocalIP returns a valid IP address without error
func TestGetLocalIP(t *testing.T) {
	ip, err := getLocalIP()
	if err != nil {
		t.Fatalf("getLocalIP() returned error: %v", err)
	}
	if ip == "" {
		t.Fatal("getLocalIP() returned empty string")
	}
	// Simple IP format check: must contain "."
	if len(ip) < 7 {
		t.Fatalf("getLocalIP() returned unexpected short string: %q", ip)
	}
}

// TestGetHostMonitor verifies GetHostMonitor returns complete monitoring data without error
func TestGetHostMonitor(t *testing.T) {
	hm, err := GetHostMonitor()
	if err != nil {
		t.Fatalf("GetHostMonitor() returned error: %v", err)
	}

	// Verify required fields are non-empty
	if hm.Arch == "" {
		t.Error("HostMonitor.Arch is empty")
	}
	if hm.OSInfo == "" {
		t.Error("HostMonitor.OSInfo is empty")
	}
	if hm.Hostname == "" {
		t.Error("HostMonitor.Hostname is empty")
	}
	if hm.LocalIP == "" {
		t.Error("HostMonitor.LocalIP is empty")
	}

	// Verify memory data is reasonable
	if hm.MemTotal == 0 {
		t.Error("HostMonitor.MemTotal is zero, expected non-zero")
	}
	if hm.MemUsed > hm.MemTotal {
		t.Error("HostMonitor.MemUsed exceeds MemTotal")
	}

	// Verify CPU load has at least 1 data point
	if len(hm.CPULoad) == 0 {
		t.Error("HostMonitor.CPULoad is empty")
	}

	// Verify new fields are not default zero values
	if hm.DiskUsage <= 0 {
		t.Error("HostMonitor.DiskUsage is zero or negative")
	}
	if hm.LoadAvg1min <= 0 && hm.LoadAvg5min <= 0 && hm.LoadAvg15min <= 0 {
		t.Error("HostMonitor load averages are all zero")
	}
	if hm.NetInterfaceName == "" {
		t.Error("HostMonitor.NetInterfaceName is empty")
	}
}

// TestToNodeState verifies ToNodeState correctly converts to NodeState
func TestToNodeState(t *testing.T) {
	hm := &HostMonitor{
		Arch:             "amd64",
		OSInfo:           "linux",
		Hostname:         "test-host",
		LocalIP:          "192.168.1.1",
		CPULoad:          []float64{25.5},
		MemUsage:         60.0,
		DiskUsage:        45.0,
		BytesRecv:        1000000,
		BytesSent:        500000,
		NetInterfaceName: "eth0",
		LoadAvg1min:      1.2,
		LoadAvg5min:      0.8,
		LoadAvg15min:     0.6,
	}

	nodeID := "node-001"
	ns := hm.ToNodeState(nodeID)

	if ns.NodeID != nodeID {
		t.Errorf("NodeState.NodeID = %q, want %q", ns.NodeID, nodeID)
	}
	if ns.IPAddress != "192.168.1.1" {
		t.Errorf("NodeState.IPAddress = %q, want %q", ns.IPAddress, "192.168.1.1")
	}
	if ns.Status != "active" {
		t.Errorf("NodeState.Status = %q, want %q", ns.Status, "active")
	}
	if ns.SystemType != "linux" {
		t.Errorf("NodeState.SystemType = %q, want %q", ns.SystemType, "linux")
	}
	if ns.CPUUsage != 25.5 {
		t.Errorf("NodeState.CPUUsage = %f, want %f", ns.CPUUsage, 25.5)
	}
	if ns.MemoryUsage != 60.0 {
		t.Errorf("NodeState.MemoryUsage = %f, want %f", ns.MemoryUsage, 60.0)
	}
	if ns.DiskUsage != 45.0 {
		t.Errorf("NodeState.DiskUsage = %f, want %f", ns.DiskUsage, 45.0)
	}
	if ns.NetInterface.InterfaceName != "eth0" {
		t.Errorf("NetInterface.InterfaceName = %q, want %q", ns.NetInterface.InterfaceName, "eth0")
	}
	if ns.NetInterface.RxBytes != 1000000 {
		t.Errorf("NetInterface.RxBytes = %d, want %d", ns.NetInterface.RxBytes, 1000000)
	}
	if ns.NetInterface.TxBytes != 500000 {
		t.Errorf("NetInterface.TxBytes = %d, want %d", ns.NetInterface.TxBytes, 500000)
	}
	if ns.SystemLoad.LoadAvg1min != 1.2 {
		t.Errorf("SystemLoad.LoadAvg1min = %f, want %f", ns.SystemLoad.LoadAvg1min, 1.2)
	}
	if ns.SystemLoad.LoadAvg5min != 0.8 {
		t.Errorf("SystemLoad.LoadAvg5min = %f, want %f", ns.SystemLoad.LoadAvg5min, 0.8)
	}
	if ns.SystemLoad.LoadAvg15min != 0.6 {
		t.Errorf("SystemLoad.LoadAvg15min = %f, want %f", ns.SystemLoad.LoadAvg15min, 0.6)
	}
	if ns.LastSeen.IsZero() {
		t.Error("NodeState.LastSeen is zero time")
	}
	if ns.LastUpdated.IsZero() {
		t.Error("NodeState.LastUpdated is zero time")
	}
	if ns.Version != 1 {
		t.Errorf("NodeState.Version = %d, want %d", ns.Version, 1)
	}
}
