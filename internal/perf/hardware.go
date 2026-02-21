package perf

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Hardware provides methods to extract hardware information.
type Hardware struct{}

// Vendor returns the system vendor.
func (h *Hardware) Vendor() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/sys_vendor")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// NormalizedVendor returns the system vendor as a lowercase first token.
func (h *Hardware) NormalizedVendor() string {
	vendor := h.Vendor()
	parts := strings.Split(vendor, " ")
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return "unknown"
}

// Product returns the system product name.
func (h *Hardware) Product() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/product_name")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// Board returns the system board name.
func (h *Hardware) Board() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/board_name")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// NormalizedProduct returns the system product name as lowercase without whitespaces.
func (h *Hardware) NormalizedProduct() string {
	product := h.Product()
	return strings.ToLower(strings.ReplaceAll(product, " ", ""))
}

// NormalizedBoard returns the board name as a lowercase name without whitespaces.
func (h *Hardware) NormalizedBoard() string {
	board := h.Board()
	parts := strings.Split(board, "/")
	if len(parts) > 0 {
		return strings.ToLower(strings.ReplaceAll(parts[0], " ", ""))
	}
	return "unknown"
}

// GetCPUModel returns the CPU model information.
func (h *Hardware) GetCPUModel() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	cmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'model name' | uniq")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	parts := strings.Split(string(output), ":")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}

// GetBogomips returns the bogomips value.
func (h *Hardware) GetBogomips() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	cmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'bogomips' | uniq")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	parts := strings.Split(string(output), ":")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}

// GetKernelVersion returns the kernel version.
func GetKernelVersion() string {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// GetGCCVersion returns the GCC version.
func GetGCCVersion() string {
	cmd := exec.Command("gcc", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

// GetGoVersion returns the Go version.
func GetGoVersion() string {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// GetGitCommit returns the git commit hash for a directory.
func GetGitCommit(dir string) string {
	if dir == "" {
		return ""
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetFileChecksum returns the checksum of a file.
func GetFileChecksum(filepath string) string {
	cmd := exec.Command("sum", filepath)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(string(output), " ")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// IsProcessRunning checks if a process with the given name is running.
func IsProcessRunning(processName string) bool {
	cmd := exec.Command("pgrep", "-x", processName)
	out, err := cmd.Output()
	return err == nil && len(out) > 0
}

// EmptyOSCache drops OS caches (requires root on Linux, purge on macOS).
func EmptyOSCache() error {
	switch runtime.GOOS {
	case "linux":
		if err := exec.Command("sync").Run(); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		cmd := exec.Command("sh", "-c", "echo 3 > /proc/sys/vm/drop_caches")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cache purge failed: %w", err)
		}
	case "darwin":
		if err := exec.Command("sync").Run(); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		if err := exec.Command("purge").Run(); err != nil {
			return fmt.Errorf("cache purge failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return nil
}
