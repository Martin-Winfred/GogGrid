package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	net3 "github.com/shirou/gopsutil/v3/net"
)

var (
	staticOnce      sync.Once
	cachedHostname  string
	cachedKernelVer string
	cachedPlatform  string
	cachedFamily    string
	cachedVersion   string

	// cpuPercentCache holds the most recent CPU utilization(s) collected
	// by the background sampler goroutine started via StartCPUSampler.
	cpuPercentCache []float64
	cpuMu           sync.RWMutex
)

// StartCPUSampler launches a background goroutine that periodically
// samples CPU utilization and caches the result. This allows
// GetHostMonitor() to return immediately without blocking on cpu.Percent.
// Call ctx cancel to stop the sampler.
func StartCPUSampler(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Collect first sample immediately so there is data available
		// on the very first GetHostMonitor() call.
		samples, err := cpu.Percent(0, false)
		if err != nil {
			slog.Warn("CPU percent sample failed", "error", err)
		} else {
			cpuMu.Lock()
			cpuPercentCache = samples
			cpuMu.Unlock()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				samples, err := cpu.Percent(0, false)
				if err != nil {
					slog.Warn("CPU percent sample failed", "error", err)
					continue
				}
				cpuMu.Lock()
				cpuPercentCache = samples
				cpuMu.Unlock()
			}
		}
	}()
}

// HostMonitor holds system monitoring data
type HostMonitor struct {
	Arch             string
	OSInfo           string
	Hostname         string
	KernelVer        string
	Version          string
	Platform         string
	Family           string
	CPULoad          []float64
	MemUsage         float64
	MemUsed          uint64
	MemTotal         uint64

	BytesRecv        uint64
	BytesSent        uint64
	LocalIP          string
	DiskUsage        float64  `json:"disk_usage"`
	LoadAvg1min      float64  `json:"load_avg_1min"`
	LoadAvg5min      float64  `json:"load_avg_5min"`
	LoadAvg15min     float64  `json:"load_avg_15min"`
	NetInterfaceName string   `json:"net_interface_name"`
}

// GetHostMonitor returns system monitoring data
func GetHostMonitor() (hostMonitor HostMonitor, err error) {
	staticOnce.Do(func() {
		var err error
		cachedHostname, err = os.Hostname()
		if err != nil {
			slog.Warn("hostname lookup failed", "error", err)
		}
		cachedKernelVer, err = host.KernelVersion()
		if err != nil {
			slog.Warn("kernel version lookup failed", "error", err)
		}
		cachedPlatform, cachedFamily, cachedVersion, err = host.PlatformInformation()
		if err != nil {
			slog.Warn("platform information lookup failed", "error", err)
		}
	})

	hostMonitor.Arch = runtime.GOARCH
	hostMonitor.OSInfo = runtime.GOOS
	hostMonitor.Hostname = cachedHostname
	hostMonitor.KernelVer = cachedKernelVer
	hostMonitor.Platform = cachedPlatform
	hostMonitor.Family = cachedFamily
	hostMonitor.Version = cachedVersion
	// Get overall CPU usage from background sampler cache
	cpuMu.RLock()
	if len(cpuPercentCache) > 0 {
		hostMonitor.CPULoad = make([]float64, len(cpuPercentCache))
		copy(hostMonitor.CPULoad, cpuPercentCache)
	}
	cpuMu.RUnlock()
	// Get memory info and usage percentage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		err = fmt.Errorf("unable to get memory load per sec: %w", err)
		return
	}
	hostMonitor.MemUsage = memInfo.UsedPercent
	hostMonitor.MemUsed = memInfo.Used
	hostMonitor.MemTotal = memInfo.Total

	// Get network counters
	netCounters, err := net3.IOCounters(true)
	if err != nil {
		err = fmt.Errorf("unable to get network counters: %w", err)
		return
	}

	var totalBytesRecv, totalBytesSent uint64
	for _, counter := range netCounters {
		totalBytesRecv += counter.BytesRecv
		totalBytesSent += counter.BytesSent
	}
	hostMonitor.BytesRecv = totalBytesRecv
	hostMonitor.BytesSent = totalBytesSent

	// Get local IP address
	hostMonitor.LocalIP, err = GetLocalIP()
	if err != nil {
		err = fmt.Errorf("unable to get local IP: %w", err)
		return
	}

	// Get disk usage
	diskUsage, err := disk.Usage("/")
	if err != nil {
		err = fmt.Errorf("unable to get disk usage: %w", err)
		return
	}
	hostMonitor.DiskUsage = diskUsage.UsedPercent

	// Get system load
	loadAvg, err := load.Avg()
	if err != nil {
		err = fmt.Errorf("unable to get load average: %w", err)
		return
	}
	hostMonitor.LoadAvg1min = loadAvg.Load1
	hostMonitor.LoadAvg5min = loadAvg.Load5
	hostMonitor.LoadAvg15min = loadAvg.Load15

	// Get first non-loopback network interface name
	hostMonitor.NetInterfaceName = getNonLoopbackInterface()

	return hostMonitor, nil
}

// GetLocalIP returns the local outbound IP address.
// It first enumerates network interfaces to find a non-loopback IPv4
// address. If that fails, it falls back to a UDP dial to determine the
// outbound IP used for the default route.
func GetLocalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok {
					if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
						return ip4.String(), nil
					}
				}
			}
		}
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("failed to determine local IP: %w", err)
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected local address type: %T", conn.LocalAddr())
	}
	return localAddr.IP.String(), nil
}

// getNonLoopbackInterface returns the first non-loopback interface name
func getNonLoopbackInterface() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			return iface.Name
		}
	}
	return ""
}

// ToNodeState converts HostMonitor to NodeState
func (h *HostMonitor) ToNodeState(nodeID string) *models.NodeState {
	now := time.Now()

	cpuUsage := 0.0
	if len(h.CPULoad) > 0 {
		cpuUsage = h.CPULoad[0]
	}

	return &models.NodeState{
		NodeID:      nodeID,
		IPAddress:   h.LocalIP,
		Status:      "active",
		SystemType:  h.OSInfo,
		CPUUsage:    cpuUsage,
		MemoryUsage: h.MemUsage,
		DiskUsage:   h.DiskUsage,
		NetInterface: models.NetInterface{
			InterfaceName: h.NetInterfaceName,
			RxBytes:       h.BytesRecv,
			TxBytes:       h.BytesSent,
		},
		SystemLoad: models.SystemLoad{
			LoadAvg1min:  h.LoadAvg1min,
			LoadAvg5min:  h.LoadAvg5min,
			LoadAvg15min: h.LoadAvg15min,
		},
		LastSeen:    now,
		LastUpdated: now,
		// Version is managed by collectAndPublish, initialized to 0 here
	}
}
