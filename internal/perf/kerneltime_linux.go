package perf

import (
	"context"
	"net"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

// kernelTimeConn is a TCP connection whose reads keep the time the kernel received the last data read,
// from SO_TIMESTAMPNS, so that a frame's arrival can be split into the wait in the kernel and in rpc_perf.
type kernelTimeConn struct {
	*net.TCPConn
	last atomic.Int64 // unix nanoseconds of the last read, 0 before any
	oob  [128]byte
}

func dialKernelTime(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tcp := c.(*net.TCPConn)
	raw, err := tcp.SyscallConn()
	if err != nil {
		return nil, err
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1)
	}); err != nil {
		return nil, err
	}
	if serr != nil {
		return nil, serr
	}
	return &kernelTimeConn{TCPConn: tcp}, nil
}

func (c *kernelTimeConn) Read(p []byte) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var n int
	var rerr error
	err = raw.Read(func(fd uintptr) bool {
		var oobn int
		n, oobn, _, _, rerr = unix.Recvmsg(int(fd), p, c.oob[:], 0)
		if rerr == unix.EAGAIN {
			return false
		}
		if rerr == nil {
			c.keep(c.oob[:oobn])
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	if rerr != nil {
		return 0, rerr
	}
	if n == 0 && len(p) > 0 {
		return 0, net.ErrClosed
	}
	return n, nil
}

func (c *kernelTimeConn) keep(oob []byte) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return
	}
	for _, m := range msgs {
		if m.Header.Level == unix.SOL_SOCKET && m.Header.Type == unix.SO_TIMESTAMPNS && len(m.Data) >= 16 {
			ts := (*unix.Timespec)(unsafe.Pointer(&m.Data[0]))
			c.last.Store(ts.Nano())
		}
	}
}

// kernelTime returns the kernel receive time of the last read of c, 0 when c does not keep it.
func kernelTime(c net.Conn) int64 {
	if k, ok := c.(*kernelTimeConn); ok {
		return k.last.Load()
	}
	return 0
}
