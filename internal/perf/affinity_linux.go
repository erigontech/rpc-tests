//go:build linux

package perf

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

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

// pinProcess runs every thread of this process on cpus, with as many Ps, and returns how to undo it.
// Nothing changes for no cpus.
func pinProcess(cpus []int) (restore func(), err error) {
	if len(cpus) == 0 {
		return func() {}, nil
	}
	prev, prevProcs := allowedCPUs(), runtime.GOMAXPROCS(len(cpus))
	if err := setAffinity(cpus); err != nil {
		runtime.GOMAXPROCS(prevProcs)
		return nil, err
	}
	return func() {
		setAffinity(prev) //nolint:errcheck
		runtime.GOMAXPROCS(prevProcs)
	}, nil
}

func setAffinity(cpus []int) error {
	var set unix.CPUSet
	for _, c := range cpus {
		set.Set(c)
	}
	// A thread started by one not yet pinned would miss a single pass.
	for range 2 {
		tasks, err := os.ReadDir("/proc/self/task")
		if err != nil {
			return fmt.Errorf("pin to CPUs %v: %w", cpus, err)
		}
		for _, task := range tasks {
			tid, err := strconv.Atoi(task.Name())
			if err != nil {
				continue
			}
			if err := unix.SchedSetaffinity(tid, &set); err != nil && !errors.Is(err, unix.ESRCH) {
				return fmt.Errorf("pin to CPUs %v: %w", cpus, err)
			}
		}
	}
	return nil
}
