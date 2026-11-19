//go:build !linux

package perf

// allowedCPUs returns nothing where the CPUs of a process cannot be set: the heavy connections are then
// read in the same process.
func allowedCPUs() []int { return nil }

func pinProcess([]int) (func(), error) { return func() {}, nil }
