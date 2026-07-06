package hostinfo

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readProcLoadAverage(procRoot string) []float64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "loadavg"))
	if err != nil {
		return []float64{0, 0, 0}
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return []float64{0, 0, 0}
	}

	result := make([]float64, 0, 3)
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			result = append(result, 0)
			continue
		}
		result = append(result, roundFloat(value))
	}
	return result
}

func readProcCPUSample(procRoot string) (cpuSample, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return cpuSample{}, false
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuSample{}, false
		}

		values := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSample{}, false
			}
			values = append(values, value)
		}

		var total uint64
		for index, value := range values {
			if index >= 8 {
				break
			}
			total += value
		}

		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuSample{total: total, idle: idle}, true
	}

	return cpuSample{}, false
}

func readProcMemInfoValues(procRoot string) map[string]uint64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return map[string]uint64{}
	}

	values := map[string]uint64{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[parts[0]] = value * 1024
	}

	return values
}

func memoryUsageFromMemInfo(values map[string]uint64) MemoryUsage {
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"]
	}
	used := uint64(0)
	if total >= available {
		used = total - available
	}

	usagePercent := 0.0
	if total > 0 {
		usagePercent = roundFloat((float64(used) / float64(total)) * 100)
	}

	return MemoryUsage{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsagePercent:   usagePercent,
	}
}

func swapUsageFromMemInfo(values map[string]uint64) SwapUsage {
	total := values["SwapTotal"]
	free := values["SwapFree"]
	used := uint64(0)
	if total >= free {
		used = total - free
	}

	usagePercent := 0.0
	if total > 0 {
		usagePercent = roundFloat((float64(used) / float64(total)) * 100)
	}

	return SwapUsage{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: free,
		UsagePercent:   usagePercent,
	}
}
