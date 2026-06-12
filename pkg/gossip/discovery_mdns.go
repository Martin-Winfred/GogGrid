package gossip

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/hashicorp/mdns"
)

// mdnsDiscovery implements the Discovery interface using mDNS (Multicast DNS).
// It registers a _goggrid._tcp.local. service with TXT records carrying the
// cluster name and gossip address, and periodically browses for peers.
type mdnsDiscovery struct {
	*discoveryBase
	cfg       config.DiscoveryConfig
	server    *mdns.Server
	cancel    context.CancelFunc
	gm        *GossipManager
	localAddr string
}

// newMDNSDiscovery creates a new mDNS-based discovery instance.
func newMDNSDiscovery(cfg config.DiscoveryConfig, clusterName string) *mdnsDiscovery {
	return &mdnsDiscovery{
		discoveryBase: newDiscoveryBase(clusterName),
		cfg:           cfg,
	}
}

// Start registers the _goggrid._tcp service via mDNS and begins browsing for
// peers. The gossip address (IP:port) is extracted from the GossipManager's
// memberlist local node and advertised via TXT records.
func (d *mdnsDiscovery) Start(gm *GossipManager) error {
	d.gm = gm

	localNode := gm.list.LocalNode()
	d.localAddr = localNode.Address()

	host, err := os.Hostname()
	if err != nil {
		host = localNode.Name
	}

	info := []string{
		"cluster_name=" + d.clusterName,
		"gossip_addr=" + d.localAddr,
	}

	service, err := mdns.NewMDNSService(
		host,
		"_goggrid._tcp",
		"",
		"",
		d.cfg.Port,
		nil,
		info,
	)
	if err != nil {
		return fmt.Errorf("create mdns service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return fmt.Errorf("start mdns server: %w", err)
	}
	d.server = server

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	go d.browseLoop(ctx)

	slog.Info("mdns discovery started", "service", "_goggrid._tcp", "gossip_addr", d.localAddr)
	return nil
}

// Stop shuts down the mDNS server and cancels the browse loop.
func (d *mdnsDiscovery) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.server != nil {
		d.server.Shutdown()
	}
	slog.Info("mdns discovery stopped")
	return nil
}

// browseLoop periodically queries for _goggrid._tcp services on the local
// network. Discovered entries are streamed to handleEntry for validation and
// join attempts.
func (d *mdnsDiscovery) browseLoop(ctx context.Context) {
	entriesCh := make(chan *mdns.ServiceEntry, 16)

	// Process incoming entries asynchronously.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-entriesCh:
				if !ok {
					return
				}
				d.handleEntry(entry)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		params := &mdns.QueryParam{
			Service: "_goggrid._tcp",
			Domain:  "local",
			Timeout: 5 * time.Second,
			Entries: entriesCh,
		}

		if err := mdns.QueryContext(ctx, params); err != nil {
			slog.Warn("mdns discovery: query failed", "error", err)
		}

		// Brief pause between queries to avoid flooding the network.
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// handleEntry processes a discovered mDNS service entry. It extracts the
// cluster name and gossip address from TXT records and, if the entry belongs
// to the same cluster and is not our own service, attempts to join the peer
// via discoveryBase.tryJoin.
func (d *mdnsDiscovery) handleEntry(entry *mdns.ServiceEntry) {
	var clusterName, gossipAddr string
	for _, field := range entry.InfoFields {
		for i := 0; i < len(field); i++ {
			if field[i] == '=' {
				key := field[:i]
				val := field[i+1:]
				switch key {
				case "cluster_name":
					clusterName = val
				case "gossip_addr":
					gossipAddr = val
				}
				break
			}
		}
	}

	if gossipAddr == "" {
		return
	}

	// Skip our own service advertisement.
	if gossipAddr == d.localAddr {
		return
	}

	if d.gm == nil {
		return
	}

	d.tryJoin(d.gm, gossipAddr, clusterName)
}
