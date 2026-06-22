package gossip

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/Martin-Winfred/GogGrid/pkg/monitor"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

// GossipManager manages memberlist communication
type GossipManager struct {
	list      *memberlist.Memberlist
	stateMgr  *state.StateManager
	cfg       *config.Config
	localNode string
	stopCh    chan struct{}
	broadcast *memberlist.TransmitLimitedQueue
	discovery Discovery // auto-discovery mechanism
	store        *storage.Storage
	pendingSyncs map[string]*pendingSync
	syncMu       sync.Mutex
}

type pendingSync struct {
	reqID  string
	peer   *memberlist.Node
	offset int
	limit  int
	total  int64
	done   chan error
}

// New creates a GossipManager
func New(cfg *config.Config, stateMgr *state.StateManager, store *storage.Storage) (*GossipManager, error) {
	gm := &GossipManager{
		stateMgr:      stateMgr,
		cfg:           cfg,
		stopCh:        make(chan struct{}),
		store:         store,
		pendingSyncs:  make(map[string]*pendingSync),
	}

	// Determine local node identity
	cs := stateMgr.GetClusterState()
	gm.localNode = cs.LocalNodeID

	// memberlist configuration
	mlCfg := memberlist.DefaultLocalConfig()
	mlCfg.Name = gm.localNode
	mlCfg.BindAddr = cfg.Cluster.BindAddr
	mlCfg.BindPort = cfg.Cluster.BindPort
	mlCfg.AdvertiseAddr = cfg.Cluster.BindAddr
	mlCfg.AdvertisePort = cfg.Cluster.BindPort
	// Auto-detect outbound IP when bind is 0.0.0.0 or empty
	if mlCfg.AdvertiseAddr == "" || mlCfg.AdvertiseAddr == "0.0.0.0" {
		if ip, err := monitor.GetLocalIP(); err != nil {
			slog.Warn("failed to detect outbound IP, AdvertiseAddr remains 0.0.0.0", "error", err)
		} else {
			mlCfg.AdvertiseAddr = ip
			slog.Info("auto-detected AdvertiseAddr", "addr", ip)
		}
	}
	mlCfg.ProbeInterval = cfg.Gossip.ProbeInterval
	mlCfg.PushPullInterval = cfg.Gossip.SyncInterval
	mlCfg.Delegate = &goggridDelegate{gm: gm}
	mlCfg.Events = &goggridEventDelegate{gm: gm}

	// Create memberlist
	list, err := memberlist.Create(mlCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}
	gm.list = list

	// Create broadcast queue
	gm.broadcast = &memberlist.TransmitLimitedQueue{
		NumNodes:      func() int { return gm.NumMembers() },
		RetransmitMult: 3,
	}

	return gm, nil
}

// Start starts gossip and joins the cluster
func (g *GossipManager) Start() error {
	seeds := g.cfg.Cluster.Seeds
	if len(seeds) > 0 {
		n, err := g.list.Join(seeds)
		if err != nil {
			slog.Warn("cluster join partially failed", "joined", n, "error", err)
		} else {
			slog.Info("cluster join succeeded", "joined", n, "seeds", seeds)
		}
	} else {
		slog.Info("no seed nodes, relying on auto-discovery")
	}

	// Start auto-discovery if enabled
	if g.cfg.Discovery.Enabled != nil && *g.cfg.Discovery.Enabled {
		switch g.cfg.Discovery.Type {
		case "udp":
			g.discovery = newUDPDiscovery(g.cfg.Discovery, g.cfg.Cluster.Name)
		case "mdns":
			g.discovery = newMDNSDiscovery(g.cfg.Discovery, g.cfg.Cluster.Name)
		default:
			slog.Warn("unknown discovery type, defaulting to udp", "type", g.cfg.Discovery.Type)
			g.discovery = newUDPDiscovery(g.cfg.Discovery, g.cfg.Cluster.Name)
		}
		if err := g.discovery.Start(g); err != nil {
			slog.Warn("failed to start discovery", "error", err)
			g.discovery = nil
		}
	}

	// Start anti-entropy loop
	go g.antiEntropyLoop()

	return nil
}

