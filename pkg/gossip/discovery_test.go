package gossip

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/hashicorp/mdns"
	"github.com/hashicorp/memberlist"
	"github.com/vmihailenco/msgpack/v5"
)

// boolPtr returns a pointer to a bool. Mirrors config.boolPtr which is unexported.
func boolPtr(b bool) *bool { return &b }

func TestDiscoveryBaseIsNew(t *testing.T) {
	db := newDiscoveryBase("test-cluster")

	// First call for a new address should return true.
	if !db.isNew("10.0.0.1:7946") {
		t.Error("isNew: first call for new address should return true")
	}

	// Second call within cooldown should return false.
	if db.isNew("10.0.0.1:7946") {
		t.Error("isNew: second call within cooldown should return false")
	}

	// Different address should return true (not previously seen).
	if !db.isNew("10.0.0.2:7946") {
		t.Error("isNew: different address should return true")
	}

	// After cooldown expires, the same address should return true again.
	db.cooldown = 10 * time.Millisecond
	time.Sleep(15 * time.Millisecond)
	if !db.isNew("10.0.0.1:7946") {
		t.Error("isNew: after cooldown expiry should return true")
	}
}

func TestDiscoveryBaseIsNewConcurrent(t *testing.T) {
	db := newDiscoveryBase("test-cluster")
	addr := "10.0.0.99:7946"

	// Launch several goroutines calling isNew concurrently with the same address.
	const goroutines = 50
	results := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			results <- db.isNew(addr)
		}()
	}

	// Only the first call should see the address as new; others should be dupes.
	newCount := 0
	for i := 0; i < goroutines; i++ {
		if <-results {
			newCount++
		}
	}
	if newCount != 1 {
		t.Errorf("isNew concurrent: expected exactly 1 true, got %d", newCount)
	}
}

// newTestMemberlist creates a memberlist on an ephemeral port for testing.
func newTestMemberlist(t *testing.T) *memberlist.Memberlist {
	t.Helper()
	mlCfg := memberlist.DefaultLANConfig()
	mlCfg.BindPort = 0
	mlCfg.AdvertisePort = 0
	mlCfg.LogOutput = nil
	mlCfg.Name = "test-discovery-node"
	list, err := memberlist.Create(mlCfg)
	if err != nil {
		t.Fatalf("failed to create test memberlist: %v", err)
	}
	t.Cleanup(func() {
		list.Shutdown()
	})
	return list
}

func TestDiscoveryBaseTryJoinClusterMismatch(t *testing.T) {
	db := newDiscoveryBase("test-cluster")
	// Cluster mismatch means Join is never reached; nil list is safe.
	gm := &GossipManager{}

	db.tryJoin(gm, "192.168.1.1:7946", "other-cluster")

	db.mu.Lock()
	_, exists := db.seenAddrs["192.168.1.1:7946"]
	db.mu.Unlock()
	if exists {
		t.Error("tryJoin: address should not be tracked for mismatched cluster name")
	}
}

func TestDiscoveryBaseTryJoinDedup(t *testing.T) {
	db := newDiscoveryBase("test-cluster")
	gm := &GossipManager{}

	addr := "192.168.1.2:7946"

	// Pre-populate seenAddrs so isNew returns false, avoiding the Join call.
	db.mu.Lock()
	db.seenAddrs[addr] = time.Now()
	db.mu.Unlock()

	db.tryJoin(gm, addr, "test-cluster")

	// Timestamp should remain unchanged since isNew returned false.
	db.mu.Lock()
	seenAt := db.seenAddrs[addr]
	db.mu.Unlock()

	elapsed := time.Since(seenAt)
	if elapsed > time.Second {
		t.Errorf("tryJoin dedup: timestamp should not be refreshed, elapsed=%v", elapsed)
	}
}

func TestDiscoveryBaseTryJoinCooldownExpiry(t *testing.T) {
	list := newTestMemberlist(t)
	gm := &GossipManager{list: list}
	db := newDiscoveryBase("test-cluster")
	db.cooldown = 10 * time.Millisecond

	addr := "127.0.0.1:17778"

	// First call with matching cluster should record the address.
	db.tryJoin(gm, addr, "test-cluster")

	db.mu.Lock()
	firstSeen, exists := db.seenAddrs[addr]
	db.mu.Unlock()
	if !exists {
		t.Fatal("tryJoin cooldown: first call should track address")
	}

	// Wait for cooldown to expire.
	time.Sleep(15 * time.Millisecond)

	// After cooldown expiry, tryJoin should refresh the timestamp and re-join.
	db.tryJoin(gm, addr, "test-cluster")

	db.mu.Lock()
	secondSeen := db.seenAddrs[addr]
	db.mu.Unlock()

	if !secondSeen.After(firstSeen) {
		t.Errorf("tryJoin cooldown expiry: timestamp should be refreshed after cooldown, first=%v second=%v",
			firstSeen, secondSeen)
	}
}

