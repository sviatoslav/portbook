package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"portbook/internal/ports"
)

type options struct {
	output        string
	defaultOutput string
}

func (o *options) validate() error {
	if o.output != "table" && o.output != "json" {
		return fmt.Errorf("invalid output %q; expected table or json", o.output)
	}
	return nil
}

func (o *options) format() string {
	if o.validate() != nil {
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
		Short:         "Configure the set of allowed TCP ports",
		Long:          "Portbook configures a set of allowed TCP ports. No booking commands exist yet.\n\nPORTBOOK_CONFIG_DIR overrides the state directory with an absolute path.\nExit codes: 0 success; 1 storage/runtime error; 2 usage error.",
		SilenceErrors: true, SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.SetOut(out)
	root.SetErr(io.Discard)
	// Expose --help on each command, without adding a separate help command.
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.PersistentFlags().StringVar(&o.output, "output", o.output, "Output format: table or json (default: table on a terminal, json otherwise)")
	once(root.PersistentFlags().Lookup("output"))
	root.Args = groupArgs
	root.RunE = requireSubcommand
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
		text := cmd.Long
		if text == "" {
			text = cmd.Short
		}
		text += "\n\n" + cmd.UsageString()
		var err error
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
	config := &cobra.Command{Use: "config", Short: "Show or change configuration", Args: groupArgs, RunE: requireSubcommand}
	ranges := &cobra.Command{Use: "ranges", Short: "Add or remove allowed ports", Args: groupArgs, RunE: requireSubcommand}
	show := &cobra.Command{
		Use: "show", Short: "Show normalized ranges and the total port count", Args: cobra.NoArgs,
		Example: "  portbook --output table config show",
		RunE:    action(o, out, showConfig),
	}
	var addRanges []ports.Range
	add := &cobra.Command{
		Use: "add PORT|START-END [PORT|START-END ...]", Short: "Add ports to the allowed set",
		Example: "  portbook --output table config ranges add 50000-50499\n  portbook --output table config ranges add 51000",
		Args: func(cmd *cobra.Command, args []string) (err error) {
			addRanges, err = parseRanges(cmd, args)
			return err
		},
		RunE: action(o, out, func(ctx context.Context) (result, error) { return addConfig(ctx, addRanges) }),
	}
	var all bool
	var removeRanges []ports.Range
	remove := &cobra.Command{
		Use: "remove [PORT|START-END ...]", Short: "Remove ports from the allowed set",
		Long:    "Remove ports from the allowed set. Supply ports/ranges or --all alone.",
		Example: "  portbook --output table config ranges remove 50000-50499\n  portbook --output table config ranges remove 51000\n  portbook --output table config ranges remove --all",
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
				return clearConfig(ctx)
			}
			return removeConfig(ctx, removeRanges)
		}),
	}
	remove.Flags().BoolVar(&all, "all", false, "Clear the configured pool; cannot be mixed with ports")
	once(remove.Flags().Lookup("all"))
	ranges.AddCommand(add, remove)
	config.AddCommand(show, ranges)
	root.AddCommand(config)
	for _, cmd := range []*cobra.Command{root, config, ranges, show, add, remove} {
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
