package booking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// Available briefly holds both wildcard families. tcp6 explicitly uses IPv6
// only in Go, so the simultaneous IPv4 probe also detects separate listeners.
// Closing these sockets leaves an inevitable race with unmanaged processes.
func Available(ctx context.Context, port int) (bool, error) {
	// Go enables SO_REUSEADDR for listeners. On BSD/macOS that can let a
	// wildcard bind coexist with a specific-address listener. A probe must
	// disable reuse before bind so it detects those occupied ports too.
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			socketErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	return available(ctx, port, lc.Listen)
}

func available(ctx context.Context, port int, listen func(context.Context, string, string) (net.Listener, error)) (ok bool, err error) {
	var held []net.Listener
	defer func() {
		for _, l := range held {
			if closeErr := l.Close(); closeErr != nil {
				ok = false
				err = errors.Join(err, fmt.Errorf("close TCP probe: %w", closeErr))
			}
		}
	}()
	for _, family := range []struct{ network, host string }{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}} {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		l, err := listen(ctx, family.network, net.JoinHostPort(family.host, strconv.Itoa(port)))
		if err == nil {
			held = append(held, l)
			continue
		}
		if errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT) {
			continue
		}
		if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return false, nil
		}
		return false, fmt.Errorf("probe TCP port %d (%s): %w", port, family.network, err)
	}
	if len(held) == 0 {
		return false, errors.New("no supported TCP address family")
	}
	return true, nil
}
