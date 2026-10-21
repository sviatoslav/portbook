package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"

	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestConcurrentResetAndRemoveSnapshots(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, 0, "config", "remove", "--all")
	run(t, dir, 0, "config", "add", "1")
	custom, _ := ports.FromRanges([]ports.Range{{Start: 1, End: 1}})
	defaults, _ := ports.FromRanges([]ports.Range{{Start: 49152, End: 65535}})
	subtracted := defaults
	subtracted[49152] = false
	store := config.Store{Coordinator: state.Coordinator{Dir: dir}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan error, 1)
	go func() {
		for {
			p, err := store.Read()
			if err != nil {
				observed <- err
				return
			}
			if p != custom && p != defaults && p != subtracted {
				observed <- fmt.Errorf("partial or unexpected snapshot: %v", p.Ranges())
				return
			}
			select {
			case <-ctx.Done():
				observed <- nil
				return
			default:
			}
		}
	}()
	const workers = 12
	cmds := make([]*exec.Cmd, workers)
	outs, diags := make([]bytes.Buffer, workers), make([]bytes.Buffer, workers)
	for i := range cmds {
		args := []string{"config", "reset"}
		if i%2 != 0 {
			args = []string{"config", "remove", "49152"}
		}
		cmds[i] = command(dir, args...)
		cmds[i].Stdout = &outs[i]
		cmds[i].Stderr = &diags[i]
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	balance := 0
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("worker %d: %v %s", i, err, &diags[i])
		}
		if diags[i].Len() != 0 {
			t.Fatal(&diags[i])
		}
		var res struct {
			Command        string
			Changed, Total int
		}
		if err := json.Unmarshal(outs[i].Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			if res.Command != "config reset" || res.Total != 16384 {
				t.Fatal(outs[i].String())
			}
			balance += res.Changed
		} else {
			balance -= res.Changed
		}
	}
	cancel()
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	final, err := store.Read()
	if err != nil || final != defaults && final != subtracted {
		t.Fatal(final.Ranges(), err)
	}
	// The first reset removes port 1 as well as adding defaults; later resets
	// can only restore a concurrently removed 49152. No mutation may be lost.
	if balance != final.Count()+1 {
		t.Fatalf("membership accounting lost a mutation: balance=%d final=%d", balance, final.Count())
	}
}
