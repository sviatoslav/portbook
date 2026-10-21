package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func cleanupCommand(o *options, out io.Writer) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{Use: "cleanup", Short: "Remove bookings only for deleted owners with unused TCP ports", Long: "Check only recorded owners. Retain existing, inaccessible, or uncertain owners and busy or uncertain ports. --dry-run previews this snapshot without writing files or locks.", Example: "  portbook cleanup --dry-run\n  portbook cleanup --output table", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.validate(); err != nil {
			return err
		}
		c, err := state.Default()
		if err != nil {
			return &commandError{code: "storage_error", err: err}
		}
		return executeCleanup(cmd.Context(), booking.Store{Config: config.Store{Coordinator: c}}, dryRun, out, o.format())
	}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview proposed removals without writing state")
	once(cmd.Flags().Lookup("dry-run"))
	cmd.InitDefaultHelpFlag()
	return cmd
}

// Keep mutation and reporting together so output failures cannot undo a save.
func executeCleanup(ctx context.Context, store booking.Store, dryRun bool, out io.Writer, format string) error {
	report, err := store.Cleanup(ctx, dryRun)
	if err != nil {
		return &commandError{code: "storage_error", err: err}
	}
	if err := renderCleanup(out, format, report); err != nil {
		return &commandError{code: "runtime_error", err: err}
	}
	return nil
}

func renderCleanup(w io.Writer, format string, r booking.CleanupReport) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(r)
	}
	if len(r.Rows) > 0 {
		table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(table, "DIRECTORY\tSERVICE\tPORT\tACTION\tREASON"); err != nil {
			return err
		}
		for _, row := range r.Rows {
			// Quote identities so tabs/newlines cannot impersonate report rows.
			if _, err := fmt.Fprintf(table, "%q\t%q\t%d\t%s\t%s\n", row.Directory, row.Service, row.Port, row.Action, row.Reason); err != nil {
				return err
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Removed: %d. Would remove: %d. Retained: %d.\n", r.Removed, r.WouldRemove, r.Retained)
	return err
}
