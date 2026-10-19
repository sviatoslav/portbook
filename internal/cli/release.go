package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func releaseCommand(o *options, out io.Writer) *cobra.Command {
	var directory, owner, service string
	cmd := &cobra.Command{
		Use: "release SERVICE", Short: "Release an idle booking for a directory and service",
		Long:    "Release one booking only when its TCP port can be bound on every supported address family. Occupied or uncertain ports are retained. A missing booking fails without rewriting the registry. The owner defaults to cwd, without Git root discovery. Configuration and saved path choices are not read.",
		Example: "  portbook release server --directory /projects/my-app\n  portbook release api",
		Args:    serviceOwnerArgs(&directory, &owner, &service),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			coordinator, err := state.Default()
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			res, err := (booking.Store{Config: config.Store{Coordinator: coordinator}}).Release(cmd.Context(), owner, service)
			if err != nil {
				code := "storage_error"
				if errors.Is(err, booking.ErrUnavailable) || errors.Is(err, booking.ErrNotFound) {
					code = "runtime_error"
				}
				return &commandError{code: code, err: err}
			}
			if err := renderRelease(out, o.format(), res); err != nil {
				return &commandError{code: "runtime_error", err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&directory, "directory", "", "Existing absolute owner directory (default: current working directory)")
	once(cmd.Flags().Lookup("directory"))
	cmd.InitDefaultHelpFlag()
	return cmd
}

func renderRelease(out io.Writer, format string, res booking.ReleaseResult) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(struct {
			Command string `json:"command"`
			booking.ReleaseResult
		}{"release", res})
	}
	_, err := fmt.Fprintf(out, "Released port %d for %q in %q.\n", *res.Port, res.Service, res.Directory)
	return err
}