// Stop stops gossip
func (g *GossipManager) Stop() error {
	// Stop discovery first
	if g.discovery != nil {
		if err := g.discovery.Stop(); err != nil {
			slog.Warn("discovery stop error", "error", err)
		}
		g.discovery = nil
	}

	slog.Info("leaving cluster")
	if err := g.list.Leave(5 * time.Second); err != nil {
		slog.Warn("leave error", "error", err)
	}
	if err := g.list.Shutdown(); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	close(g.stopCh)
	return nil
}

// BroadcastLocalState broadcasts local node state
func (g *GossipManager) BroadcastLocalState(ns *models.NodeState) error {
	payload, err := EncodePayload(&NodeStatePayload{State: ns})
	if err != nil {
		return fmt.Errorf("failed to encode state: %w", err)
	}
	msg := NewGossipMessage(MsgNodeState, g.localNode, payload)
	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}
	g.broadcast.QueueBroadcast(&simpleBroadcast{msg: data, nodeID: ns.NodeID})
	return nil
}

// NumMembers returns the number of online members
func (g *GossipManager) NumMembers() int {
	return g.list.NumMembers()
}

// Members returns the member list
func (g *GossipManager) Members() []*memberlist.Node {
	return g.list.Members()
}

// getClusterStateJSON returns serialized cluster state (using msgpack)
func (g *GossipManager) getClusterStateJSON() []byte {
	cs := g.stateMgr.GetClusterState()
	payload, err := EncodePayload(&ClusterSyncPayload{
		Nodes: mapToSlice(cs.Nodes),
	})
	if err != nil {
		return nil
	}
	msg := NewGossipMessage(MsgClusterSync, g.localNode, payload)
	data, err := EncodeMessage(msg)
	if err != nil {
		slog.Warn("encode message failed during cluster sync", "error", err)
		return nil
	}
	return data
}

// mapToSlice converts node map to slice
func mapToSlice(nodes map[string]*models.NodeState) []*models.NodeState {
	result := make([]*models.NodeState, 0, len(nodes))
	for _, ns := range nodes {
		result = append(result, ns)
	}
	return result
}

// antiEntropyLoop runs the anti-entropy sync loop
func (g *GossipManager) antiEntropyLoop() {
	ticker := time.NewTicker(g.cfg.Gossip.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			members := g.list.Members()
			if len(members) <= 1 {
				continue
			}
			// Randomly select another node
			idx := rand.Intn(len(members))
			target := members[idx]
			if target.Name == g.localNode {
				continue
			}

			state := g.getClusterStateJSON()
			if state == nil {
				continue
			}
			if err := g.list.SendReliable(target, state); err != nil {
				slog.Warn("anti-entropy sync failed", "target", target.Name, "error", err)
			}
		}
	}
}

// ====== memberlist Delegate implementation ======

type goggridDelegate struct {
	gm *GossipManager
}

func (d *goggridDelegate) NodeMeta(limit int) []byte {
	return []byte{}
}

func (d *goggridDelegate) NotifyMsg(msg []byte) {
	d.gm.handleMessage(msg)
}

func (d *goggridDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	return d.gm.broadcast.GetBroadcasts(overhead, limit)
}

func (d *goggridDelegate) LocalState(join bool) []byte {
	return d.gm.getClusterStateJSON()
}

func (d *goggridDelegate) MergeRemoteState(buf []byte, join bool) {
	d.gm.handleMergeRemoteState(buf, join)
}

// ====== memberlist EventDelegate implementation ======

type goggridEventDelegate struct {
	gm *GossipManager
}

func (e *goggridEventDelegate) NotifyJoin(node *memberlist.Node) {
	slog.Info("node joined", "node", node.Name, "addr", node.Addr)
}

func (e *goggridEventDelegate) NotifyLeave(node *memberlist.Node) {
	slog.Info("node left", "node", node.Name, "addr", node.Addr)
	e.gm.stateMgr.MarkNodeInactive(node.Name)
}

func (e *goggridEventDelegate) NotifyUpdate(node *memberlist.Node) {
	// Node metadata update, not handled yet
}

// ====== Message handling ======

