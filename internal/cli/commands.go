package cli

import (
	"context"
	"encoding/json"
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

func newCommandTree(o *options, out io.Writer, helpErr *error) *cobra.Command {
	root := &cobra.Command{
		Use:           "portbook",
		Short:         "Configure and reserve TCP ports",
		Long:          "Portbook configures and reserves TCP ports for directory/service identities.\n\nSelect configuration with --config-dir, PORTBOOK_CONFIG_DIR, a saved choice, or the OS default.\nThe shared lock remains in the OS default directory.\nExit codes: 0 success; 1 storage/runtime error; 2 usage error.",
		SilenceErrors: true, SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
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
	repairHelp := map[*cobra.Command]bool{}
	lookup := getCommand(o, out)
	release := releaseCommand(o, out)
	cleanup := cleanupCommand(o, out)
	list := newListCommand(o, out)
	install := installSkillsCommand(o)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		// Cobra parses flags before help but intentionally skips Args. Validate
		// this command's supplied operands too, permitting only missing operands.
		if err := o.validate(); err != nil {
			*helpErr = err
			return
		}
		if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
			*helpErr = err
			return
		}
		// Get, list, release, and cleanup use only the fixed registry, including for help.
		// Explicit path flags were validated above. Repair commands can bypass
		// a broken saved pointer just as their execution does.
		var err error
		if cmd != lookup && cmd != release && cmd != list && cmd != cleanup && cmd != install {
			var l config.Location
			l, err = config.DefaultLocation(o.configDir, o.configDirSet())
			if err == nil && !repairHelp[cmd] {
				_, err = l.Resolve()
			}
		}
		if err != nil {
			*helpErr = &commandError{code: "storage_error", err: err}
			return
		}
		text := cmd.Long
		if text == "" {
			text = cmd.Short
		}
		text += "\n\n" + cmd.UsageString()
		err = nil
		if o.format() == "json" {
			err = json.NewEncoder(out).Encode(struct {
				Command string `json:"command"`
				Help    string `json:"help"`
			}{"help", text})
		} else {
			_, err = io.WriteString(out, text)
		}
		if err != nil {
			*helpErr = &commandError{code: "runtime_error", err: err}
		}
	})
	configCmd := &cobra.Command{Use: "config", Short: "Show or change configuration", Args: groupArgs, RunE: requireSubcommand}
	configCmd.PersistentFlags().StringVar(&o.output, "output", o.output, "Output format: table or json (default: table on a terminal, json otherwise)")
	once(configCmd.PersistentFlags().Lookup("output"))
	for _, cmd := range []*cobra.Command{list, cleanup} {
		cmd.Flags().StringVar(&o.output, "output", o.output, "Output format: table or json (default: table on a terminal, json otherwise)")
		once(cmd.Flags().Lookup("output"))
	}
	show := &cobra.Command{
		Use: "show", Short: "Show normalized ranges and the total port count",
		Long: "Show the effective pool. Missing configuration uses 49152-65535 (16,384 ports); an explicit empty configuration stays empty. Reading never saves configuration.", Args: cobra.NoArgs,
		Example: "  portbook config show --output table",
		RunE:    action(o, out, o.showConfig),
	}
	var addRanges []ports.Range
	add := &cobra.Command{
		Use: "add PORT|START-END [PORT|START-END ...]", Short: "Add ports to the allowed set",
		Long:    "Add ports to the effective set. With no saved config, additions form a union with the default pool (49152-65535). To replace the pool, clear it before adding custom ports.",
		Example: "  portbook config add --output table 50000-50499\n  portbook config add --output table 51000",
		Args: func(cmd *cobra.Command, args []string) (err error) {
			addRanges, err = parseRanges(cmd, args)
			return err
		},
		RunE: action(o, out, func(ctx context.Context) (result, error) { return o.addConfig(ctx, addRanges) }),
	}
	var all bool
	var removeRanges []ports.Range
	remove := &cobra.Command{
		Use: "remove [PORT|START-END ...]", Short: "Remove ports from the allowed set",
		Long:    "Remove ports from the effective set, including defaults when no config is saved. Supply ports/ranges or --all alone. Clearing the pool disables new allocations but preserves existing bookings.",
		Example: "  portbook config remove --output table 50000-50499\n  portbook config remove --output table 51000\n  portbook config remove --output table --all",
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
	}
	remove.Flags().BoolVar(&all, "all", false, "Clear the configured pool; cannot be mixed with ports")
	once(remove.Flags().Lookup("all"))
	resetPool := &cobra.Command{
		Use: "reset", Short: "Restore the selected pool to the default ports",
		Long:    "Atomically restore the selected configuration's pool to 49152-65535 (16,384 ports). Accepts no operands. Preserves bookings, listeners, and the saved location choice. Like other pool mutations, saves explicit defaults even when unchanged or missing; corrupt or unreadable configuration fails without replacement. Changed counts added and removed port memberships. This is distinct from config path reset, which removes only the saved location choice.",
		Example: "  portbook config reset --output table",
		Args:    cobra.NoArgs, RunE: action(o, out, o.resetConfig),
	}
	path := &cobra.Command{Use: "path", Short: "Discover or persistently select the config directory", Args: groupArgs, RunE: requireSubcommand}
	get := &cobra.Command{Use: "get", Short: "Report the effective directory, config file, and selection source without writing files", Args: cobra.NoArgs, RunE: action(o, out, o.pathGet)}
	var target string
	set := &cobra.Command{Use: "set /absolute/path", Short: "Save a directory choice; does not migrate existing configuration", Long: "Save a directory choice in the OS default Portbook directory. Temporary overrides remain effective. Selecting an uninitialized directory uses the default pool; no files are migrated.", Example: "  portbook config path set /absolute/path", Args: func(cmd *cobra.Command, args []string) error {
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
	}, RunE: action(o, out, func(ctx context.Context) (result, error) { return o.pathChange(ctx, target, false) })}
	reset := &cobra.Command{Use: "reset", Short: "Remove only the saved choice; preserve configurations and active overrides", Example: "  portbook config path reset\n  portbook --config-dir /temporary/path config path reset", Args: cobra.NoArgs, RunE: action(o, out, func(ctx context.Context) (result, error) { return o.pathChange(ctx, "", true) })}
	repairHelp[set], repairHelp[reset] = true, true
	path.AddCommand(get, set, reset)
	configCmd.AddCommand(show, add, remove, resetPool, path)
	var owner, directory, service string
	reserve := &cobra.Command{
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
			port, err := (booking.Store{Config: cfg}).Reserve(cmd.Context(), owner, service)
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
	}
	reserve.Flags().StringVar(&directory, "directory", "", "Existing absolute owner directory (default: current working directory)")
	once(reserve.Flags().Lookup("directory"))
	root.AddCommand(configCmd, lookup, reserve, list, release, cleanup, install)
	for _, cmd := range []*cobra.Command{root, configCmd, show, add, remove, path, get, set, reset, reserve, list} {
		cmd.InitDefaultHelpFlag()
	}
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
