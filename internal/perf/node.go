package perf

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// userHZ is the unit of the CPU times in /proc/<pid>/stat: the kernel exports them in USER_HZ, fixed at 100 on Linux.
const userHZ = 100

const nodeSampleInterval = time.Second

// NodeInfo holds the resources the node process used during a test repetition, read from /proc.
type NodeInfo struct {
	Process       string  `json:"process"`
	PID           int     `json:"pid"`
	Seconds       float64 `json:"seconds"`
	CPUSeconds    float64 `json:"cpuSeconds"`
	RSSStartBytes uint64  `json:"rssStartBytes"`
	RSSEndBytes   uint64  `json:"rssEndBytes"`
	RSSMaxBytes   uint64  `json:"rssMaxBytes"`
	WriteSyscalls uint64  `json:"writeSyscalls"`
	SamplesFile   string  `json:"samplesFile"`
}

type procSample struct {
	at            time.Time
	cpuTicks      uint64
	rssBytes      uint64
	writeSyscalls uint64
}

// findPID returns the lowest pid whose comm is name: the process name as pgrep -x matches it.
func findPID(procDir, name string) (int, bool) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return 0, false
	}
	found := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || (found != 0 && pid >= found) {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procDir, e.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == name {
			found = pid
		}
	}
	return found, found != 0
}

func readProcSample(procDir string, pid int) (procSample, error) {
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	s := procSample{at: time.Now()}

	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return s, err
	}
	// The comm field can contain spaces and parentheses: the fields after it start at the last ')'.
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(fields) < 13 {
		return s, fmt.Errorf("short %s/stat", dir)
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return s, fmt.Errorf("bad CPU times in %s/stat", dir)
	}
	s.cpuTicks = utime + stime

	rssKB, err := procField(filepath.Join(dir, "status"), "VmRSS:")
	if err != nil {
		return s, err
	}
	s.rssBytes = rssKB * 1024

	s.writeSyscalls, err = procField(filepath.Join(dir, "io"), "syscw:")
	return s, err
}

// procField returns the first number after key in a "key value [unit]" /proc file.
func procField(path, key string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == key {
			return strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return 0, fmt.Errorf("%s not found in %s", key, path)
}

func summarizeNode(process string, pid int, samples []procSample, samplesFile string) *NodeInfo {
	first, last := samples[0], samples[len(samples)-1]
	node := &NodeInfo{
		Process:       process,
		PID:           pid,
		Seconds:       last.at.Sub(first.at).Seconds(),
		CPUSeconds:    float64(last.cpuTicks-first.cpuTicks) / userHZ,
		RSSStartBytes: first.rssBytes,
		RSSEndBytes:   last.rssBytes,
		WriteSyscalls: last.writeSyscalls - first.writeSyscalls,
		SamplesFile:   samplesFile,
	}
	for _, s := range samples {
		node.RSSMaxBytes = max(node.RSSMaxBytes, s.rssBytes)
	}
	return node
}

// nodeSampler reads /proc/<pid> every interval and writes each sample to a CSV series.
type nodeSampler struct {
	procDir, process string
	pid              int
	csvPath          string
	csv              *os.File
	samples          []procSample
	done             chan struct{}
	wg               sync.WaitGroup
	mu               sync.Mutex
}

func startNodeSampler(procDir, process string, pid int, interval time.Duration, csvPath string) (*nodeSampler, error) {
	first, err := readProcSample(procDir, pid)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(csvPath)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString("ms,cpuTicks,rssBytes,writeSyscalls\n"); err != nil {
		f.Close()
		return nil, err
	}
	s := &nodeSampler{procDir: procDir, process: process, pid: pid, csvPath: csvPath, csv: f, done: make(chan struct{})}
	s.add(first)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				if sample, err := readProcSample(procDir, pid); err == nil {
					s.add(sample)
				}
			}
		}
	}()
	return s, nil
}

func (s *nodeSampler) add(sample procSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sample)
	fmt.Fprintf(s.csv, "%d,%d,%d,%d\n", sample.at.UnixMilli(), sample.cpuTicks, sample.rssBytes, sample.writeSyscalls)
}

// stop takes the last sample and returns the summary of the repetition.
func (s *nodeSampler) stop() *NodeInfo {
	close(s.done)
	s.wg.Wait()
	if sample, err := readProcSample(s.procDir, s.pid); err == nil {
		s.add(sample)
	}
	s.csv.Close()
	return summarizeNode(s.process, s.pid, s.samples, filepath.Base(s.csvPath))
}

// startNodeSampler samples the node process next to the vegeta binary of the repetition; nil when it cannot be read.
func (pt *PerfTest) startNodeSampler() *nodeSampler {
	process := pt.Config.ProcessName()
	pid, ok := findPID(pt.procDir, process)
	if !ok {
		return nil
	}
	csvPath := strings.TrimSuffix(pt.Config.BinaryFileFullPathname, ".bin") + ".proc.csv"
	sampler, err := startNodeSampler(pt.procDir, process, pid, nodeSampleInterval, csvPath)
	if err != nil {
		return nil
	}
	return sampler
}

// formatNode is the console line of the node resources of a repetition.
func formatNode(node *NodeInfo) string {
	rssDeltaMiB := (int64(node.RSSEndBytes) - int64(node.RSSStartBytes)) >> 20
	return fmt.Sprintf("      node cpu=%.1fs (%.1f%%) rss max=%.2fGiB (%+dMiB) write syscalls=%d (%.0f/s)",
		node.CPUSeconds, 100*node.CPUSeconds/node.Seconds, float64(node.RSSMaxBytes)/(1<<30), rssDeltaMiB,
		node.WriteSyscalls, float64(node.WriteSyscalls)/node.Seconds)
}