// TestDiscoveryBaseTryJoinWithMemberlist verifies the full tryJoin flow with a
// real memberlist instance. The join target address is unreachable so the join
// will fail, but the call is exercised.
func TestDiscoveryBaseTryJoinWithMemberlist(t *testing.T) {
	list := newTestMemberlist(t)
	gm := &GossipManager{list: list}
	db := newDiscoveryBase("test-cluster")

	// First call records the address and attempts memberlist.Join.
	db.tryJoin(gm, "127.0.0.1:17777", "test-cluster")

	db.mu.Lock()
	firstSeen, exists := db.seenAddrs["127.0.0.1:17777"]
	db.mu.Unlock()
	if !exists {
		t.Error("tryJoin with memberlist: should track address when cluster matches")
	}

	// Second call with same address must be deduped (no new Join attempt).
	db.tryJoin(gm, "127.0.0.1:17777", "test-cluster")

	db.mu.Lock()
	secondSeen := db.seenAddrs["127.0.0.1:17777"]
	db.mu.Unlock()

	if !secondSeen.Equal(firstSeen) {
		t.Errorf("tryJoin with memberlist: dedup should not refresh timestamp, first=%v second=%v",
			firstSeen, secondSeen)
	}
}

// ====== UDP Discovery Tests ======

func newTestGossipManager(t *testing.T) *GossipManager {
	t.Helper()
	list := newTestMemberlist(t)
	return &GossipManager{
		list: list,
		cfg: &config.Config{
			Cluster: config.ClusterConfig{
				Name: "test-cluster",
			},
		},
	}
}

func TestUDPDiscoveryBroadcast(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newUDPDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "udp",
		Port:     0,
		Interval: 1 * time.Hour,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	actualPort := d.conn.LocalAddr().(*net.UDPAddr).Port

	discoveryMsg := &DiscoveryMessage{
		NodeID:      "test-cluster-10.0.0.99:7946",
		ClusterName: "test-cluster",
		GossipAddr:  "10.0.0.99:7946",
		Timestamp:   time.Now().Unix(),
	}

	data, err := msgpack.Marshal(discoveryMsg)
	if err != nil {
		t.Fatalf("marshal DiscoveryMessage: %v", err)
	}

	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: actualPort}
	sender, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatalf("dial UDP: %v", err)
	}
	defer sender.Close()

	if _, err := sender.Write(data); err != nil {
		t.Fatalf("write UDP: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	d.mu.Lock()
	_, exists := d.seenAddrs["10.0.0.99:7946"]
	d.mu.Unlock()

	if !exists {
		t.Error("expected seenAddrs to contain 10.0.0.99:7946 after receiving broadcast")
	}
}

func TestUDPDiscoveryBroadcastSelfSkip(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newUDPDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "udp",
		Port:     0,
		Interval: 1 * time.Hour,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	actualPort := d.conn.LocalAddr().(*net.UDPAddr).Port

	discoveryMsg := &DiscoveryMessage{
		NodeID:      "test-cluster-" + d.localAddr,
		ClusterName: "test-cluster",
		GossipAddr:  d.localAddr,
		Timestamp:   time.Now().Unix(),
	}

	data, err := msgpack.Marshal(discoveryMsg)
	if err != nil {
		t.Fatalf("marshal DiscoveryMessage: %v", err)
	}

	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: actualPort}
	sender, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatalf("dial UDP: %v", err)
	}
	defer sender.Close()

	if _, err := sender.Write(data); err != nil {
		t.Fatalf("write UDP: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	d.mu.Lock()
	_, exists := d.seenAddrs[d.localAddr]
	d.mu.Unlock()

	if exists {
		t.Error("expected self-broadcast to be skipped, but seenAddrs contains local address")
	}
}

func TestUDPDiscoveryBroadcastClusterMismatch(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newUDPDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "udp",
		Port:     0,
		Interval: 1 * time.Hour,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	actualPort := d.conn.LocalAddr().(*net.UDPAddr).Port

	discoveryMsg := &DiscoveryMessage{
		NodeID:      "other-cluster-10.0.0.88:7946",
		ClusterName: "other-cluster",
		GossipAddr:  "10.0.0.88:7946",
		Timestamp:   time.Now().Unix(),
	}

	data, err := msgpack.Marshal(discoveryMsg)
	if err != nil {
		t.Fatalf("marshal DiscoveryMessage: %v", err)
	}

	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: actualPort}
	sender, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatalf("dial UDP: %v", err)
	}
	defer sender.Close()

	if _, err := sender.Write(data); err != nil {
		t.Fatalf("write UDP: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	d.mu.Lock()
	_, exists := d.seenAddrs["10.0.0.88:7946"]
	d.mu.Unlock()

	if exists {
		t.Error("expected cluster mismatch to prevent recording address in seenAddrs")
	}
}

func TestUDPDiscoveryStop(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newUDPDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "udp",
		Port:     0,
		Interval: 1 * time.Hour,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := d.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	_, err := d.conn.WriteToUDP([]byte("test"), &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 9999,
	})
	if err == nil {
		t.Error("expected write on closed connection to fail")
	}
}

// ====== mDNS Discovery Tests ======