func (g *GossipManager) handleMessage(data []byte) {
	msg, err := DecodeMessage(data)
	if err != nil {
		slog.Warn("message decode failed", "error", err)
		return
	}

	switch msg.Type {
	case MsgNodeState:
		var payload NodeStatePayload
		if err := DecodePayload(msg.Payload, &payload); err != nil {
			slog.Warn("node state decode failed", "error", err)
			return
		}
		if payload.State != nil {
			g.stateMgr.MergeNodeState(payload.State)
		}
	case MsgClusterSync:
		var payload ClusterSyncPayload
		if err := DecodePayload(msg.Payload, &payload); err != nil {
			slog.Warn("cluster sync decode failed", "error", err)
			return
		}
		for _, ns := range payload.Nodes {
			g.stateMgr.MergeNodeState(ns)
		}
	case MsgHeartbeat:
		// Heartbeat: no special handling, memberlist handles liveness internally
	case MsgHistoryPullRequest:
		var payload HistoryPullRequestPayload
		if err := DecodePayload(msg.Payload, &payload); err != nil {
			slog.Warn("history pull request decode failed", "error", err)
			return
		}
		g.handleHistoryPullRequest(&payload, msg.FromNode)
	case MsgHistoryPullResponse:
		var payload HistoryPullResponsePayload
		if err := DecodePayload(msg.Payload, &payload); err != nil {
			slog.Warn("history pull response decode failed", "error", err)
			return
		}
		g.handleHistoryPullResponse(&payload)
	}
}

func (g *GossipManager) handleMergeRemoteState(buf []byte, join bool) {
	msg, err := DecodeMessage(buf)
	if err != nil {
		slog.Warn("merge remote state: message decode failed", "error", err)
		return
	}
	if msg.Type == MsgClusterSync {
		var payload ClusterSyncPayload
		if err := DecodePayload(msg.Payload, &payload); err != nil {
			slog.Warn("merge remote state: payload decode failed", "error", err)
			return
		}
		for _, ns := range payload.Nodes {
			g.stateMgr.MergeNodeState(ns)
		}
	}
}

func (g *GossipManager) handleHistoryPullRequest(payload *HistoryPullRequestPayload, fromNode string) {
	since := time.Time{}
	if payload.SinceUnixNano > 0 {
		since = time.Unix(0, payload.SinceUnixNano)
	}
	limit := payload.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	records, err := g.store.GetAllHistorySince(since, payload.Offset, limit)
	if err != nil {
		slog.Warn("history pull: query failed", "error", err, "req_id", payload.RequestID)
		return
	}
	hasMore := len(records) == limit
	respPayload, err := EncodePayload(&HistoryPullResponsePayload{
		RequestID:  payload.RequestID,
		Records:    records,
		HasMore:    hasMore,
		NextOffset: payload.Offset + len(records),
		TotalCount: -1,
	})
	if err != nil {
		slog.Warn("history pull: encode response failed", "error", err)
		return
	}
	msg := NewGossipMessage(MsgHistoryPullResponse, g.localNode, respPayload)
	data, err := EncodeMessage(msg)
	if err != nil {
		slog.Warn("history pull: encode message failed", "error", err)
		return
	}
	for _, member := range g.list.Members() {
		if member.Name == fromNode {
			if err := g.list.SendReliable(member, data); err != nil {
				slog.Warn("history pull: send response failed", "to", fromNode, "error", err)
			}
			return
		}
	}
	slog.Warn("history pull: target node not found", "from", fromNode)
}

func (g *GossipManager) handleHistoryPullResponse(payload *HistoryPullResponsePayload) {
	g.syncMu.Lock()
	ps, ok := g.pendingSyncs[payload.RequestID]
	if !ok {
		g.syncMu.Unlock()
		slog.Warn("history pull response for unknown request", "req_id", payload.RequestID)
		return
	}
	g.syncMu.Unlock()

	inserted, err := g.store.ImportHistoryRecords(payload.Records)
	if err != nil {
		g.failPendingSync(ps, err)
		return
	}
	g.syncMu.Lock()
	ps.total += inserted
	hasMore := payload.HasMore
	if payload.HasMore {
		ps.offset = payload.NextOffset
	}
	g.syncMu.Unlock()

	if hasMore {
		const maxPages = 100
		if ps.offset/ps.limit >= maxPages {
			g.failPendingSync(ps, fmt.Errorf("history sync exceeded max pages (%d)", maxPages))
			return
		}
		g.syncMu.Lock()
		if _, ok := g.pendingSyncs[ps.reqID]; !ok {
			g.syncMu.Unlock()
			return
		}
		g.syncMu.Unlock()
		go g.sendHistoryPullRequest(ps)
	} else {
		latestTime, err := g.store.GetLatestHistoryTime()
		if err == nil && !latestTime.IsZero() {
			g.stateMgr.SetHistorySyncTime(latestTime)
		}
		g.failPendingSync(ps, nil)
		slog.Info("history sync completed", "req_id", payload.RequestID, "total", ps.total)
	}
}

