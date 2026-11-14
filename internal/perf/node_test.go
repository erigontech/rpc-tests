package perf

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeFakeProc writes the /proc files of pid under procDir: comm, stat with utime+stime = cpuTicks, VmRSS and syscw.
func writeFakeProc(t *testing.T, procDir string, pid int, comm string, cpuTicks, rssKB, syscw uint64) {
	t.Helper()
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fields 14 and 15 of stat are utime and stime; the comm in parentheses may contain spaces.
	stat := strconv.Itoa(pid) + " (" + comm + ") S 1 2 3 4 5 6 7 8 9 10 " +
		strconv.FormatUint(cpuTicks-1, 10) + " 1 0 0 20 0 30 0 100\n"
	files := map[string]string{
		"comm":   comm + "\n",
		"stat":   stat,
		"status": "Name:\t" + comm + "\nVmPeak:\t 9999 kB\nVmRSS:\t " + strconv.FormatUint(rssKB, 10) + " kB\nThreads:\t30\n",
		"io":     "rchar: 1\nwchar: 2\nsyscr: 3\nsyscw: " + strconv.FormatUint(syscw, 10) + "\nread_bytes: 0\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadProcSample(t *testing.T) {
	procDir := t.TempDir()
	writeFakeProc(t, procDir, 42, "my node", 250, 1024, 77)

	s, err := readProcSample(procDir, 42)
	if err != nil {
		t.Fatalf("readProcSample: %v", err)
	}
	if s.cpuTicks != 250 {
		t.Errorf("cpuTicks = %d, want utime+stime = 250", s.cpuTicks)
	}
	if s.rssBytes != 1024*1024 {
		t.Errorf("rssBytes = %d, want %d", s.rssBytes, 1024*1024)
	}
	if s.writeSyscalls != 77 {
		t.Errorf("writeSyscalls = %d, want 77", s.writeSyscalls)
	}
}

func TestReadProcSampleOfAMissingProcess(t *testing.T) {
	if _, err := readProcSample(t.TempDir(), 42); err == nil {
		t.Error("want an error when the process is not there")
	}
}

func TestFindPIDTakesTheOldestProcessWithThatName(t *testing.T) {
	procDir := t.TempDir()
	writeFakeProc(t, procDir, 900, "erigon", 1, 1, 1)
	writeFakeProc(t, procDir, 120, "erigon", 1, 1, 1)
	writeFakeProc(t, procDir, 50, "rpcdaemon", 1, 1, 1)
	if err := os.MkdirAll(filepath.Join(procDir, "self"), 0o755); err != nil {
		t.Fatal(err)
	}

	if pid, ok := findPID(procDir, "erigon"); !ok || pid != 120 {
		t.Errorf("findPID = %d, %v; want 120, true", pid, ok)
	}
	if _, ok := findPID(procDir, "geth"); ok {
		t.Error("findPID found a process that does not run")
	}
}

func TestSummarizeNodeSamples(t *testing.T) {
	start := time.Unix(1000, 0)
	samples := []procSample{
		{at: start, cpuTicks: 1000, rssBytes: 500, writeSyscalls: 10},
		{at: start.Add(time.Second), cpuTicks: 1100, rssBytes: 900, writeSyscalls: 40},
		{at: start.Add(2 * time.Second), cpuTicks: 1150, rssBytes: 700, writeSyscalls: 70},
	}

	node := summarizeNode("erigon", 7, samples, "x.proc.csv")

	want := NodeInfo{
		Process:       "erigon",
		PID:           7,
		Seconds:       2,
		CPUSeconds:    1.5,
		RSSStartBytes: 500,
		RSSEndBytes:   700,
		RSSMaxBytes:   900,
		WriteSyscalls: 60,
		SamplesFile:   "x.proc.csv",
	}
	if *node != want {
		t.Errorf("summarizeNode = %+v, want %+v", *node, want)
	}
}

func TestNodeSamplerWritesTheSeriesAndTheSummary(t *testing.T) {
	procDir := t.TempDir()
	writeFakeProc(t, procDir, 42, "erigon", 1000, 2048, 10)
	csvPath := filepath.Join(t.TempDir(), "run.proc.csv")

	sampler, err := startNodeSampler(procDir, "erigon", 42, time.Hour, csvPath)
	if err != nil {
		t.Fatalf("startNodeSampler: %v", err)
	}
	writeFakeProc(t, procDir, 42, "erigon", 1200, 4096, 25)
	node := sampler.stop()

	if node.CPUSeconds != 2 || node.WriteSyscalls != 15 || node.RSSMaxBytes != 4096*1024 {
		t.Errorf("node = %+v, want 2 CPU seconds, 15 write syscalls, 4 MiB max RSS", *node)
	}
	if node.SamplesFile != "run.proc.csv" {
		t.Errorf("samplesFile = %q, want the base name of the series", node.SamplesFile)
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 || lines[0] != "ms,cpuTicks,rssBytes,writeSyscalls" {
		t.Fatalf("series = %q, want a header and the first and last sample", lines)
	}
	if !strings.HasSuffix(lines[2], ",1200,4194304,25") {
		t.Errorf("last sample = %q", lines[2])
	}
}

func TestNodeSamplerOfAMissingProcess(t *testing.T) {
	if _, err := startNodeSampler(t.TempDir(), "erigon", 42, time.Hour, filepath.Join(t.TempDir(), "x.proc.csv")); err == nil {
		t.Error("want an error when the process cannot be read")
	}
}

func TestFormatNode(t *testing.T) {
	node := &NodeInfo{Seconds: 2, CPUSeconds: 1.5, RSSStartBytes: 3 << 30, RSSEndBytes: 3<<30 - 5<<20, RSSMaxBytes: 7 << 29, WriteSyscalls: 2001}

	want := "      node cpu=1.5s (75.0%) rss max=3.50GiB (-5MiB) write syscalls=2001 (1000/s)"
	if got := formatNode(node); got != want {
		t.Errorf("formatNode =\n%q, want\n%q", got, want)
	}

	node.RSSEndBytes = 3<<30 + 137<<20
	if got := formatNode(node); !strings.Contains(got, "(+137MiB)") {
		t.Errorf("formatNode = %q, want the RSS growth signed (+137MiB)", got)
	}
}
