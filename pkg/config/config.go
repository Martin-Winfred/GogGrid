package config

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration
type Config struct {
	Cluster   ClusterConfig   `yaml:"cluster"`
	Monitor   MonitorConfig   `yaml:"monitor"`
	Storage   StorageConfig   `yaml:"storage"`
	API       APIConfig       `yaml:"api"`
	Gossip    GossipConfig    `yaml:"gossip"`
	Discovery DiscoveryConfig `yaml:"discovery"`
}

// ClusterConfig holds cluster configuration
type ClusterConfig struct {
	Name     string   `yaml:"name"`
	BindAddr string   `yaml:"bind_addr"`
	BindPort int      `yaml:"bind_port"`
	Seeds    []string `yaml:"seeds"`
}

// MonitorConfig holds monitoring configuration
type MonitorConfig struct {
	Interval time.Duration `yaml:"interval"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	DBPath    string        `yaml:"db_path"`
	Retention time.Duration `yaml:"retention"`
}

// WSConfig holds WebSocket configuration
type WSConfig struct {
	Enabled        *bool    `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// APIConfig holds API configuration
type APIConfig struct {
	Enabled  *bool   `yaml:"enabled"`
	BindAddr string  `yaml:"bind_addr"`
	Port     int     `yaml:"port"`
	Token    string  `yaml:"token"`
	WS       WSConfig `yaml:"ws"`
}

// GossipConfig holds gossip configuration
type GossipConfig struct {
	SyncInterval  time.Duration `yaml:"sync_interval"`
	ProbeInterval time.Duration `yaml:"probe_interval"`
}

// DiscoveryConfig holds node auto-discovery configuration
type DiscoveryConfig struct {
	Enabled  *bool          `yaml:"enabled"`
	Type     string        `yaml:"type"`
	Port     int           `yaml:"port"`
	Interval time.Duration `yaml:"interval"`
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool { return &b }

// DefaultConfig returns config with default values
func DefaultConfig() *Config {
	return &Config{
		Cluster: ClusterConfig{
			Name:     "MyCluster",
			BindAddr: "0.0.0.0",
			BindPort: 7946,
		},
		Monitor: MonitorConfig{
			Interval: 5 * time.Second,
		},
		Storage: StorageConfig{
			DBPath:    "./goggrid.db",
			Retention: 168 * time.Hour,
		},
		API: APIConfig{
			Enabled:  boolPtr(true),
			BindAddr: "0.0.0.0",
			Port:     8080,
			WS: WSConfig{
				Enabled:        boolPtr(false),
				AllowedOrigins: []string{},
			},
		},
		Gossip: GossipConfig{
			SyncInterval:  30 * time.Second,
			ProbeInterval: 5 * time.Second,
		},
		Discovery: DiscoveryConfig{
			Enabled:  boolPtr(true),
			Type:     "udp",
			Port:     7947,
			Interval: 3 * time.Second,
		},
	}
}

// Load loads config from YAML file, merging onto defaults
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	return cfg, nil
}

// LoadConfigFile loads and merges YAML configuration onto cfg.
// Returns error on YAML parse failure; warns and returns nil if the file is not found.
// If configPath is empty, this is a no-op.
func LoadConfigFile(cfg *Config, configPath string) error {
	if configPath == "" {
		return nil
	}
	loaded, err := Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("WARNING: config file not found %s, using defaults", configPath)
			return nil
		}
		return fmt.Errorf("failed to load config file %s: %w", configPath, err)
	}
	mergeConfig(cfg, loaded)
	return nil
}

