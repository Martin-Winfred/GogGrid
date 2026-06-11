<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/license-GPL--3.0-blue.svg" alt="License">
  <img src="https://img.shields.io/badge/database-SQLite-003B57?style=flat&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/gossip-memberlist-844FBA?style=flat&logo=consul" alt="Memberlist">
</p>

<h1 align="center">GogGrid</h1>

<p align="center">
  <b>Decentralized Cluster Monitoring · Zero Central Server · Gossip-Powered</b>
</p>

<p align="center">
  GogGrid is a lightweight, peer-to-peer cluster monitoring system written in Go.<br>
  Every node holds a complete view of the cluster — synchronized via gossip protocol.<br>
  No master. No leader. Just peers.
</p>

---


## Features

| Feature | Detail |
|---------|--------|
| 🔄 **Decentralized** | Every node holds full cluster state — no single point of failure |
| 📡 **Gossip Protocol** | Auto-discovery, state propagation, failure detection via [hashicorp/memberlist](https://github.com/hashicorp/memberlist) |
| 📊 **System Metrics** | CPU, memory, disk, load averages, network I/O per node |
| 🕐 **LWW Conflict Resolution** | Last-Writer-Wins with scalar version (VectorClock reserved for future fast-sync) |
| 🔒 **API Token Auth** | Optional Bearer token authentication for REST API and WebSocket |
| 🌐 **REST API** | Cluster state, node details, time-series history queries |
| 🔌 **WebSocket Push** | Real-time updates on node state changes |
| 💾 **SQLite Persistence** | Embedded database, configurable history retention |
| 🛡️ **Graceful Shutdown** | SIGINT/SIGTERM → LIFO cleanup (API → Gossip → Storage) |

## Quick Start

### Prerequisites

- **Go** 1.21 or later
- **GCC** (required for SQLite CGO)

### Install

```bash
git clone https://github.com/Martin-Winfred/GogGrid.git
cd GogGrid
go build -o goggrid ./cmd/GogGrid
```

### Run

**Start a seed node** (the first node in a new cluster):

```bash
./goggrid \
  --cluster-name MyCluster \
  --bind 0.0.0.0:7946 \
  --api-bind 0.0.0.0:8080
```

**Join an existing cluster:**

```bash
./goggrid \
  --cluster-name MyCluster \
  --bind 0.0.0.0:7947 \
  --api-bind 0.0.0.0:8081 \
  --config config.yaml
```

<details>
<summary>Example <code>config.yaml</code></summary>

```yaml
cluster:
  name: "MyCluster"
  bind_addr: "0.0.0.0"
  bind_port: 7946
  seeds:
    - "10.0.0.1:7946"
    - "10.0.0.2:7946"

monitor:
  interval: 5s

storage:
  db_path: "./goggrid.db"
  retention: 168h

api:
  enabled: true
  bind_addr: "0.0.0.0"
  port: 8080
  token: ""

gossip:
  sync_interval: 30s
  probe_interval: 5s
```
</details>

## Configuration

Three layers, applied in order (later overrides earlier):

```
CLI flags  >  Environment variables  >  YAML file  >  Defaults
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | — | Path to YAML config file |
| `--cluster-name` | `MyCluster` | Cluster name |
| `--bind` | `0.0.0.0:7946` | Gossip bind address |
| `--api-bind` | `0.0.0.0:8080` | API bind address |
| `--api-token` | — | API authentication token (empty = no auth) |

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `GOGGRID_CLUSTER_NAME` | `cluster.name` |
| `GOGGRID_API_PORT` | `api.port` |
| `GOGGRID_API_TOKEN` | `api.token` |

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/health` | Health check — returns node ID |
| `GET` | `/api/cluster` | Full cluster state with all nodes |
| `GET` | `/api/nodes` | Flat list of all node states |
| `GET` | `/api/nodes/{id}` | Single node detail |
| `GET` | `/api/nodes/{id}/history?since=&until=` | Time-series history (RFC 3339) |
| `GET` | `/ws` | WebSocket — real-time `NodeChangeEvent` push |

### Sample Response

```bash
curl http://localhost:8080/api/cluster | jq
```

```json
{
  "cluster_name": "MyCluster",
  "nodes": {
    "node1": {
      "node_id": "node1",
      "ip_address": "192.168.0.1",
      "status": "active",
      "system_type": "linux",
      "cpu_usage": 12.5,
      "memory_usage": 45.3,
      "disk_usage": 32.1,
      "network_interface": {
        "interface_name": "eth0",
        "speed": 1000,
        "rx_bytes": 1234567890,
        "tx_bytes": 987654321
      },
      "system_load": {
        "load_avg_1min": 0.8,
        "load_avg_5min": 0.6,
        "load_avg_15min": 0.5
      },
      "last_seen": "2026-06-08T00:00:00Z",
      "version": 42
    },
    "node2": {
      "node_id": "node2",
      "ip_address": "192.168.0.2",
      "status": "active",
      "cpu_usage": 8.3,
      "memory_usage": 62.1,
      "disk_usage": 18.7,
      "network_interface": {
        "interface_name": "eth0",
        "speed": 1000,
        "rx_bytes": 2345678901,
        "tx_bytes": 1987654321
      },
      "system_load": {
        "load_avg_1min": 0.3,
        "load_avg_5min": 0.4,
        "load_avg_15min": 0.5
      },
      "last_seen": "2026-06-08T00:00:01Z",
      "version": 37
    }
  },
  "local_node_id": "node1",
  "updated_at": "2026-06-08T00:00:01Z"
}
```

## Project Structure

```
cmd/GogGrid/main.go          Entrypoint — wiring + lifecycle
pkg/
├── api/                     HTTP REST + WebSocket
│   ├── api.go               Endpoints + middleware
│   └── websocket.go         Hub + client management
├── config/config.go         YAML + CLI + env vars
├── gossip/                  Memberlist-based gossip
│   ├── gossip.go            GossipManager + Delegate
│   └── message.go           Msgpack message types
├── models/models.go         Shared types + VectorClock
├── monitor/monitor.go       System metrics (gopsutil)
├── state/state.go           In-memory cluster state + LWW
└── storage/storage.go       GORM + SQLite persistence
```

## Development

```bash
# Run all tests (SQLite requires CGO)
CGO_ENABLED=1 go test ./... -v -count=1

# Race detection on state manager
go test ./pkg/state -race -count=1

# Build
go build -o goggrid ./cmd/GogGrid
```

## Tech Stack

| Layer | Library | Purpose |
|-------|---------|---------|
| Metrics | [gopsutil](https://github.com/shirou/gopsutil) | CPU, memory, disk, load, network |
| Gossip | [memberlist](https://github.com/hashicorp/memberlist) | Node discovery + state propagation |
| Serialization | [msgpack](https://github.com/vmihailenco/msgpack) | Efficient binary encoding |
| Database | [GORM](https://gorm.io) + SQLite | Embedded persistence |
| WebSocket | [gorilla/websocket](https://github.com/gorilla/websocket) | Real-time push |
| Config | [yaml.v3](https://gopkg.in/yaml.v3) | YAML parsing |

## License

[GPL-3.0](LICENSE)
