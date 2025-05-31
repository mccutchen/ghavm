// Package main is the main entrypoint for the ghavm CLI.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/mccutchen/ghavm/internal/slogctx"
	"github.com/spf13/cobra"
)

func main() {
	app := newApp(os.Stdin, os.Stdout, os.Stderr)
	if err := app.Execute(); err != nil {
		os.Exit(1)
	}
}

func newApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "ghavm",
		Short: "ghavm manages version pinning and upgrades for GitHub Actions workflows.",
		// Don't print usage when invoked command returns an error
		SilenceUsage: true,
	}

	listCmd := &cobra.Command{
		Use:   "list [path...]",
		Short: "List current action versions and available upgrades",
		Example: `  # list versions and available upgrades for all actions in the
  # current repo
  ghavm list

  # list actions in a specific file
  ghavm list .github/workflows/my-workflow.yaml

  # list version and available upgrades for all 'actions/setup-go'
  # actions in the current repo
  ghavm pin --target actions/setup-go`,
		RunE: listCmd,
	}

	pinCmd := &cobra.Command{
		Use:   "pin [path...]",
		Short: "Pin current action versions to immutable commit hashes",
		Example: `  # pin the versions of all actions in the current repo
  ghavm pin

  # pin the versions of all actions in a specific file
  ghavm pin .github/workflows/my-workflow.yaml

  # pin all 'actions/setup-go' actions in the current repo
  ghavm pin --target actions/setup-go`,
		RunE: pinOrUpgradeCmd,
	}

	upgradeCmd := &cobra.Command{
		Use:   "upgrade [flags] [path...]",
		Short: "Upgrade and re-pin action versions according to --mode",
		Long: strings.TrimSpace(`
Upgrade and re-pin action versions according to --mode.

Available modes:
  --mode=compat (default)
      chooses the newest release with the same major version
      as the action's current version

  --mode=latest
      chooses the newest release regardless of major version
`),
		Example: `  # upgrade all actions in the current repo to latest compat release
  ghavm upgrade
  ghavm upgrade --mode=compat

  # upgrade all actions in the current repo to absolute latest release
  ghavm upgrade --mode=latest

  # upgrade all actions in a specific file
  ghavm upgrade .github/workflows/my-workflow.yaml

  # upgrade 'actions/setup-go' actions in the current repo to the
  # latest release, regardless of major version
  ghavm upgrade --target actions/setup-go --mode=latest`,
		RunE: pinOrUpgradeCmd,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			mode := cmd.Flag("mode").Value.String()
			if mode != "compat" && mode != "latest" {
				return fmt.Errorf("--mode/-m must be one of \"compat\" or \"latest\"")
			}
			return nil
		},
	}
	upgradeCmd.Flags().StringP("mode", "m", "compat", "Upgrade mode")

	// define common arguments for all commands that resolve action versions
	// (which is every command today, but might not be in the future, so we
	// don't want to define these on the root command)
	for _, cmd := range []*cobra.Command{listCmd, pinCmd, upgradeCmd} {
		cmd.Flags().StringP("github-token", "g", "", "GitHub access token (default: GITHUB_TOKEN env value)")
		cmd.Flags().StringSliceP("targets", "t", nil, "Limit upgrades to specific actions (e.g. --target actions/checkout)")
		cmd.Flags().IntP("jobs", "j", runtime.NumCPU(), "Limit parallelism when accessing the GitHub API")
		cmd.Flags().BoolP("verbose", "v", false, "Enable verbose logging")
		cmd.Flags().Bool("strict", false, "Strict mode, abort on any error")

		// set up env var handling
		cmd.PreRunE = wrapPreRunE(cmd, func(cmd *cobra.Command, _ []string) error {
			// github-token is required, but we will also take the value from
			// the GITHUB_TOKEN env var if found.
			if f := cmd.Flag("github-token"); !f.Changed {
				if token := os.Getenv("GITHUB_TOKEN"); token != "" {
					if err := f.Value.Set(token); err != nil {
						return fmt.Errorf("internals: failed to set value of github-token flag: %w", err)
					}
				} else {
					return fmt.Errorf("either --github-token/-g flag or GITHUB_TOKEN env var are required")
				}
			}

			// verbose flag is optional, but we also support setting via env vars
			if f := cmd.Flag("verbose"); !f.Changed {
				if verbose := os.Getenv("VERBOSE"); verbose != "" && verbose != "0" && verbose != "false" {
					if err := f.Value.Set("true"); err != nil {
						return fmt.Errorf("internals: failed to set value of verbose flag: %w", err)
					}
				}
			}

			return nil
		})
	}

	// add subcommands to our root command
	rootCmd.AddCommand(listCmd, pinCmd, upgradeCmd)

	// wire up I/O
	rootCmd.SetIn(stdin)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	// disable or hide subcommands cobra adds by default
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	rootCmd.CompletionOptions = cobra.CompletionOptions{HiddenDefaultCmd: true}

	return rootCmd
}

