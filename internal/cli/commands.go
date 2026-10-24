package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/buildinfo"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
)

type options struct {
	helpErr       error
	installer     skillInstaller
	output        string
	defaultOutput string
	configDir     string
	configFlag    *pflag.Flag
}

func (o *options) configDirSet() bool { return o.configFlag != nil && o.configFlag.Changed }

func (o *options) validate() error {
	if o.configDirSet() {
		if err := config.ValidateDirectory(o.configDir); err != nil {
			return err
		}
		if err := o.validateSelection(o.configDir); err != nil {
			return err
		}
	}
	if o.output != "table" && o.output != "json" {
		return fmt.Errorf("invalid output %q; expected table or json", o.output)
	}
	return nil
}

// Namespace collisions are storage failures; ordinary malformed path operands
// remain usage errors from ValidateDirectory before this root-aware check.
func (o *options) validateSelection(dir string) error {
	l, err := config.DefaultLocation(o.configDir, o.configDirSet())
	if err == nil {
		err = l.ValidateSelection(dir)
	}
	if err != nil {
		return &commandError{code: "storage_error", err: err}
	}
	return nil
}

func (o *options) format() string {
	if o.output != "table" && o.output != "json" {
		return o.defaultOutput
	}
	return o.output
}

// onceValue preserves the existing rejection of repeated --output/--all flags
// while delegating value parsing and flag placement to pflag.
type onceValue struct {
	pflag.Value
	name string
	seen bool
}

func (v *onceValue) Set(s string) error {
	if v.seen {
		return fmt.Errorf("--%s may only be specified once", v.name)
	}
	v.seen = true
	return v.Value.Set(s)
}

func once(flag *pflag.Flag) { flag.Value = &onceValue{Value: flag.Value, name: flag.Name} }

