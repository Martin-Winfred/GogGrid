package monitor

import (
	"errors"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	net3 "github.com/shirou/gopsutil/v3/net"
)

// HostMonitor Stracture
type HostMonitor struct {
	Arch      string
	OSInfo    string
	Hostname  string
	KernelVer string
	Version   string
	Platform  string
	Family    string
	CPULoad   []float64
	MemUsage  float64
	MemUsed   uint64
	MemTotal  uint64
	NetName   string
	BytesRecv uint64
	BytesSent uint64
	LocalIP   string
}

// GetHostMonitor 返回主机监控数据的结构体
func GetHostMonitor() (hostMonitor HostMonitor, err error) {
	//Get host Arch
	hostMonitor.Arch = runtime.GOARCH
	//Get OS type
	hostMonitor.OSInfo = runtime.GOOS
	//Get HostName
	hostMonitor.Hostname, err = os.Hostname()
	if err != nil {
		err = errors.New("can't detect HostInfo")
		return
	}
	//get Kernel Version
	hostMonitor.KernelVer, err = host.KernelVersion()
	if err != nil {
		err = errors.New("can not load kernelVerion")
		return
	}
	//System faiily and version info
	hostMonitor.Platform, hostMonitor.Family, hostMonitor.Version, err = host.PlatformInformation()
	if err != nil {
		err = errors.New("can't load platform version")
		return
	}
	//Get overall CPu usage
	hostMonitor.CPULoad, err = cpu.Percent(time.Second, false)
	if err != nil {
		err = errors.New("unable to get CPU load per sec")
		return
	}
	//Memory Info and use percentage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		err = errors.New("unable to get memory load per sec")
		return
	}
	hostMonitor.MemUsage = memInfo.UsedPercent
	hostMonitor.MemUsed = memInfo.Used
	hostMonitor.MemTotal = memInfo.Total

	//NetSpeed
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

	hostMonitor.LocalIP = getLocalIP()

	return hostMonitor, nil
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
