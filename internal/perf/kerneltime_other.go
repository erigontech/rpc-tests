//go:build !linux

package perf

import (
	"context"
	"net"
)

func dialKernelTime(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func kernelTime(net.Conn) int64 { return 0 }