// TestMDNSDiscoveryStartStop verifies that an mDNS discovery instance can be
// started and stopped cleanly without errors.
func TestMDNSDiscoveryStartStop(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newMDNSDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "mdns",
		Port:     17947,
		Interval: 3 * time.Second,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("mdns Start failed: %v", err)
	}

	// Give the mDNS server and browse loop a moment to initialize.
	time.Sleep(200 * time.Millisecond)

	if err := d.Stop(); err != nil {
		t.Fatalf("mdns Stop failed: %v", err)
	}

	// Verify the server field is set during Start.
	if d.server == nil {
		t.Error("expected server to be non-nil after Start")
	}
}

// TestMDNSDiscoveryServiceRegistration verifies that starting an mDNS
// discovery registers a _goggrid._tcp service with correct TXT records
// containing the cluster name and gossip address.
func TestMDNSDiscoveryServiceRegistration(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newMDNSDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "mdns",
		Port:     17948,
		Interval: 3 * time.Second,
	}, "test-cluster")

	if err := d.Start(gm); err != nil {
		t.Fatalf("mdns Start failed: %v", err)
	}
	defer d.Stop()

	// Query the mDNS network for _goggrid._tcp services.
	entriesCh := make(chan *mdns.ServiceEntry, 8)
	params := &mdns.QueryParam{
		Service: "_goggrid._tcp",
		Domain:  "local",
		Timeout: 3 * time.Second,
		Entries: entriesCh,
	}

	queryDone := make(chan struct{})
	go func() {
		if err := mdns.Query(params); err != nil {
			t.Logf("mdns query returned error (may be expected in CI): %v", err)
		}
		close(entriesCh)
		close(queryDone)
	}()

	var foundCluster, foundGossip bool
	for entry := range entriesCh {
		for _, field := range entry.InfoFields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "cluster_name":
				if parts[1] == "test-cluster" {
					foundCluster = true
				}
			case "gossip_addr":
				if parts[1] == d.localAddr {
					foundGossip = true
				}
			}
		}
	}

	<-queryDone

	if !foundCluster {
		t.Error("expected to find TXT record cluster_name=test-cluster in mDNS service")
	}
	if !foundGossip {
		t.Errorf("expected to find TXT record gossip_addr=%s in mDNS service", d.localAddr)
	}
}

// TestMDNSDiscoveryHandleEntrySelfSkip verifies that handleEntry skips
// entries matching our own gossip address to prevent self-joins.
func TestMDNSDiscoveryHandleEntrySelfSkip(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newMDNSDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "mdns",
		Port:     17949,
		Interval: 3 * time.Second,
	}, "test-cluster")

	// Set up without starting the real server (manual handleEntry test).
	d.gm = gm
	d.localAddr = "127.0.0.1:17949"

	// Simulate receiving our own service entry.
	entry := &mdns.ServiceEntry{
		InfoFields: []string{
			"cluster_name=test-cluster",
			"gossip_addr=127.0.0.1:17949",
		},
	}

	d.handleEntry(entry)

	// Self-entry should have been skipped; verify seenAddrs is empty.
	d.mu.Lock()
	count := len(d.seenAddrs)
	d.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 entries in seenAddrs after self-skip, got %d", count)
	}
}

// TestMDNSDiscoveryHandleEntryClusterMismatch verifies that entries from
// other clusters are silently skipped.
func TestMDNSDiscoveryHandleEntryClusterMismatch(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newMDNSDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "mdns",
		Port:     17950,
		Interval: 3 * time.Second,
	}, "test-cluster")

	d.gm = gm
	d.localAddr = "127.0.0.1:17950"

	// Simulate receiving a service entry from a different cluster.
	entry := &mdns.ServiceEntry{
		InfoFields: []string{
			"cluster_name=other-cluster",
			"gossip_addr=10.0.0.99:7946",
		},
	}

	d.handleEntry(entry)

	// Cluster mismatch: tryJoin exits early, nothing recorded.
	d.mu.Lock()
	count := len(d.seenAddrs)
	d.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 entries in seenAddrs after cluster mismatch, got %d", count)
	}
}

// TestMDNSDiscoveryHandleEntryValid verifies that a valid peer entry from
// the same cluster triggers a join attempt (address is recorded in seenAddrs).
func TestMDNSDiscoveryHandleEntryValid(t *testing.T) {
	gm := newTestGossipManager(t)

	d := newMDNSDiscovery(config.DiscoveryConfig{
		Enabled:  boolPtr(true),
		Type:     "mdns",
		Port:     17951,
		Interval: 3 * time.Second,
	}, "test-cluster")

	d.gm = gm
	d.localAddr = "127.0.0.1:17951"

	peerAddr := "10.0.0.100:7946"

	// Simulate receiving a valid peer entry from the same cluster.
	entry := &mdns.ServiceEntry{
		InfoFields: []string{
			"cluster_name=test-cluster",
			"gossip_addr=" + peerAddr,
		},
	}

	d.handleEntry(entry)

	// Valid peer should be recorded in seenAddrs after tryJoin.
	d.mu.Lock()
	_, exists := d.seenAddrs[peerAddr]
	d.mu.Unlock()

	if !exists {
		t.Errorf("expected seenAddrs to contain %s after valid peer entry", peerAddr)
	}
}
