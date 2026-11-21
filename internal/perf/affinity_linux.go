//go:build linux

package perf

import "golang.org/x/sys/unix"

// allowedCPUs returns the CPUs this process may run on.
func allowedCPUs() []int {
	var set unix.CPUSet
	if unix.SchedGetaffinity(0, &set) != nil {
		return nil
	}
	var cpus []int
	for c := range len(set) * 64 {
		if set.IsSet(c) {
			cpus = append(cpus, c)
		}
	}
	return cpus
}
