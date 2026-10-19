package cli

import (
	"context"

	"portbook/internal/config"
	"portbook/internal/ports"
)

func showConfig(_ context.Context) (result, error) {
	s, err := config.Default()
	if err != nil {
		return result{}, err
	}
	p, err := s.Read()
	if err != nil {
		return result{}, err
	}
	return result{Command: "config show", Ranges: p.Ranges(), Total: p.Count()}, nil
}

func addConfig(ctx context.Context, ranges []ports.Range) (result, error) {
	return mutateConfig(ctx, "config ranges add", func(p *ports.Pool) (int, error) { return p.Apply(ranges, true), nil })
}

func removeConfig(ctx context.Context, ranges []ports.Range) (result, error) {
	return mutateConfig(ctx, "config ranges remove", func(p *ports.Pool) (int, error) { return p.Apply(ranges, false), nil })
}

func clearConfig(ctx context.Context) (result, error) {
	return mutateConfig(ctx, "config ranges remove", func(p *ports.Pool) (int, error) { n := p.Count(); *p = ports.Pool{}; return n, nil })
}

func mutateConfig(ctx context.Context, command string, change func(*ports.Pool) (int, error)) (result, error) {
	s, err := config.Default()
	if err != nil {
		return result{}, err
	}
	changed, total, err := s.Mutate(ctx, change)
	if err != nil {
		return result{}, err
	}
	return result{Command: command, Changed: &changed, Total: total}, nil
}
