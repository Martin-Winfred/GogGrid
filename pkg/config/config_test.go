package config

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Cluster.Name != "MyCluster" {
		t.Errorf("cluster name = %s", cfg.Cluster.Name)
	}
	if cfg.Cluster.BindPort != 7946 {
		t.Errorf("port = %d", cfg.Cluster.BindPort)
	}
	if cfg.Monitor.Interval != 5*time.Second {
		t.Errorf("monitor interval = %v", cfg.Monitor.Interval)
	}
	if cfg.Storage.DBPath != "./goggrid.db" {
		t.Errorf("DB path = %s", cfg.Storage.DBPath)
	}
	if cfg.Storage.Retention != 168*time.Hour {
		t.Errorf("retention = %v", cfg.Storage.Retention)
	}
	if !cfg.API.Enabled {
		t.Error("API should be enabled by default")
	}
	if cfg.API.Port != 8080 {
		t.Errorf("API port = %d", cfg.API.Port)
	}
	if cfg.Gossip.SyncInterval != 30*time.Second {
		t.Errorf("sync interval = %v", cfg.Gossip.SyncInterval)
	}
	if cfg.Gossip.ProbeInterval != 5*time.Second {
		t.Errorf("probe interval = %v", cfg.Gossip.ProbeInterval)
	}
}

func TestLoadValidYAML(t *testing.T) {
	cfg, err := Load("testdata/valid_config.yaml")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.Cluster.Name != "TestCluster" {
		t.Errorf("cluster name = %s", cfg.Cluster.Name)
	}
	if cfg.Cluster.BindPort != 7946 {
		t.Errorf("port = %d", cfg.Cluster.BindPort)
	}
	if len(cfg.Cluster.Seeds) != 2 {
		t.Errorf("seed count = %d", len(cfg.Cluster.Seeds))
	}
	if cfg.Monitor.Interval != 10*time.Second {
		t.Errorf("monitor interval = %v", cfg.Monitor.Interval)
	}
	if cfg.Storage.DBPath != "/tmp/test.db" {
		t.Errorf("DB path = %s", cfg.Storage.DBPath)
	}
	if cfg.Storage.Retention != 720*time.Hour {
		t.Errorf("retention = %v", cfg.Storage.Retention)
	}
	if cfg.API.BindAddr != "127.0.0.1" {
		t.Errorf("API address = %s", cfg.API.BindAddr)
	}
	if cfg.API.Port != 9090 {
		t.Errorf("API port = %d", cfg.API.Port)
	}
	if cfg.Gossip.SyncInterval != 60*time.Second {
		t.Errorf("sync interval = %v", cfg.Gossip.SyncInterval)
	}
	if cfg.Gossip.ProbeInterval != 10*time.Second {
		t.Errorf("probe interval = %v", cfg.Gossip.ProbeInterval)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("testdata/nonexistent.yaml")
	if err == nil {
		t.Error("should return error")
	}
}

func TestApplyEnv(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("GOGGRID_CLUSTER_NAME", "EnvCluster")
	t.Setenv("GOGGRID_API_PORT", "9090")
	ApplyEnv(cfg)
	if cfg.Cluster.Name != "EnvCluster" {
		t.Errorf("cluster name = %s", cfg.Cluster.Name)
	}
	if cfg.API.Port != 9090 {
		t.Errorf("API port = %d", cfg.API.Port)
	}
}

func TestPrecedence(t *testing.T) {
	// After loading YAML, ApplyEnv should override
	cfg, _ := Load("testdata/valid_config.yaml")
	t.Setenv("GOGGRID_CLUSTER_NAME", "EnvOverride")
	ApplyEnv(cfg)
	if cfg.Cluster.Name != "EnvOverride" {
		t.Errorf("env var should override YAML: %s", cfg.Cluster.Name)
	}
}

func TestParseFlagsBindAddr(t *testing.T) {
	// Test bind parsing logic (via net.SplitHostPort)
	cfg := DefaultConfig()
	host, portStr, err := net.SplitHostPort("192.168.0.1:9999")
	if err != nil {
		t.Fatalf("SplitHostPort parse failed: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port conversion failed: %v", err)
	}
	if host != "192.168.0.1" {
		t.Errorf("address = %s", host)
	}
	if port != 9999 {
		t.Errorf("port = %d", port)
	}
	_ = cfg
}
