// Package main is the main entrypoint for the ghavm CLI.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/mccutchen/ghavm/internal/slogctx"
)

func main() {
	app := newApp(os.Stdin, os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "ghavm",
		Usage: "GitHub Actions Version Manager",
		Arguments: []cli.Argument{
			&cli.StringArgs{
				Name: "paths",
				Min:  0,
				Max:  256, // arbitrary number
			},
		},
		// Global flags for all subcommands
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "github-token",
				Usage:    "GitHub access token",
				Sources:  cli.EnvVars("GITHUB_TOKEN"),
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:    "targets",
				Aliases: []string{"t"},
				Usage:   "Limit upgrades to specific actions (e.g. --target actions/checkout)",
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Enable verbose debug logging",
				Sources: cli.EnvVars("VERBOSE"),
				Value:   false,
			},
			&cli.IntFlag{
				Name:    "parallelism",
				Aliases: []string{"j"},
				Usage:   "Limit parallelism (defaults to CPU count)",
				Value:   runtime.NumCPU(),
			},
		},
		// Subcommands
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "List current action versions and available upgrades",
				Action:    listCmd,
				Reader:    stdin,
				Writer:    stdout,
				ErrWriter: stderr,
			},
			{
				Name:      "pin",
				Usage:     "Pin current action versions to commit hashes",
				Action:    pinOrUpgradeCmd,
				Reader:    stdin,
				Writer:    stdout,
				ErrWriter: stderr,
			},
			{
				Name:   "upgrade",
				Usage:  "Upgrade and re-pin action versions",
				Action: pinOrUpgradeCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "mode",
						Aliases: []string{"m"},
						Usage:   "Upgrade mode, either 'latest' or 'compat'",
						Value:   "compat",
						Action: func(_ context.Context, _ *cli.Command, val string) error {
							if val != "latest" && val != "compat" {
								return fmt.Errorf("invalid mode: %s (must be 'latest' or 'compat')", val)
							}
							return nil
						},
					},
				},
				Reader:    stdin,
				Writer:    stdout,
				ErrWriter: stderr,
			},
		},
		Reader:    stdin,
		Writer:    stdout,
		ErrWriter: stderr,
	}
}

func listCmd(ctx context.Context, cmd *cli.Command) error {
	var (
		token       = cmd.String("github-token")
		targets     = cmd.StringSlice("targets")
		parallelism = cmd.Int("parallelism")
		ghClient    = NewGitHubClient(token, nil)
	)
	ctx = newAppContext(ctx, cmd.ErrWriter, chooseLogLevel(cmd.Bool("verbose")))

	// find workflow files to work on
	files, err := FindWorkflows(cmd.Args().Slice())
	if err != nil {
		return fmt.Errorf("error finding workflow files: %s", err)
	}
	if len(files) == 0 {
		fmt.Fprintln(cmd.ErrWriter, "warning: no workflows found")
		return nil
	}

	// scan workflow files for action steps to upgrade
	root, err := ScanWorkflows(files, targets)
	if err != nil {
		return fmt.Errorf("failed to scan workflow files: %w", err)
	}

	engine := newEngine(root, ghClient, parallelism, cmd.ErrWriter)
	if err := engine.List(ctx, cmd.Writer); err != nil {
		return fmt.Errorf("engine error: %w", err)
	}
	return nil
}

func pinOrUpgradeCmd(ctx context.Context, cmd *cli.Command) error {
	var (
		token       = cmd.String("github-token")
		targets     = cmd.StringSlice("targets")
		parallelism = cmd.Int("parallelism")
		ghClient    = NewGitHubClient(token, nil)
	)
	ctx = newAppContext(ctx, cmd.ErrWriter, chooseLogLevel(cmd.Bool("verbose")))

	var mode PinMode
	if cmd.Name == "pin" {
		mode = ModeCurrent
	} else {
		modeStr := cmd.String("mode")
		switch modeStr {
		case "latest":
			mode = ModeLatest
		case "compat":
			mode = ModeCompat
		default:
			panic("invalid upgrade mode: " + modeStr)
		}
	}

	// find workflow files to work on
	files, err := FindWorkflows(cmd.Args().Slice())
	if err != nil {
		return cli.Exit(fmt.Errorf("error finding workflow files: %s", err), 1)
	}
	if len(files) == 0 {
		fmt.Fprintln(cmd.ErrWriter, "warning: no workflows found")
		return nil
	}

	// scan workflow files for action steps to upgrade
	root, err := ScanWorkflows(files, targets)
	if err != nil {
		return fmt.Errorf("failed to scan workflow files: %w", err)
	}

	// pin or upgrade actions
	engine := newEngine(root, ghClient, parallelism, cmd.ErrWriter)
	if err := engine.Pin(ctx, mode); err != nil {
		return fmt.Errorf("engine error: %w", err)
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
