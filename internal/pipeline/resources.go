package pipeline

import (
	"bufio"
	"os"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"
)

type ResourceSummary struct {
	ElapsedSeconds               float64         `json:"elapsed_seconds"`
	Samples                      int             `json:"samples"`
	CPUSamples                   int             `json:"cpu_samples"`
	MeanProcessMachineCPUPercent float64         `json:"mean_process_machine_cpu_percent"`
	PeakProcessMachineCPUPercent float64         `json:"peak_process_machine_cpu_percent"`
	PeakRSSBytes                 uint64          `json:"peak_rss_bytes"`
	PeakGoHeapBytes              uint64          `json:"peak_go_heap_bytes"`
	HostTotalMemoryBytes         uint64          `json:"host_total_memory_bytes"`
	MinHostAvailableMemoryBytes  uint64          `json:"min_host_available_memory_bytes"`
	GCCollections                uint32          `json:"gc_collections"`
	GCPauseSeconds               float64         `json:"gc_pause_seconds"`
	GCCPUSeconds                 float64         `json:"gc_cpu_seconds"`
	AllocatedBytes               uint64          `json:"allocated_bytes"`
	PeakProcessSwapBytes         uint64          `json:"peak_process_swap_bytes"`
	IOAvailable                  bool            `json:"io_available"`
	ReadBytes                    uint64          `json:"physical_read_bytes"`
	WriteBytes                   uint64          `json:"physical_write_bytes"`
	LogicalReadBytes             uint64          `json:"logical_read_bytes"`
	LogicalWriteBytes            uint64          `json:"logical_write_bytes"`
	Trace                        []ResourcePoint `json:"trace,omitempty"`
	TraceSamplesOmitted          int             `json:"trace_samples_omitted,omitempty"`
	HostSwapCountersAvailable    bool            `json:"host_swap_counters_available"`
	HostSwapInPages              uint64          `json:"host_swap_in_pages"`
	HostSwapOutPages             uint64          `json:"host_swap_out_pages"`
	PageSizeBytes                int             `json:"page_size_bytes"`
}

// A bounded one-second trace supports honest sustained-window checks. It is
// telemetry, not an extra workload; long jobs retain at most five hours here.
type ResourcePoint struct {
	ElapsedSeconds    float64 `json:"elapsed_seconds"`
	CPUValid          bool    `json:"cpu_valid"`
	MachineCPUPercent float64 `json:"machine_cpu_percent"`
	RSSBytes          uint64  `json:"rss_bytes"`
	HeapBytes         uint64  `json:"heap_bytes"`
	ProcessSwapBytes  uint64  `json:"process_swap_bytes"`
	Goroutines        int     `json:"goroutines"`
	GCCollections     uint32  `json:"gc_collections"`
	ReadBytes         uint64  `json:"physical_read_bytes"`
	WriteBytes        uint64  `json:"physical_write_bytes"`
	HostSwapInPages   uint64  `json:"host_swap_in_pages"`
	HostSwapOutPages  uint64  `json:"host_swap_out_pages"`
}
type cpuReading struct {
	total, process uint64
	valid          bool
}

func cpuRead() cpuReading {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return cpuReading{}
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return cpuReading{}
	}
	var total uint64
	for i := 1; i < len(fields) && i <= 8; i++ {
		v, e := strconv.ParseUint(fields[i], 10, 64)
		if e != nil {
			return cpuReading{}
		}
		total += v
	}
	b, e = os.ReadFile("/proc/self/stat")
	if e != nil {
		return cpuReading{}
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return cpuReading{}
	}
	fields = strings.Fields(string(b[end+1:]))
	if len(fields) < 13 {
		return cpuReading{}
	}
	u, e := strconv.ParseUint(fields[11], 10, 64)
	if e != nil {
		return cpuReading{}
	}
	s, e := strconv.ParseUint(fields[12], 10, 64)
	if e != nil {
		return cpuReading{}
	}
	return cpuReading{total: total, process: u + s, valid: true}
}
func procKB(path string) map[string]uint64 {
	return procValues(path, 1024)
}

func procValues(path string, scale uint64) map[string]uint64 {
	f, e := os.Open(path)
	if e != nil {
		return nil
	}
	defer f.Close()
	out := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		value, e := strconv.ParseUint(parts[1], 10, 64)
		if e == nil {
			out[strings.TrimSuffix(parts[0], ":")] = value * scale
		}
	}
	return out
}

