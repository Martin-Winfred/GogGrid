package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// wsHub WebSocket connection hub
type wsHub struct {
	clients    map[*wsClient]bool
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
	stateMgr   *state.StateManager
	stopCh     chan struct{}
}

// wsClient single WebSocket connection
type wsClient struct {
	conn *websocket.Conn
	send chan []byte
	hub  *wsHub
}

func newWSHub(stateMgr *state.StateManager) *wsHub {
	return &wsHub{
		clients:    make(map[*wsClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		stateMgr:   stateMgr,
		stopCh:     make(chan struct{}),
	}
}

// Run starts WebSocket hub main loop
func (h *wsHub) Run() {
	// Subscribe to state changes
	ch := h.stateMgr.Subscribe()
	defer h.stateMgr.Unsubscribe(ch)

	// Forward state changes to broadcast
	go func() {
		for event := range ch {
			data, err := json.Marshal(event)
			if err != nil {
				slog.Warn("WebSocket event marshal failed", "error", err)
				continue
			}
			select {
			case h.broadcast <- data:
			case <-h.stopCh:
				return
			}
		}
	}()

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			slog.Info("WebSocket client connected", "total", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				slog.Info("WebSocket client disconnected", "total", len(h.clients))
			}
		case message := <-h.broadcast:
			var stale []*wsClient
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					stale = append(stale, client)
				}
			}
			for _, client := range stale {
				delete(h.clients, client)
			}
		case <-h.stopCh:
			return
		}
	}
}

func (h *wsHub) Stop() {
	select {
	case <-h.stopCh:
	default:
		close(h.stopCh)
	}
}

// readPump reads WebSocket messages (handles ping/pong/close)
func (c *wsClient) readPump() {
	defer func() { c.hub.unregister <- c; c.conn.Close() }()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// writePump writes WebSocket messages
func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() { ticker.Stop(); c.conn.Close() }()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					slog.Warn("WebSocket close message write failed", "error", err)
				}
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
