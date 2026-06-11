package config

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration
type Config struct {
	Cluster ClusterConfig `yaml:"cluster"`
	Monitor MonitorConfig `yaml:"monitor"`
	Storage StorageConfig `yaml:"storage"`
	API     APIConfig     `yaml:"api"`
	Gossip  GossipConfig  `yaml:"gossip"`
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

// APIConfig holds API configuration
type APIConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BindAddr string `yaml:"bind_addr"`
	Port     int    `yaml:"port"`
}

// GossipConfig holds gossip configuration
type GossipConfig struct {
	SyncInterval  time.Duration `yaml:"sync_interval"`
	ProbeInterval time.Duration `yaml:"probe_interval"`
}

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
			Enabled:  true,
			BindAddr: "0.0.0.0",
			Port:     8080,
		},
		Gossip: GossipConfig{
			SyncInterval:  30 * time.Second,
			ProbeInterval: 5 * time.Second,
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

// ParseFlags parses CLI flags and overrides config
// Registers the following flags:
//   --config       config file path
//   --cluster-name cluster name
//   --bind         Gossip bind address (ip:port)
//   --api-bind     API bind address (ip:port)
func ParseFlags(cfg *Config) {
	configPath := flag.String("config", "", "config file path")
	clusterName := flag.String("cluster-name", "", "cluster name")
	bindAddr := flag.String("bind", "", "Gossip bind address (ip:port)")
	apiBind := flag.String("api-bind", "", "API bind address (ip:port)")
	flag.Parse()
	if *configPath != "" {
		loaded, err := Load(*configPath)
		if err != nil {
			log.Printf("WARNING: failed to load config file %s: %v", *configPath, err)
		} else {
			mergeConfig(cfg, loaded)
		}
	}
	if *clusterName != "" {
		cfg.Cluster.Name = *clusterName
	}
	if *bindAddr != "" {
		if host, port, err := net.SplitHostPort(*bindAddr); err == nil {
			cfg.Cluster.BindAddr = host
			if p, err := strconv.Atoi(port); err != nil {
				log.Printf("WARNING: invalid bind port %q: %v", port, err)
			} else {
				cfg.Cluster.BindPort = p
			}
		}
	}
	if *apiBind != "" {
		if host, port, err := net.SplitHostPort(*apiBind); err == nil {
			cfg.API.BindAddr = host
			if p, err := strconv.Atoi(port); err != nil {
				log.Printf("WARNING: invalid API port %q: %v", port, err)
			} else {
				cfg.API.Port = p
			}
		}
	}
}

// ApplyEnv overrides config from environment variables
// GOGGRID_CLUSTER_NAME, GOGGRID_API_PORT
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
	dst.API.Enabled = src.API.Enabled
	if src.API.BindAddr != "" {
		dst.API.BindAddr = src.API.BindAddr
	}
	if src.API.Port != 0 {
		dst.API.Port = src.API.Port
	}
	if src.Gossip.SyncInterval != 0 {
		dst.Gossip.SyncInterval = src.Gossip.SyncInterval
	}
	if src.Gossip.ProbeInterval != 0 {
		dst.Gossip.ProbeInterval = src.Gossip.ProbeInterval
	}
}
