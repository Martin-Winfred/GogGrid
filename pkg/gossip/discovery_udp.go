package gossip

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/vmihailenco/msgpack/v5"
)

// udpDiscovery implements Discovery via UDP broadcast on the local LAN.
// Nodes periodically broadcast their presence to 255.255.255.255 and listen
// for broadcasts from other nodes. When a new peer is discovered, tryJoin
// is called to add it to the memberlist cluster.
type udpDiscovery struct {
	*discoveryBase
	cfg       config.DiscoveryConfig
	conn      *net.UDPConn
	cancel    context.CancelFunc
	gm        *GossipManager
	localAddr string
}

// newUDPDiscovery creates a new UDP-based discovery instance.
func newUDPDiscovery(cfg config.DiscoveryConfig, clusterName string) *udpDiscovery {
	return &udpDiscovery{
		discoveryBase: newDiscoveryBase(clusterName),
		cfg:           cfg,
	}
}

// Start begins the UDP discovery process: opens a UDP listener on the
// configured port and launches broadcast and listen goroutines.
func (d *udpDiscovery) Start(gm *GossipManager) error {
	d.gm = gm
	d.localAddr = net.JoinHostPort(
		gm.list.LocalNode().Addr.String(),
		fmt.Sprintf("%d", gm.list.LocalNode().Port),
	)

	// Validate localAddr is not a zero address that peers cannot route to
	if strings.HasPrefix(d.localAddr, "0.0.0.0:") || strings.HasPrefix(d.localAddr, "[::]:") || strings.HasPrefix(d.localAddr, ":") {
		return fmt.Errorf("udp discovery: invalid local address %q, cannot advertise to peers", d.localAddr)
	}

	addr := &net.UDPAddr{IP: net.IPv4zero, Port: d.cfg.Port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("udp discovery listen on :%d: %w", d.cfg.Port, err)
	}
	d.conn = conn

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	go d.broadcastLoop(ctx)
	go d.listenLoop(ctx)

	slog.Info("udp discovery started", "port", d.cfg.Port, "interval", d.cfg.Interval)
	return nil
}

// Stop shuts down the UDP discovery cleanly by cancelling goroutines and
// closing the connection.
func (d *udpDiscovery) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.conn != nil {
		d.conn.Close()
	}
	slog.Info("udp discovery stopped")
	return nil
}

// broadcastLoop periodically sends a DiscoveryMessage to the broadcast
// address so other nodes on the LAN can discover this node.
func (d *udpDiscovery) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.Interval)
	defer ticker.Stop()

	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: d.cfg.Port}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := DiscoveryMessage{
				NodeID:      d.gm.cfg.Cluster.Name + "-" + d.localAddr,
				ClusterName: d.gm.cfg.Cluster.Name,
				GossipAddr:  d.localAddr,
				Timestamp:   time.Now().Unix(),
			}
			data, err := msgpack.Marshal(msg)
			if err != nil {
				slog.Warn("udp discovery: marshal failed", "error", err)
				continue
			}
			if _, err := d.conn.WriteToUDP(data, bcast); err != nil {
				slog.Warn("udp discovery: broadcast failed", "error", err)
			}
		}
	}
}

// listenLoop receives UDP broadcasts from other nodes, decodes the
// DiscoveryMessage, and calls tryJoin for each newly discovered peer.
func (d *udpDiscovery) listenLoop(ctx context.Context) {
	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("udp discovery: read failed", "error", err)
			continue
		}
		var msg DiscoveryMessage
		if err := msgpack.Unmarshal(buf[:n], &msg); err != nil {
			slog.Warn("udp discovery: unmarshal failed", "from", remoteAddr)
			continue
		}
		// Skip our own broadcasts.
		if msg.GossipAddr == d.localAddr {
			continue
		}
		d.tryJoin(d.gm, msg.GossipAddr, msg.ClusterName)
	}
}
