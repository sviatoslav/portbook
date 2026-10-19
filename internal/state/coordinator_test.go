package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFailureAndPanicReleaseLock(t *testing.T) {
	c := Coordinator{Dir: t.TempDir(), LockTimeout: 100 * time.Millisecond}
	abort := errors.New("abort")
	if err := c.WithLock(context.Background(), func() error { return abort }); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not propagate")
			}
		}()
		_ = c.WithLock(context.Background(), func() error { panic("operation panic") })
	}()
	if err := c.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("lock leaked: %v", err)
	}
}

func TestCancelledContextDoesNotRunOperation(t *testing.T) {
	c := Coordinator{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := c.WithLock(ctx, func() error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if err := c.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