// ParseFlags applies CLI flag overrides to cfg.
// Flags must have been registered and flag.Parse() called by the caller.
func ParseFlags(cfg *Config, clusterName, bindAddr, apiBind, apiToken, seeds, discoveryEnabled, discoveryType, discoveryPort, discoveryInterval, wsEnabled, wsAllowedOrigins string) {
	if clusterName != "" {
		cfg.Cluster.Name = clusterName
	}
	if bindAddr != "" {
		if host, port, err := net.SplitHostPort(bindAddr); err == nil {
			cfg.Cluster.BindAddr = host
			if p, err := strconv.Atoi(port); err != nil {
				log.Printf("WARNING: invalid bind port %q: %v", port, err)
			} else {
				cfg.Cluster.BindPort = p
			}
		}
	}
	if apiBind != "" {
		if host, port, err := net.SplitHostPort(apiBind); err == nil {
			cfg.API.BindAddr = host
			if p, err := strconv.Atoi(port); err != nil {
				log.Printf("WARNING: invalid API port %q: %v", port, err)
			} else {
				cfg.API.Port = p
			}
		}
	}
	if apiToken != "" {
		cfg.API.Token = apiToken
	}
	if seeds != "" {
		cfg.Cluster.Seeds = splitSeeds(seeds)
	}
	if discoveryEnabled != "" {
		if b, err := strconv.ParseBool(discoveryEnabled); err != nil {
			log.Printf("WARNING: invalid --discovery-enabled %q: %v", discoveryEnabled, err)
		} else {
			*cfg.Discovery.Enabled = b
		}
	}
	if discoveryType != "" {
		cfg.Discovery.Type = discoveryType
	}
	if discoveryPort != "" {
		if p, err := strconv.Atoi(discoveryPort); err != nil {
			log.Printf("WARNING: invalid --discovery-port %q: %v", discoveryPort, err)
		} else {
			cfg.Discovery.Port = p
		}
	}
	if discoveryInterval != "" {
		if d, err := time.ParseDuration(discoveryInterval); err != nil {
			log.Printf("WARNING: invalid --discovery-interval %q: %v", discoveryInterval, err)
		} else {
			cfg.Discovery.Interval = d
		}
	}
	if wsEnabled != "" {
		if b, err := strconv.ParseBool(wsEnabled); err != nil {
			log.Printf("WARNING: invalid --ws-enabled %q: %v", wsEnabled, err)
		} else {
			*cfg.API.WS.Enabled = b
		}
	}
	if wsAllowedOrigins != "" {
		cfg.API.WS.AllowedOrigins = splitSeeds(wsAllowedOrigins)
	}
}

// ApplyEnv overrides config from environment variables
func ApplyEnv(cfg *Config) {
	if v := os.Getenv("GOGGRID_CLUSTER_NAME"); v != "" {
		cfg.Cluster.Name = v
	}
	if v := os.Getenv("GOGGRID_API_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err != nil {
			log.Printf("WARNING: invalid GOGGRID_API_PORT %q: %v", v, err)
		} else {
			cfg.API.Port = p
		}
	}
	if v := os.Getenv("GOGGRID_API_TOKEN"); v != "" {
		cfg.API.Token = v
	}
	if v := os.Getenv("GOGGRID_SEEDS"); v != "" {
		cfg.Cluster.Seeds = splitSeeds(v)
	}
	if v := os.Getenv("GOGGRID_DISCOVERY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err != nil {
			log.Printf("WARNING: invalid GOGGRID_DISCOVERY_ENABLED %q: %v", v, err)
		} else {
			*cfg.Discovery.Enabled = b
		}
	}
	if v := os.Getenv("GOGGRID_DISCOVERY_TYPE"); v != "" {
		cfg.Discovery.Type = v
	}
	if v := os.Getenv("GOGGRID_DISCOVERY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err != nil {
			log.Printf("WARNING: invalid GOGGRID_DISCOVERY_PORT %q: %v", v, err)
		} else {
			cfg.Discovery.Port = p
		}
	}
	if v := os.Getenv("GOGGRID_DISCOVERY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err != nil {
			log.Printf("WARNING: invalid GOGGRID_DISCOVERY_INTERVAL %q: %v", v, err)
		} else {
			cfg.Discovery.Interval = d
		}
	}
	if v := os.Getenv("GOGGRID_WS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err != nil {
			log.Printf("WARNING: invalid GOGGRID_WS_ENABLED %q: %v", v, err)
		} else {
			*cfg.API.WS.Enabled = b
		}
	}
	if v := os.Getenv("GOGGRID_WS_ALLOWED_ORIGINS"); v != "" {
		cfg.API.WS.AllowedOrigins = splitSeeds(v)
	}
}