// stop returns the completed summary and synchronizes sampler shutdown.
func startResourceSampler() func() ResourceSummary {
	started := time.Now()
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	initialIO := procValues("/proc/self/io", 1)
	initialVM := procValues("/proc/vmstat", 1)
	gcCPU := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(gcCPU)
	initialGCCPU := gcCPU[0].Value.Float64()
	initialCPU := cpuRead()
	stop := make(chan struct{})
	result := make(chan ResourceSummary, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var out ResourceSummary
		out.PageSizeBytes = os.Getpagesize()
		previous := initialCPU
		var totalTicks, processTicks uint64
		sample := func() {
			out.Samples++
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			out.ElapsedSeconds = time.Since(started).Seconds()
			out.GCCollections = m.NumGC - initial.NumGC
			out.GCPauseSeconds = float64(m.PauseTotalNs-initial.PauseTotalNs) / 1e9
			out.AllocatedBytes = m.TotalAlloc - initial.TotalAlloc
			metrics.Read(gcCPU)
			out.GCCPUSeconds = gcCPU[0].Value.Float64() - initialGCCPU
			io := procValues("/proc/self/io", 1)
			if io != nil && initialIO != nil {
				out.IOAvailable = true
				out.ReadBytes = io["read_bytes"] - initialIO["read_bytes"]
				out.WriteBytes = io["write_bytes"] - initialIO["write_bytes"]
				out.LogicalReadBytes = io["rchar"] - initialIO["rchar"]
				out.LogicalWriteBytes = io["wchar"] - initialIO["wchar"]
			}
			out.PeakGoHeapBytes = max(out.PeakGoHeapBytes, m.HeapAlloc)
			status := procKB("/proc/self/status")
			vm := procValues("/proc/vmstat", 1)
			if vm != nil && initialVM != nil {
				out.HostSwapCountersAvailable = true
				out.HostSwapInPages = vm["pswpin"] - initialVM["pswpin"]
				out.HostSwapOutPages = vm["pswpout"] - initialVM["pswpout"]
			}
			point := ResourcePoint{ElapsedSeconds: out.ElapsedSeconds, RSSBytes: status["VmRSS"], HeapBytes: m.HeapAlloc, ProcessSwapBytes: status["VmSwap"], Goroutines: runtime.NumGoroutine(), GCCollections: out.GCCollections, ReadBytes: out.ReadBytes, WriteBytes: out.WriteBytes}
			point.HostSwapInPages, point.HostSwapOutPages = out.HostSwapInPages, out.HostSwapOutPages
			out.PeakRSSBytes = max(out.PeakRSSBytes, status["VmRSS"])
			out.PeakProcessSwapBytes = max(out.PeakProcessSwapBytes, status["VmSwap"])
			memory := procKB("/proc/meminfo")
			if total := memory["MemTotal"]; total > 0 {
				out.HostTotalMemoryBytes = total
				available := memory["MemAvailable"]
				if out.Samples == 1 || available < out.MinHostAvailableMemoryBytes {
					out.MinHostAvailableMemoryBytes = available
				}
			}
			next := cpuRead()
			if previous.valid && next.valid && next.total > previous.total && next.process >= previous.process {
				pct := 100 * float64(next.process-previous.process) / float64(next.total-previous.total)
				point.CPUValid, point.MachineCPUPercent = true, pct
				out.CPUSamples++
				totalTicks += next.total - previous.total
				processTicks += next.process - previous.process
				out.PeakProcessMachineCPUPercent = max(out.PeakProcessMachineCPUPercent, pct)
			}
			previous = next
			if len(out.Trace) < 18000 {
				out.Trace = append(out.Trace, point)
			} else {
				out.TraceSamplesOmitted++
			}
		}
		for {
			select {
			case <-ticker.C:
				sample()
			case <-stop:
				sample()
				if totalTicks > 0 {
					out.MeanProcessMachineCPUPercent = 100 * float64(processTicks) / float64(totalTicks)
				}
				result <- out
				return
			}
		}
	}()
	return func() ResourceSummary { close(stop); return <-result }
}

// StartResourceSampler exposes the same bounded monitor to long-lived cache jobs.
func StartResourceSampler() func() ResourceSummary { return startResourceSampler() }
