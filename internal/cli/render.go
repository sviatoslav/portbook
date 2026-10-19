package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"portbook/internal/ports"
	"text/tabwriter"
)

type result struct {
	Command string        `json:"command"`
	Ranges  []ports.Range `json:"ranges,omitempty"`
	Changed *int          `json:"changed,omitempty"`
	Total   int           `json:"total"`
}

type diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func fail(w io.Writer, format, code string, err error, status int) int {
	if format == "json" {
		_ = json.NewEncoder(w).Encode(struct {
			Error diagnostic `json:"error"`
		}{Error: diagnostic{Code: code, Message: err.Error()}})
	} else {
		_, _ = fmt.Fprintf(w, "Error: %s\n", err)
	}
	return status
}

func noun(n int) string {
	if n == 1 {
		return "port"
	}
	return "ports"
}

func render(w io.Writer, format string, res result) error {
	if format == "json" {
		// The empty show result must carry [] rather than omit ranges.
		if res.Changed == nil {
			return json.NewEncoder(w).Encode(struct {
				Command string        `json:"command"`
				Ranges  []ports.Range `json:"ranges"`
				Total   int           `json:"total"`
			}{res.Command, res.Ranges, res.Total})
		}
		return json.NewEncoder(w).Encode(res)
	}
	if res.Changed != nil {
		verb := "Added"
		if res.Command == "config ranges remove" {
			verb = "Removed"
		}
		_, err := fmt.Fprintf(w, "%s %d %s. Total: %d %s.\n", verb, *res.Changed, noun(*res.Changed), res.Total, noun(res.Total))
		return err
	}
	if len(res.Ranges) == 0 {
		if _, err := fmt.Fprintln(w, "No port ranges configured."); err != nil {
			return err
		}
	} else {
		table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(table, "RANGE\tPORTS"); err != nil {
			return err
		}
		for _, r := range res.Ranges {
			if _, err := fmt.Fprintf(table, "%s\t%d\n", r.String(), r.End-r.Start+1); err != nil {
				return err
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Total: %d %s.\n", res.Total, noun(res.Total))
	return err
}
