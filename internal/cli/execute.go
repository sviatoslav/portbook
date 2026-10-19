package cli

import (
	"context"
	"path/filepath"

	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
)

func (o *options) showConfig(_ context.Context) (result, error) {
	s, err := config.WithOverride(o.configDir, o.configDirSet())
	if err != nil {
		return result{}, err
	}
	p, err := s.Read()
	if err != nil {
		return result{}, err
	}
	return result{Command: "config show", Ranges: p.Ranges(), Total: p.Count()}, nil
}

func (o *options) addConfig(ctx context.Context, ranges []ports.Range) (result, error) {
	return o.mutateConfig(ctx, "config add", func(p *ports.Pool) (int, error) { return p.Apply(ranges, true), nil })
}

func (o *options) removeConfig(ctx context.Context, ranges []ports.Range) (result, error) {
	return o.mutateConfig(ctx, "config remove", func(p *ports.Pool) (int, error) { return p.Apply(ranges, false), nil })
}

func (o *options) clearConfig(ctx context.Context) (result, error) {
	return o.mutateConfig(ctx, "config remove", func(p *ports.Pool) (int, error) { n := p.Count(); *p = ports.Pool{}; return n, nil })
}

func (o *options) mutateConfig(ctx context.Context, command string, change func(*ports.Pool) (int, error)) (result, error) {
	s, err := config.WithOverride(o.configDir, o.configDirSet())
	if err != nil {
		return result{}, err
	}
	changed, total, err := s.Mutate(ctx, change)
	if err != nil {
		return result{}, err
	}
	return result{Command: command, Changed: &changed, Total: total}, nil
}

func (o *options) pathGet(_ context.Context) (result, error) {
	l, err := config.DefaultLocation(o.configDir, o.configDirSet())
	if err != nil {
		return result{}, err
	}
	effective, err := l.Resolve()
	return result{Command: "config path get", Path: &pathResult{Selection: effective}}, err
}

func (o *options) pathChange(ctx context.Context, dir string, reset bool) (result, error) {
	l, err := config.DefaultLocation(o.configDir, o.configDirSet())
	if err != nil {
		return result{}, err
	}
	effective, err := l.Change(ctx, dir)
	command := "config path set"
	var saved *string
	if reset {
		command = "config path reset"
	} else {
		clean := filepath.Clean(dir)
		saved = &clean
	}
	return result{Command: command, Path: &pathResult{Selection: effective, SavedDirectory: saved, Changed: true}}, err
}
