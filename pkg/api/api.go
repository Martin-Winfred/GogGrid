package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/config"
	"github.com/Martin-Winfred/GogGrid/pkg/state"
	"github.com/Martin-Winfred/GogGrid/pkg/storage"
)

// APIServer HTTP API server
type APIServer struct {
	cfg      *config.Config
	stateMgr *state.StateManager
	store    *storage.Storage
	srv      *http.Server
	wsHub    *wsHub
}

// New creates API server
func New(cfg *config.Config, stateMgr *state.StateManager, store *storage.Storage) *APIServer {
	a := &APIServer{
		cfg:      cfg,
		stateMgr: stateMgr,
		store:    store,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", a.handleHealth)
	mux.HandleFunc("/api/cluster", a.handleCluster)
	mux.HandleFunc("/api/nodes", a.handleNodes)
	mux.HandleFunc("/api/nodes/", a.handleNodeDetail) // matches /api/nodes/{id} and /api/nodes/{id}/history
	if cfg.API.WS.Enabled != nil && *cfg.API.WS.Enabled {
		a.wsHub = newWSHub(stateMgr, cfg.API.WS.AllowedOrigins)
		mux.HandleFunc("/ws", a.handleWebSocket)
	}
	// Middleware wrapping
	handler := authMiddleware(cfg.API.Token)(loggingMiddleware(corsMiddleware(recoveryMiddleware(mux))))
	a.srv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.API.BindAddr, cfg.API.Port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return a
}

// Start starts API service
func (a *APIServer) Start() error {
	slog.Info("API service starting", "addr", a.srv.Addr)
	if a.wsHub != nil {
		go a.wsHub.Run()
	}
	return a.srv.ListenAndServe()
}

// Stop gracefully shuts down
func (a *APIServer) Stop(ctx context.Context) error {
	if a.wsHub != nil {
		a.wsHub.Stop()
	}
	return a.srv.Shutdown(ctx)
}

// handleHealth GET /api/health
func (a *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	cs := a.stateMgr.GetClusterState()
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"node_id": cs.LocalNodeID,
	})
}

// handleCluster GET /api/cluster
func (a *APIServer) handleCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	cs := a.stateMgr.GetClusterState()
	writeJSON(w, http.StatusOK, cs)
}

// handleNodes GET /api/nodes
func (a *APIServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	cs := a.stateMgr.GetClusterState()
	nodes := make([]any, 0, len(cs.Nodes))
	for _, ns := range cs.Nodes {
		nodes = append(nodes, ns)
	}
	writeJSON(w, http.StatusOK, nodes)
}

// handleNodeDetail handles /api/nodes/{id} and /api/nodes/{id}/history
func (a *APIServer) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	// Parse path: /api/nodes/{id} or /api/nodes/{id}/history
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.SplitN(path, "/", 2)
	nodeID := parts[0]
	isHistory := len(parts) == 2 && parts[1] == "history"

	if isHistory {
		a.handleNodeHistory(w, r, nodeID)
	} else if len(parts) == 1 {
		ns, ok := a.stateMgr.GetNode(nodeID)
		if !ok {
			// Try reading from storage
			stored, err := a.store.GetNodeState(nodeID)
			if err != nil {
				http.Error(w, `{"error":"node not found"}`, 404)
				return
			}
			writeJSON(w, http.StatusOK, stored)
			return
		}
		writeJSON(w, http.StatusOK, ns)
	} else {
		http.Error(w, "Not Found", 404)
	}
}

// handleNodeHistory GET /api/nodes/{id}/history?since=...&until=...
func (a *APIServer) handleNodeHistory(w http.ResponseWriter, r *http.Request, nodeID string) {
	var since, until time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err != nil {
			slog.Warn("invalid since parameter, ignoring", "value", s, "error", err)
		} else {
			since = t
		}
	}
	if u := r.URL.Query().Get("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err != nil {
			slog.Warn("invalid until parameter, ignoring", "value", u, "error", err)
		} else {
			until = t
		}
	}
	records, err := a.store.GetNodeHistory(nodeID, since, until)
	if err != nil {
		slog.Warn("node history query failed", "error", err)
		http.Error(w, `{"error":"internal server error"}`, 500)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

// handleWebSocket handles WebSocket upgrade
func (a *APIServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.cfg.API.Token != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != a.cfg.API.Token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}
	conn, err := a.wsHub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("WebSocket upgrade failed", "error", err)
		return
	}
	client := &wsClient{conn: conn, send: make(chan []byte, 256), hub: a.wsHub}
	a.wsHub.register <- client
	go client.writePump()
	go client.readPump()
}

// writeJSON writes JSON response
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Middleware

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("HTTP", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovery", "error", err)
				http.Error(w, "Internal Server Error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