func (g *GossipManager) SyncHistoryOnJoin(ctx context.Context) error {
	members := g.list.Members()
	if len(members) <= 1 {
		slog.Info("history sync: no peers to pull from, skipping")
		return nil
	}

	var peer *memberlist.Node
	for _, m := range members {
		if m.Name != g.localNode {
			peer = m
			break
		}
	}
	if peer == nil {
		return nil
	}

	slog.Info("history sync: starting pull", "peer", peer.Name)
	reqID := fmt.Sprintf("%s-%d", g.localNode, time.Now().UnixNano())

	ps := &pendingSync{
		reqID:  reqID,
		peer:   peer,
		offset: 0,
		limit:  500,
		total:  0,
		done:   make(chan error, 1),
	}
	g.syncMu.Lock()
	g.pendingSyncs[reqID] = ps
	g.syncMu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			g.failPendingSync(ps, ctx.Err())
		default:
			g.sendHistoryPullRequest(ps)
		}
	}()

	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()

	select {
	case err := <-ps.done:
		if err != nil {
			slog.Warn("history sync failed", "error", err)
		}
		return err
	case <-ctx.Done():
		g.syncMu.Lock()
		delete(g.pendingSyncs, reqID)
		g.syncMu.Unlock()
		return ctx.Err()
	case <-timer.C:
		g.syncMu.Lock()
		delete(g.pendingSyncs, reqID)
		g.syncMu.Unlock()
		return fmt.Errorf("history sync timeout after 120s")
	}
}

func (g *GossipManager) sendHistoryPullRequest(ps *pendingSync) {
	sinceNano := int64(0)
	if syncTime := g.stateMgr.GetHistorySyncTime(); !syncTime.IsZero() {
		sinceNano = syncTime.UnixNano()
	}
	g.syncMu.Lock()
	offset := ps.offset
	g.syncMu.Unlock()
	reqPayload, err := EncodePayload(&HistoryPullRequestPayload{
		RequestID:     ps.reqID,
		SinceUnixNano: sinceNano,
		Offset:        offset,
		Limit:         500,
	})
	if err != nil {
		slog.Warn("history pull: encode request failed", "req_id", ps.reqID, "error", err)
		g.failPendingSync(ps, err)
		return
	}
	msg := NewGossipMessage(MsgHistoryPullRequest, g.localNode, reqPayload)
	data, err := EncodeMessage(msg)
	if err != nil {
		slog.Warn("history pull: encode message failed", "req_id", ps.reqID, "error", err)
		g.failPendingSync(ps, err)
		return
	}
	if err := g.list.SendReliable(ps.peer, data); err != nil {
		slog.Warn("history pull: send request failed", "req_id", ps.reqID, "peer", ps.peer.Name, "error", err)
		g.failPendingSync(ps, err)
	}
}

func (g *GossipManager) failPendingSync(ps *pendingSync, err error) {
	g.syncMu.Lock()
	if _, ok := g.pendingSyncs[ps.reqID]; ok {
		delete(g.pendingSyncs, ps.reqID)
		g.syncMu.Unlock()
		ps.done <- err
		close(ps.done)
	} else {
		g.syncMu.Unlock()
	}
}

// ====== Broadcast implementation ======

type simpleBroadcast struct {
	msg    []byte
	nodeID string
}

func (b *simpleBroadcast) Invalidates(other memberlist.Broadcast) bool {
	if o, ok := other.(*simpleBroadcast); ok {
		return b.nodeID == o.nodeID
	}
	return false
}

func (b *simpleBroadcast) Message() []byte {
	return b.msg
}

func (b *simpleBroadcast) Finished() {}
