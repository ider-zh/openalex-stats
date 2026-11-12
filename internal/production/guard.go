package production

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func memoryValues(path string) map[string]uint64 {
	f, e := os.Open(path)
	if e != nil {
		return nil
	}
	defer f.Close()
	values := map[string]uint64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		n, e := strconv.ParseUint(fields[1], 10, 64)
		if e == nil {
			values[strings.TrimSuffix(fields[0], ":")] = n * 1024
		}
	}
	return values
}

// Soft admission/pressure guard. It cancels only this run, preserving complete
// shards; it never changes host swap or signals unrelated processes.
func startGuard(ctx context.Context, cancel context.CancelCauseFunc, maxRSS, availableFloor uint64) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		low, over := 0, 0
		var previousSwap uint64
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				memory, status := memoryValues("/proc/meminfo"), memoryValues("/proc/self/status")
				if availableFloor > 0 && memory["MemTotal"] > 0 && memory["MemAvailable"] < availableFloor {
					low++
				} else {
					low = 0
				}
				if maxRSS > 0 && status["VmRSS"] > maxRSS {
					over++
				} else {
					over = 0
				}
				if low >= 3 {
					cancel(fmt.Errorf("host MemAvailable below %d bytes for 6s; resumable run stopped", availableFloor))
					return
				}
				if over >= 3 {
					cancel(fmt.Errorf("project RSS above %d bytes for 6s; resumable run stopped", maxRSS))
					return
				}
				swap := status["VmSwap"]
				if swap > previousSwap+(128<<20) && memory["MemTotal"] > 0 && memory["MemAvailable"] < availableFloor*2 {
					cancel(fmt.Errorf("project is accumulating swap under host pressure"))
					return
				}
				previousSwap = swap
			}
		}
	}()
	return func() { close(done); <-stopped }
}
