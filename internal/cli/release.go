package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func releaseCommand(o *options, out io.Writer) *cobra.Command {
	var directory, owner, service string
	var all bool
	cmd := &cobra.Command{
		Use: "release SERVICE | --all", Short: "Delete one booking or all bookings for an owner directory",
		Long:    "Delete registry records without probing TCP ports or stopping listeners. Supply one service or --all for every service of this exact owner, excluding child directories. A missing booking fails without rewriting the registry. The owner defaults to cwd, without Git root discovery. Configuration and saved path choices are not read.",
		Example: "  portbook release server --directory /projects/my-app\n  portbook release api\n  portbook release --all",
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("all") && len(args) != 0 {
				return fmt.Errorf("--all cannot be mixed with a service")
			}
			if !all {
				return serviceOwnerArgs(&directory, &owner, &service)(cmd, args)
			}
			var err error
			owner, err = ownerDirectory(cmd, directory)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			coordinator, err := state.Default()
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			store := booking.Store{Config: config.Store{Coordinator: coordinator}}
			var render func() error
			if all {
				var res booking.ReleaseAllResult
				res, err = store.ReleaseAll(cmd.Context(), owner)
				render = func() error { return renderReleaseAll(out, o.format(), res) }
			} else {
				var res booking.ReleaseResult
				res, err = store.Release(cmd.Context(), owner, service)
				render = func() error { return renderRelease(out, o.format(), res) }
			}
			if err != nil {
				code := "storage_error"
				if errors.Is(err, booking.ErrNotFound) {
					code = "runtime_error"
				}
				return &commandError{code: code, err: err}
			}
			if err := render(); err != nil {
				return &commandError{code: "runtime_error", err: err}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Delete all services of this exact owner; cannot be mixed with a service")
	once(cmd.Flags().Lookup("all"))
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

func renderReleaseAll(out io.Writer, format string, res booking.ReleaseAllResult) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(struct {
			Command string `json:"command"`
			All     bool   `json:"all"`
			booking.ReleaseAllResult
		}{"release", true, res})
	}
	ports := make([]string, len(res.Ports))
	for i, port := range res.Ports {
		ports[i] = strconv.Itoa(port)
	}
	noun := "bookings"
	if res.Released == 1 {
		noun = "booking"
	}
	_, err := fmt.Fprintf(out, "Released %d %s in %q: %s.\n", res.Released, noun, res.Directory, strings.Join(ports, ", "))
	return err
}
