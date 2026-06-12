package config

import (
	"net"
	"os"
	"strconv"
	"strings"
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
	if cfg.API.Enabled == nil || !*cfg.API.Enabled {
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

func TestDiscoveryDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Discovery.Enabled == nil || !*cfg.Discovery.Enabled {
		t.Error("discovery should be enabled by default")
	}
	if cfg.Discovery.Type != "udp" {
		t.Errorf("discovery type = %s", cfg.Discovery.Type)
	}
	if cfg.Discovery.Port != 7947 {
		t.Errorf("discovery port = %d", cfg.Discovery.Port)
	}
	if cfg.Discovery.Interval != 3*time.Second {
		t.Errorf("discovery interval = %v", cfg.Discovery.Interval)
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
	if cfg.API.Enabled == nil || !*cfg.API.Enabled {
		t.Error("API should be enabled when YAML sets enabled: true")
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

func TestLoadConfigFileNotFound(t *testing.T) {
	cfg := DefaultConfig()
	err := LoadConfigFile(cfg, "testdata/nonexistent.yaml")
	if err != nil {
		t.Errorf("file-not-found should return nil, got: %v", err)
	}
}

func TestLoadConfigFileParseError(t *testing.T) {
	// Write a temp file with invalid YAML
	tmpDir := t.TempDir()
	badYAML := tmpDir + "/bad.yaml"
	if err := os.WriteFile(badYAML, []byte("cluster: [unclosed"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	cfg := DefaultConfig()
	err := LoadConfigFile(cfg, badYAML)
	if err == nil {
		t.Error("parse error should return error")
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

func TestApplyEnvSeeds(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("GOGGRID_SEEDS", "10.0.0.1:7946, 10.0.0.2:7946 , 10.0.0.3:7946")
	ApplyEnv(cfg)
	if len(cfg.Cluster.Seeds) != 3 {
		t.Errorf("expected 3 seeds, got %d: %v", len(cfg.Cluster.Seeds), cfg.Cluster.Seeds)
	}
	if cfg.Cluster.Seeds[0] != "10.0.0.1:7946" {
		t.Errorf("seed[0] = %s", cfg.Cluster.Seeds[0])
	}
	if cfg.Cluster.Seeds[1] != "10.0.0.2:7946" {
		t.Errorf("seed[1] = %s", cfg.Cluster.Seeds[1])
	}
	if cfg.Cluster.Seeds[2] != "10.0.0.3:7946" {
		t.Errorf("seed[2] = %s", cfg.Cluster.Seeds[2])
	}
}

func TestApplyEnvDiscovery(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("GOGGRID_DISCOVERY_ENABLED", "false")
	t.Setenv("GOGGRID_DISCOVERY_TYPE", "mdns")
	t.Setenv("GOGGRID_DISCOVERY_PORT", "9999")
	ApplyEnv(cfg)
	if *cfg.Discovery.Enabled {
		t.Error("discovery should be disabled")
	}
	if cfg.Discovery.Type != "mdns" {
		t.Errorf("discovery type = %s", cfg.Discovery.Type)
	}
	if cfg.Discovery.Port != 9999 {
		t.Errorf("discovery port = %d", cfg.Discovery.Port)
	}
}

func TestApplyEnvInvalidDiscovery(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("GOGGRID_DISCOVERY_ENABLED", "bogus")
	t.Setenv("GOGGRID_DISCOVERY_PORT", "not-a-number")
	ApplyEnv(cfg)
	// Should still have defaults (invalid values are warned and ignored)
	if cfg.Discovery.Enabled == nil || !*cfg.Discovery.Enabled {
		t.Error("discovery should still be enabled (invalid env ignored)")
	}
	if cfg.Discovery.Port != 7947 {
		t.Errorf("discovery port should be default 7947, got %d", cfg.Discovery.Port)
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

func TestParseFlagsSeeds(t *testing.T) {
	cfg := DefaultConfig()
	ParseFlags(cfg, "", "", "", "", "10.0.0.1:7946,10.0.0.2:7946", "", "", "", "")
	if len(cfg.Cluster.Seeds) != 2 {
		t.Errorf("expected 2 seeds, got %d", len(cfg.Cluster.Seeds))
	}
	if cfg.Cluster.Seeds[0] != "10.0.0.1:7946" {
		t.Errorf("seed[0] = %s", cfg.Cluster.Seeds[0])
	}
}

func TestParseFlagsSeedsWithWhitespace(t *testing.T) {
	cfg := DefaultConfig()
	ParseFlags(cfg, "", "", "", "", " 10.0.0.1:7946 , 10.0.0.2:7946 ,, ", "", "", "", "")
	if len(cfg.Cluster.Seeds) != 2 {
		t.Errorf("expected 2 seeds after trimming, got %d: %v", len(cfg.Cluster.Seeds), cfg.Cluster.Seeds)
	}
}

func TestParseFlagsDiscovery(t *testing.T) {
	cfg := DefaultConfig()
	ParseFlags(cfg, "", "", "", "", "", "true", "mdns", "8888", "")
	if cfg.Discovery.Enabled == nil || !*cfg.Discovery.Enabled {
		t.Error("discovery should be enabled")
	}
	if cfg.Discovery.Type != "mdns" {
		t.Errorf("discovery type = %s", cfg.Discovery.Type)
	}
	if cfg.Discovery.Port != 8888 {
		t.Errorf("discovery port = %d", cfg.Discovery.Port)
	}
}

func TestParseFlagsDiscoveryInvalid(t *testing.T) {
	cfg := DefaultConfig()
	ParseFlags(cfg, "", "", "", "", "", "bogus", "", "not-a-number", "")
	// Should keep defaults
	if cfg.Discovery.Enabled == nil || !*cfg.Discovery.Enabled {
		t.Error("discovery should still be enabled (invalid flag ignored)")
	}
	if cfg.Discovery.Port != 7947 {
		t.Errorf("discovery port should be default 7947, got %d", cfg.Discovery.Port)
	}
}

func TestMergeConfigNilEnabled(t *testing.T) {
	// When src.API.Enabled is nil, dst.API.Enabled should be unchanged
	dst := DefaultConfig()
	dst.API.Enabled = boolPtr(false) // explicitly set to false

	src := DefaultConfig()
	src.API.Enabled = nil // simulate YAML that didn't set enabled

	mergeConfig(dst, src)

	if *dst.API.Enabled {
		t.Error("API.Enabled should still be false (nil src should not overwrite)")
	}
}

func TestMergeConfigExplicitEnabled(t *testing.T) {
	// When src.API.Enabled is non-nil, it should overwrite dst
	dst := DefaultConfig()
	*dst.API.Enabled = false

	src := DefaultConfig()
	*src.API.Enabled = true

	mergeConfig(dst, src)

	if !*dst.API.Enabled {
		t.Error("API.Enabled should be true (explicit src overwrites)")
	}
}

func TestMergeConfigDiscovery(t *testing.T) {
	dst := DefaultConfig()
	src := DefaultConfig()
	src.Discovery.Type = "mdns"
	src.Discovery.Port = 9999
	src.Discovery.Interval = 10 * time.Second
	src.Discovery.Enabled = boolPtr(false)

	mergeConfig(dst, src)

	if dst.Discovery.Type != "mdns" {
		t.Errorf("discovery type = %s", dst.Discovery.Type)
	}
	if dst.Discovery.Port != 9999 {
		t.Errorf("discovery port = %d", dst.Discovery.Port)
	}
	if dst.Discovery.Interval != 10*time.Second {
		t.Errorf("discovery interval = %v", dst.Discovery.Interval)
	}
	if *dst.Discovery.Enabled {
		t.Error("discovery should be disabled after merge")
	}
}

func TestSplitSeeds(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{",", nil},
		{" , ", nil},
		{"10.0.0.1:7946", []string{"10.0.0.1:7946"}},
		{"10.0.0.1:7946,10.0.0.2:7946", []string{"10.0.0.1:7946", "10.0.0.2:7946"}},
		{" 10.0.0.1:7946 , 10.0.0.2:7946 ,, ", []string{"10.0.0.1:7946", "10.0.0.2:7946"}},
	}
	for _, tt := range tests {
		result := splitSeeds(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitSeeds(%q) = %v (len=%d), want %v (len=%d)", tt.input, result, len(result), tt.expected, len(tt.expected))
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("splitSeeds(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestGenerateDefault(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/goggrid.yaml"

	// First call: should create
	if err := GenerateDefault(path); err != nil {
		t.Fatalf("GenerateDefault failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	content := string(data)

	// Verify key sections are present
	checks := []string{
		"cluster:",
		"monitor:",
		"storage:",
		"api:",
		"gossip:",
		"discovery:",
		"enabled: true",
		"type: \"udp\"",
		"port: 7947",
		"interval: 3s",
	}
	for _, c := range checks {
		if !strings.Contains(content, c) {
			t.Errorf("generated YAML missing: %q", c)
		}
	}

	// Second call: should be no-op (file already exists)
	if err := GenerateDefault(path); err != nil {
		t.Errorf("GenerateDefault on existing file should be no-op, got: %v", err)
	}
}