func listCmd(cmd *cobra.Command, args []string) error {
	var (
		flags      = cmd.Flags()
		token, _   = flags.GetString("github-token")
		targets, _ = flags.GetStringSlice("target")
		jobs, _    = flags.GetInt("jobs")
		strict, _  = flags.GetBool("strict")
		verbose, _ = flags.GetBool("verbose")
	)
	var (
		ctx      = newAppContext(context.Background(), cmd.ErrOrStderr(), chooseLogLevel(verbose))
		ghClient = NewGitHubClient(token, nil)
	)

	// ensure our auth token is valid
	if _, err := ghClient.ValidateAuth(ctx); err != nil {
		return fmt.Errorf("GitHub authentication failed: %s", err)
	}

	// find workflow files to work on
	files, err := FindWorkflows(args)
	if err != nil {
		return fmt.Errorf("error finding workflow files: %s", err)
	}
	if len(files) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: no workflows found")
		return nil
	}

	// scan workflow files for action steps to upgrade
	root, err := ScanWorkflows(files, targets)
	if err != nil {
		return fmt.Errorf("failed to scan workflow files: %w", err)
	}

	engine := newEngine(root, ghClient, jobs, cmd.ErrOrStderr(), strict, verbose)
	if err := engine.List(ctx, cmd.OutOrStdout()); err != nil {
		return err
	}
	return nil
}

func pinOrUpgradeCmd(cmd *cobra.Command, args []string) error {
	var (
		flags      = cmd.Flags()
		token, _   = flags.GetString("github-token")
		targets, _ = flags.GetStringSlice("target")
		jobs, _    = flags.GetInt("jobs")
		strict, _  = flags.GetBool("strict")
		verbose, _ = flags.GetBool("verbose")
	)
	var (
		ctx      = newAppContext(context.Background(), cmd.ErrOrStderr(), chooseLogLevel(verbose))
		ghClient = NewGitHubClient(token, nil)
	)

	var mode PinMode
	if cmd.Name() == "pin" {
		mode = ModeCurrent
	} else {
		modeStr, _ := flags.GetString("mode")
		switch modeStr {
		case "latest":
			mode = ModeLatest
		case "compat":
			mode = ModeCompat
		default:
			panic("invalid upgrade mode: " + modeStr)
		}
	}

	// ensure our auth token is valid
	if _, err := ghClient.ValidateAuth(ctx); err != nil {
		return fmt.Errorf("GitHub authentication failed: %s", err)
	}

	// find workflow files to work on
	files, err := FindWorkflows(args)
	if err != nil {
		return fmt.Errorf("error finding workflow files: %s", err)
	}
	if len(files) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: no workflows found")
		return nil
	}

	// scan workflow files for action steps to upgrade
	root, err := ScanWorkflows(files, targets)
	if err != nil {
		return fmt.Errorf("failed to scan workflow files: %w", err)
	}

	// pin or upgrade actions
	engine := newEngine(root, ghClient, jobs, cmd.ErrOrStderr(), strict, verbose)
	if err := engine.Pin(ctx, mode); err != nil {
		return err
	}
	return nil
}

func newAppContext(ctx context.Context, out io.Writer, level slog.Level) context.Context {
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: level,
	}))
	return slogctx.New(ctx, logger)
}

func chooseLogLevel(verbose bool) slog.Level {
	if verbose {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

// wrapPreRunE acts as a "middleware" for cobra Command.PreRunE functions.
func wrapPreRunE(cmd *cobra.Command, newPreRunE preRunE) preRunE {
	if cmd.PreRunE == nil {
		return newPreRunE
	}
	oldPreRunE := cmd.PreRunE
	return func(cmd *cobra.Command, args []string) error {
		if err := oldPreRunE(cmd, args); err != nil {
			return err
		}
		return newPreRunE(cmd, args)
	}
}

type preRunE func(cmd *cobra.Command, args []string) error