func mergeConfig(dst, src *Config) {
	if src.Cluster.Name != "" {
		dst.Cluster.Name = src.Cluster.Name
	}
	if src.Cluster.BindAddr != "" {
		dst.Cluster.BindAddr = src.Cluster.BindAddr
	}
	if src.Cluster.BindPort != 0 {
		dst.Cluster.BindPort = src.Cluster.BindPort
	}
	if len(src.Cluster.Seeds) > 0 {
		dst.Cluster.Seeds = src.Cluster.Seeds
	}
	if src.Monitor.Interval != 0 {
		dst.Monitor.Interval = src.Monitor.Interval
	}
	if src.Storage.DBPath != "" {
		dst.Storage.DBPath = src.Storage.DBPath
	}
	if src.Storage.Retention != 0 {
		dst.Storage.Retention = src.Storage.Retention
	}
	if src.API.Enabled != nil {
		*dst.API.Enabled = *src.API.Enabled
	}
	if src.API.BindAddr != "" {
		dst.API.BindAddr = src.API.BindAddr
	}
	if src.API.Port != 0 {
		dst.API.Port = src.API.Port
	}
	if src.API.Token != "" {
		dst.API.Token = src.API.Token
	}
	if src.API.WS.Enabled != nil {
		*dst.API.WS.Enabled = *src.API.WS.Enabled
	}
	if len(src.API.WS.AllowedOrigins) > 0 {
		dst.API.WS.AllowedOrigins = src.API.WS.AllowedOrigins
	}
	if src.Gossip.SyncInterval != 0 {
		dst.Gossip.SyncInterval = src.Gossip.SyncInterval
	}
	if src.Gossip.ProbeInterval != 0 {
		dst.Gossip.ProbeInterval = src.Gossip.ProbeInterval
	}
	if src.Discovery.Type != "" {
		dst.Discovery.Type = src.Discovery.Type
	}
	if src.Discovery.Port != 0 {
		dst.Discovery.Port = src.Discovery.Port
	}
	if src.Discovery.Interval != 0 {
		dst.Discovery.Interval = src.Discovery.Interval
	}
	if src.Discovery.Enabled != nil {
		*dst.Discovery.Enabled = *src.Discovery.Enabled
	}
}

// splitSeeds splits a comma-separated seed string into a slice,
// trimming whitespace and omitting empty entries.
func splitSeeds(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// GenerateDefault writes a default configuration template to path.
// The file is generated only if it does not already exist.
func GenerateDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	cfg := DefaultConfig()
	content := fmt.Sprintf(`# GogGrid configuration — generated automatically on first run.
# Edit and restart to apply changes.

cluster:
  name: "%s"
  bind_addr: "%s"
  bind_port: %d
  seeds: []

monitor:
  interval: %s

storage:
  db_path: "%s"
  retention: %s

api:
  enabled: %t
  bind_addr: "%s"
  port: %d
  token: ""
  ws:
    enabled: %t
    allowed_origins: []

gossip:
  sync_interval: %s
  probe_interval: %s

discovery:
  enabled: %t
  type: "%s"
  port: %d
  interval: %s
`,
		cfg.Cluster.Name,
		cfg.Cluster.BindAddr,
		cfg.Cluster.BindPort,
		cfg.Monitor.Interval,
		cfg.Storage.DBPath,
		cfg.Storage.Retention,
		*cfg.API.Enabled,
		cfg.API.BindAddr,
		cfg.API.Port,
		*cfg.API.WS.Enabled,
		cfg.Gossip.SyncInterval,
		cfg.Gossip.ProbeInterval,
		*cfg.Discovery.Enabled,
		cfg.Discovery.Type,
		cfg.Discovery.Port,
		cfg.Discovery.Interval,
	)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}
