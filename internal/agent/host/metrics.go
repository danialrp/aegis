// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleMetrics returns a snapshot of the managed host's basic
// resource state. Reads /proc directly — fast (sub-millisecond) and
// avoids pulling in a metrics library.
//
// Designed for periodic polling (the UI hits this every 5s); response
// is small (<1 KiB JSON for typical mount counts).
func (m *Manager) HandleMetrics(ctx context.Context, _ json.RawMessage) (any, error) {
	out := protocol.MetricsResult{
		CollectedAt: time.Now().Unix(),
		CPUCount:    runtime.NumCPU(),
	}
	out.UptimeSec, _ = readUptime()
	out.LoadAvg = readLoadAvg()
	out.Memory, out.Swap = readMeminfo()
	out.Disks = readDisks()
	out.Kernel = readKernelVersion()
	return out, nil
}

func readUptime() (int64, error) {
	b, err := os.ReadFile("/proc/uptime") //nolint:gosec // constant path
	if err != nil {
		return 0, err
	}
	field := strings.Fields(string(b))
	if len(field) == 0 {
		return 0, nil
	}
	f, err := strconv.ParseFloat(field[0], 64)
	if err != nil {
		return 0, err
	}
	return int64(f), nil
}

func readLoadAvg() [3]float64 {
	b, err := os.ReadFile("/proc/loadavg") //nolint:gosec // constant path
	if err != nil {
		return [3]float64{}
	}
	parts := strings.Fields(string(b))
	if len(parts) < 3 {
		return [3]float64{}
	}
	var la [3]float64
	for i := range la {
		f, _ := strconv.ParseFloat(parts[i], 64)
		la[i] = f
	}
	return la
}

func readMeminfo() (mem protocol.MemoryStats, swap protocol.MemoryStats) {
	f, err := os.Open("/proc/meminfo") //nolint:gosec // constant path
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	var (
		memTotal, memAvail        uint64
		swapTotal, swapFree       uint64
		buffers, cached, sreclaim uint64
	)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		// Values are typically "12345 kB"; strip the unit.
		v = strings.TrimSuffix(v, " kB")
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			continue
		}
		bytes := parsed * 1024
		switch k {
		case "MemTotal":
			memTotal = bytes
		case "MemAvailable":
			memAvail = bytes
		case "Buffers":
			buffers = bytes
		case "Cached":
			cached = bytes
		case "SReclaimable":
			sreclaim = bytes
		case "SwapTotal":
			swapTotal = bytes
		case "SwapFree":
			swapFree = bytes
		}
	}
	mem.Total = memTotal
	if memAvail > 0 {
		mem.Free = memAvail
		if memTotal >= memAvail {
			mem.Used = memTotal - memAvail
		}
	} else {
		// Pre-3.14 kernels: fall back to Total - (Buffers + Cached + SReclaimable).
		free := buffers + cached + sreclaim
		mem.Free = free
		if memTotal >= free {
			mem.Used = memTotal - free
		}
	}
	swap.Total = swapTotal
	swap.Free = swapFree
	if swapTotal >= swapFree {
		swap.Used = swapTotal - swapFree
	}
	return
}

// readDisks returns usage for filesystems mounted under '/' that look
// like real block storage. Filters out tmpfs/proc/sysfs/etc.
func readDisks() []protocol.DiskUsage {
	f, err := os.Open("/proc/mounts") //nolint:gosec // constant path
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var out []protocol.DiskUsage
	seen := make(map[string]struct{})
	s := bufio.NewScanner(f)
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) < 3 {
			continue
		}
		dev, mount, fs := parts[0], parts[1], parts[2]
		// Skip non-block filesystems by type.
		switch fs {
		case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts",
			"cgroup", "cgroup2", "pstore", "bpf", "tracefs",
			"securityfs", "debugfs", "configfs", "fusectl",
			"hugetlbfs", "mqueue", "ramfs", "rpc_pipefs",
			"binfmt_misc", "autofs", "overlay", "squashfs":
			continue
		}
		// Skip duplicate-mount entries.
		if _, ok := seen[mount]; ok {
			continue
		}
		// Skip devices that don't start with a real block-device hint.
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		seen[mount] = struct{}{}

		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize) //nolint:gosec // bsize is positive
		total := st.Blocks * bsize
		free := st.Bavail * bsize
		if total == 0 {
			continue
		}
		out = append(out, protocol.DiskUsage{
			Mount: mount, FS: fs,
			Total: total,
			Used:  total - free,
		})
	}
	return out
}

func readKernelVersion() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease") //nolint:gosec // constant path
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
