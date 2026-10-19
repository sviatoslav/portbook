// Package ports implements the configured set of allowed TCP ports.
package ports

import (
	"fmt"
	"strconv"
	"strings"
)

type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func (r Range) Validate() error {
	if r.Start < 1 || r.End > 65535 || r.Start > r.End {
		return fmt.Errorf("range must be ordered and within 1..65535: %d-%d", r.Start, r.End)
	}
	return nil
}

func (r Range) String() string {
	if r.Start == r.End {
		return strconv.Itoa(r.Start)
	}
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

func Parse(s string) (Range, error) {
	parts := strings.Split(s, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return Range{}, fmt.Errorf("invalid port range %q; expected PORT or START-END", s)
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return Range{}, fmt.Errorf("invalid port range %q; expected PORT or START-END", s)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return Range{}, fmt.Errorf("invalid port range %q", s)
		}
		values[i] = n
	}
	r := Range{Start: values[0], End: values[len(values)-1]}
	if err := r.Validate(); err != nil {
		return Range{}, err
	}
	return r, nil
}

// Pool is a set, so input ranges have no persistent identity.
type Pool [65536]bool

func FromRanges(ranges []Range) (Pool, error) {
	var p Pool
	for _, r := range ranges {
		if err := r.Validate(); err != nil {
			return Pool{}, err
		}
	}
	p.Apply(ranges, true)
	return p, nil
}

// Apply requires validated ranges and returns the actual number of changed ports.
func (p *Pool) Apply(ranges []Range, add bool) int {
	changed := 0
	for _, r := range ranges {
		for n := r.Start; n <= r.End; n++ {
			if p[n] != add {
				p[n] = add
				changed++
			}
		}
	}
	return changed
}

func (p *Pool) Count() int {
	total := 0
	for n := 1; n <= 65535; n++ {
		if p[n] {
			total++
		}
	}
	return total
}

// Ranges returns sorted disjoint intervals, merging adjacent ports.
func (p *Pool) Ranges() []Range {
	ranges := []Range{}
	for n := 1; n <= 65535; n++ {
		if !p[n] {
			continue
		}
		start := n
		for n < 65535 && p[n+1] {
			n++
		}
		ranges = append(ranges, Range{Start: start, End: n})
	}
	return ranges
}
