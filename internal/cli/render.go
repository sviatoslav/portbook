package cli

import (
	"encoding/json"
	"fmt"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
	"io"
	"text/tabwriter"
)

type pathResult struct {
	config.Selection
	SavedDirectory *string `json:"saved_directory"`
	Changed        bool    `json:"-"`
}

type result struct {
	Path    *pathResult   `json:"-"`
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
	if res.Path != nil {
		if format == "json" && !res.Path.Changed {
			return json.NewEncoder(w).Encode(struct {
				Command string `json:"command"`
				config.Selection
			}{res.Command, res.Path.Selection})
		}
		if res.Path.Changed {
			saved := "(none; OS default)"
			if res.Path.SavedDirectory != nil {
				saved = *res.Path.SavedDirectory
			}
			if _, err := fmt.Fprintf(w, "Saved directory: %s\n", saved); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "Effective directory: %s\nConfig file: %s\nSource: %s\n", res.Path.Directory, res.Path.ConfigFile, res.Path.Source)
		return err
	}
	if res.Changed != nil {
		verb := "Added"
		if res.Command == "config remove" {
			verb = "Removed"
		} else if res.Command == "config reset" {
			verb = "Reset"
		}
		_, err := fmt.Fprintf(w, "%s %d %s. Total: %d %s.\n", verb, *res.Changed, noun(*res.Changed), res.Total, noun(res.Total))
		return err
	}
	if format == "json" {
		// The empty show result must carry [] rather than omit ranges.
		return json.NewEncoder(w).Encode(struct {
			Command string        `json:"command"`
			Ranges  []ports.Range `json:"ranges"`
			Total   int           `json:"total"`
		}{res.Command, res.Ranges, res.Total})
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