func newCommandTree(o *options, out io.Writer) *cobra.Command {
	root := o.command(&cobra.Command{
		Use:           "portbook",
		Short:         "Configure and reserve TCP ports",
		Long:          "Portbook configures and reserves TCP ports for directory/service identities.\n\nSelect configuration with --config-dir, PORTBOOK_CONFIG_DIR, a saved choice, or the OS default.\nThe shared lock remains in the OS default directory.\nExit codes: 0 success; 1 storage/runtime error; 2 usage error.",
		SilenceErrors: true, SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}, commandPolicy{help: helpConfigSelection})
	root.SetOut(out)
	root.SetErr(io.Discard)
	// Expose --help on each command, without adding a separate help command.
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.PersistentFlags().StringVar(&o.configDir, "config-dir", "", "Temporary absolute config directory (takes precedence over environment and saved choice)")
	o.configFlag = root.PersistentFlags().Lookup("config-dir")
	once(o.configFlag)
	root.Args = groupArgs
	var version bool
	root.Flags().BoolVar(&version, "version", false, "Print version and commit without reading state")
	once(root.Flags().Lookup("version"))
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if !version {
			return requireSubcommand(cmd, args)
		}
		if err := o.validate(); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, buildinfo.String()); err != nil {
			return &commandError{code: "runtime_error", err: err}
		}
		return nil
	}
	configCmd := o.command(&cobra.Command{Use: "config", Short: "Show or change configuration", Args: groupArgs, RunE: requireSubcommand}, commandPolicy{help: helpConfigSelection})
	show := o.command(&cobra.Command{
		Use: "show", Short: "Show normalized ranges and the total port count",
		Long: "Show the effective pool. Missing configuration uses 49152-65535 (16,384 ports); an explicit empty configuration stays empty. Reading never saves configuration.", Args: cobra.NoArgs,
		Example: "  portbook config show --output table",
		RunE:    action(o, out, o.showConfig),
	}, commandPolicy{help: helpConfigSelection, structuredOutput: true})
	var addRanges []ports.Range
	add := o.command(&cobra.Command{
		Use: "add PORT|START-END [PORT|START-END ...]", Short: "Add ports to the allowed set",
		Long:    "Add ports to the effective set. With no saved config, additions form a union with the default pool (49152-65535). To replace the pool, clear it before adding custom ports. Success always prints a plain confirmation; --output is not supported.",
		Example: "  portbook config add 50000-50499\n  portbook config add 51000",
		Args: func(cmd *cobra.Command, args []string) (err error) {
			addRanges, err = parseRanges(cmd, args)
			return err
		},
		RunE: action(o, out, func(ctx context.Context) (result, error) { return o.addConfig(ctx, addRanges) }),
	}, commandPolicy{help: helpConfigSelection})
	var all bool
	var removeRanges []ports.Range
	remove := o.command(&cobra.Command{
		Use: "remove [PORT|START-END ...]", Short: "Remove ports from the allowed set",
		Long:    "Remove ports from the effective set, including defaults when no config is saved. Supply ports/ranges or --all alone. Clearing the pool disables new allocations but preserves existing bookings. Success always prints a plain confirmation; --output is not supported.",
		Example: "  portbook config remove 50000-50499\n  portbook config remove 51000\n  portbook config remove --all",
		Args: func(cmd *cobra.Command, args []string) (err error) {
			if cmd.Flags().Changed("all") && len(args) != 0 {
				return fmt.Errorf("--all cannot be mixed with port operands")
			}
			if all {
				return nil
			}
			removeRanges, err = parseRanges(cmd, args)
			return err
		},
		RunE: action(o, out, func(ctx context.Context) (result, error) {
			if all {
				return o.clearConfig(ctx)
			}
			return o.removeConfig(ctx, removeRanges)
		}),
	}, commandPolicy{help: helpConfigSelection})
	remove.Flags().BoolVar(&all, "all", false, "Clear the configured pool; cannot be mixed with ports")
	once(remove.Flags().Lookup("all"))
	resetPool := o.command(&cobra.Command{
		Use: "reset", Short: "Restore the selected pool to the default ports",
		Long:    "Atomically restore the selected configuration's pool to 49152-65535 (16,384 ports). Accepts no operands. Preserves bookings, listeners, and the saved location choice. Like other pool mutations, saves explicit defaults even when unchanged or missing; corrupt or unreadable configuration fails without replacement. The plain confirmation counts added and removed port memberships; --output is not supported. This is distinct from config path reset, which removes only the saved location choice.",
		Example: "  portbook config reset",
		Args:    cobra.NoArgs, RunE: action(o, out, o.resetConfig),
	}, commandPolicy{help: helpConfigSelection})
	path := o.command(&cobra.Command{Use: "path", Short: "Discover or persistently select the config directory", Args: groupArgs, RunE: requireSubcommand}, commandPolicy{help: helpConfigSelection})
	get := o.command(&cobra.Command{Use: "get", Short: "Report the effective directory, config file, and selection source without writing files", Args: cobra.NoArgs, RunE: action(o, out, o.pathGet)}, commandPolicy{help: helpConfigSelection, structuredOutput: true})
	var target string
	set := o.command(&cobra.Command{Use: "set /absolute/path", Short: "Save a directory choice; does not migrate existing configuration", Long: "Save a directory choice in the OS default Portbook directory. Temporary overrides remain effective. Selecting an uninitialized directory uses the default pool; no files are migrated. Success always prints saved and effective directory details; --output is not supported.", Example: "  portbook config path set /absolute/path", Args: func(cmd *cobra.Command, args []string) error {
		help, _ := cmd.Flags().GetBool("help")
		if len(args) == 0 && help {
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("%s requires one absolute directory path", cmd.CommandPath())
		}
		if err := config.ValidateDirectory(args[0]); err != nil {
			return err
		}
		if err := o.validateSelection(args[0]); err != nil {
			return err
		}
		target = args[0]
		return nil
	}, RunE: action(o, out, func(ctx context.Context) (result, error) { return o.pathChange(ctx, target, false) })}, commandPolicy{help: helpConfigRepair})
	reset := o.command(&cobra.Command{Use: "reset", Short: "Remove only the saved choice; preserve configurations and active overrides", Long: "Remove only the saved location choice. Always print plain saved and effective directory details, including active overrides; --output is not supported.", Example: "  portbook config path reset\n  portbook --config-dir /temporary/path config path reset", Args: cobra.NoArgs, RunE: action(o, out, func(ctx context.Context) (result, error) { return o.pathChange(ctx, "", true) })}, commandPolicy{help: helpConfigRepair})
	path.AddCommand(get, set, reset)
	configCmd.AddCommand(show, add, remove, resetPool, path)
	var owner, directory, service string
	reserve := o.command(&cobra.Command{
		Use: "reserve SERVICE", Short: "Reserve a TCP port for a directory and service",
		Long:    "Reserve the lowest available allowed TCP port for a directory and service. No configuration is needed: a missing config uses 49152-65535 (16,384 ports). An existing booking fails with reservation already exists. Use get to read it. Success always prints only the decimal port, and does not accept --output. The owner defaults to the current directory; Git roots are not detected.",
		Example: "  portbook reserve server --directory /projects/my-app\n  portbook reserve api",
		Args:    serviceOwnerArgs(&directory, &owner, &service),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			cfg, err := config.WithOverride(o.configDir, o.configDirSet())
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			port, err := (booking.Store{Coordinator: cfg.Coordinator, ReadPool: cfg.Read}).Reserve(cmd.Context(), owner, service)
			if err != nil {
				code := "storage_error"
				if errors.Is(err, booking.ErrExhausted) || errors.Is(err, booking.ErrAlreadyExists) {
					code = "runtime_error"
				}
				return &commandError{code: code, err: err}
			}
			if _, err := fmt.Fprintf(out, "%d\n", port); err != nil {
				return &commandError{code: "runtime_error", err: err}
			}
			return nil
		},
	}, commandPolicy{help: helpConfigSelection})
	reserve.Flags().StringVar(&directory, "directory", "", "Existing absolute owner directory (default: current working directory)")
	once(reserve.Flags().Lookup("directory"))
	root.AddCommand(configCmd, getCommand(o, out), reserve, newListCommand(o, out), releaseCommand(o, out), cleanupCommand(o, out), installSkillsCommand(o))
	return root
}

func groupArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q for %s; use --help", args[0], cmd.CommandPath())
	}
	return nil
}

func requireSubcommand(cmd *cobra.Command, _ []string) error {
	return fmt.Errorf("%s requires a subcommand; use --help", cmd.CommandPath())
}

func parseRanges(cmd *cobra.Command, args []string) ([]ports.Range, error) {
	help, _ := cmd.Flags().GetBool("help")
	if len(args) == 0 && !help {
		return nil, fmt.Errorf("%s requires a port or range", cmd.CommandPath())
	}
	ranges := make([]ports.Range, 0, len(args))
	for _, arg := range args {
		r, err := ports.Parse(arg)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

func action(o *options, out io.Writer, execute func(context.Context) (result, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if err := o.validate(); err != nil {
			return err
		}
		res, err := execute(cmd.Context())
		if err != nil {
			return &commandError{code: "storage_error", err: err}
		}
		if err := render(out, o.format(), res); err != nil {
			return &commandError{code: "runtime_error", err: err}
		}
		return nil
	}
}
