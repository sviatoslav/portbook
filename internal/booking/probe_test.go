package booking

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

type probeListener struct {
	closed   bool
	closeErr error
}

func (l *probeListener) Accept() (net.Conn, error) { panic("unused") }
func (l *probeListener) Addr() net.Addr            { return &net.TCPAddr{} }
func (l *probeListener) Close() error              { l.closed = true; return l.closeErr }
func TestProbeFamilyFailurePolicyAndCleanup(t *testing.T) {
	unknown := errors.New("unexpected failure")
	for _, tc := range []struct {
		name       string
		v4, v6     error
		want, fail bool
	}{
		{"both", nil, nil, true, false},
		{"v4 only", nil, syscall.EAFNOSUPPORT, true, false},
		{"v6 only", syscall.EPROTONOSUPPORT, nil, true, false},
		{"none", syscall.EAFNOSUPPORT, syscall.EPROTONOSUPPORT, false, true},
		{"busy v4", syscall.EADDRINUSE, nil, false, false},
		{"busy v6", nil, syscall.EADDRINUSE, false, false},
		{"permission", nil, syscall.EACCES, false, false},
		{"forbidden", syscall.EPERM, nil, false, false},
		{"unknown", nil, unknown, false, true},
		{"address unavailable", nil, syscall.EADDRNOTAVAIL, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened []*probeListener
			ok, err := available(context.Background(), 12345, func(_ context.Context, network, address string) (net.Listener, error) {
				familyErr := tc.v4
				if network == "tcp6" {
					familyErr = tc.v6
				}
				if familyErr != nil {
					return nil, &net.OpError{Op: "listen", Net: network, Err: familyErr}
				}
				l := &probeListener{}
				opened = append(opened, l)
				return l, nil
			})
			if ok != tc.want || (err != nil) != tc.fail {
				t.Fatal(ok, err)
			}
			for _, l := range opened {
				if !l.closed {
					t.Fatal("leaked probe socket")
				}
			}
		})
	}
	l := &probeListener{closeErr: unknown}
	if ok, err := available(context.Background(), 12345, func(context.Context, string, string) (net.Listener, error) { return l, nil }); ok || !errors.Is(err, unknown) {
		t.Fatal(ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := available(ctx, 12345, func(context.Context, string, string) (net.Listener, error) {
		t.Fatal("cancelled probe called listen")
		return nil, nil
	}); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
}
