package monitor

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/Martin-Winfred/GogGrid/pkg/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	net3 "github.com/shirou/gopsutil/v3/net"
)

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
	NetName          string
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
	// Get host architecture
	hostMonitor.Arch = runtime.GOARCH
	// Get OS type
	hostMonitor.OSInfo = runtime.GOOS
	// Get hostname
	hostMonitor.Hostname, err = os.Hostname()
	if err != nil {
		err = errors.New("can't detect HostInfo")
		return
	}
	// Get kernel version
	hostMonitor.KernelVer, err = host.KernelVersion()
	if err != nil {
		err = errors.New("can not load kernelVerion")
		return
	}
	// Get platform family and version info
	hostMonitor.Platform, hostMonitor.Family, hostMonitor.Version, err = host.PlatformInformation()
	if err != nil {
		err = errors.New("can't load platform version")
		return
	}
	// Get overall CPU usage
	hostMonitor.CPULoad, err = cpu.Percent(time.Second, false)
	if err != nil {
		err = errors.New("unable to get CPU load per sec")
		return
	}
	// Get memory info and usage percentage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		err = errors.New("unable to get memory load per sec")
		return
	}
	hostMonitor.MemUsage = memInfo.UsedPercent
	hostMonitor.MemUsed = memInfo.Used
	hostMonitor.MemTotal = memInfo.Total

	// Get network counters
	netCounters, err := net3.IOCounters(true)
	if err != nil {
		err = errors.New("unable to get network counters")
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
	hostMonitor.LocalIP, err = getLocalIP()
	if err != nil {
		err = fmt.Errorf("unable to get local IP: %w", err)
		return
	}

	// Get disk usage
	diskUsage, err := disk.Usage("/")
	if err != nil {
		err = errors.New("unable to get disk usage")
		return
	}
	hostMonitor.DiskUsage = diskUsage.UsedPercent

	// Get system load
	loadAvg, err := load.Avg()
	if err != nil {
		err = errors.New("unable to get load average")
		return
	}
	hostMonitor.LoadAvg1min = loadAvg.Load1
	hostMonitor.LoadAvg5min = loadAvg.Load5
	hostMonitor.LoadAvg15min = loadAvg.Load15

	// Get first non-loopback network interface name
	hostMonitor.NetInterfaceName = getNonLoopbackInterface()

	return hostMonitor, nil
}

// getLocalIP returns the local outbound IP address
func getLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("failed to dial: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
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
		Version:     1,
	}
}
