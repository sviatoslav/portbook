package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func newListCommand(o *options, out io.Writer) *cobra.Command {
	var all bool
	var directory, owner string
	cmd := &cobra.Command{
		Use: "list", Short: "List bookings for the current directory or all owners",
		Long:    "List saved bookings without probing ports or reading configuration. Defaults to the current directory, without Git-root discovery. --all includes stored directories that no longer exist. --all and --directory cannot be combined.",
		Example: "  portbook list\n  portbook list --directory /projects/my-app\n  portbook list --all --output json",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			if cmd.Flags().Changed("all") && cmd.Flags().Changed("directory") {
				return fmt.Errorf("--all and --directory cannot be combined")
			}
			if all {
				return nil
			}
			selected := directory
			if !cmd.Flags().Changed("directory") {
				var err error
				selected, err = os.Getwd()
				if err != nil {
					return &commandError{code: "runtime_error", err: err}
				}
			}
			var err error
			owner, err = booking.Directory(selected)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			c, err := state.Default()
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			entries, err := (booking.Store{Config: config.Store{Coordinator: c}}).List(owner)
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			if err := renderList(out, o.format(), entries, all); err != nil {
				return &commandError{code: "runtime_error", err: err}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "List every owner, including disappeared directories")
	cmd.Flags().StringVar(&directory, "directory", "", "Existing absolute owner directory (default: current working directory)")
	once(cmd.Flags().Lookup("all"))
	once(cmd.Flags().Lookup("directory"))
	return cmd
}

func renderList(w io.Writer, format string, entries []booking.Entry, all bool) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(struct {
			Command  string          `json:"command"`
			Bookings []booking.Entry `json:"bookings"`
		}{"list", entries})
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No bookings found.")
		return err
	}
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if all {
		if _, err := fmt.Fprint(table, "DIRECTORY\t"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(table, "SERVICE\tPORT"); err != nil {
		return err
	}
	for _, entry := range entries {
		if all {
			if _, err := fmt.Fprintf(table, "%s\t", listCell(entry.Directory)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(table, "%s\t%d\n", listCell(entry.Service), entry.Port); err != nil {
			return err
		}
	}
	return table.Flush()
}

// Preserve one table row per booking even when valid identities contain controls.
// JSON retains the exact identity using the encoder's standard escaping.
func listCell(value string) string {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\"\\") || strings.IndexFunc(value, func(r rune) bool { return !strconv.IsPrint(r) }) >= 0 {
		return strconv.Quote(value)
	}
	return value
}
