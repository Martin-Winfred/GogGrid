package main

import (
	"fmt"

	"github.com/Martin-Winfred/GogGrid/pkg/monitor"
)

func main() {
	hostMonitor, err := monitor.GetHostMonitor()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// 打印主机监控数据
	fmt.Println("Architecture:", hostMonitor.Arch)
	fmt.Println("OS Info:", hostMonitor.OSInfo)
	fmt.Println("Hostname:", hostMonitor.Hostname)
	fmt.Println("Kernel Version:", hostMonitor.KernelVer)
	fmt.Println("Platform:", hostMonitor.Platform)
	fmt.Println("Family:", hostMonitor.Family)
	fmt.Println("Version:", hostMonitor.Version)
	fmt.Println("CPU Load:", hostMonitor.CPULoad)
	fmt.Println("Memory Usage:", hostMonitor.MemUsage)
	fmt.Println("Memory Used:", hostMonitor.MemUsed)
	fmt.Println("Memory Total:", hostMonitor.MemTotal)
	fmt.Println("Network Name:", hostMonitor.NetName)
	fmt.Println("Bytes Received:", hostMonitor.BytesRecv)
	fmt.Println("Bytes Sent:", hostMonitor.BytesSent)
	fmt.Println("Local IP:", hostMonitor.LocalIP)
}
