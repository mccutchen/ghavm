package main

import (
	"testing"

	"github.com/mccutchen/ghavm/internal/testing/assert"
)

func TestCLIErrorHandling(t *testing.T) {
	testCases := map[string]struct {
		args           []string
		wantExitCode   int
		wantStderrText string
	}{
		"no command": {
			args:           []string{},
			wantExitCode:   0, // help is shown, no error
			wantStderrText: "",
		},
		"invalid command": {
			args:           []string{"invalid"},
			wantExitCode:   1,
			wantStderrText: `unknown command "invalid"`,
		},
		"missing github token": {
			args:           []string{"list"},
			wantExitCode:   1,
			wantStderrText: "either --github-token/-g flag or GITHUB_TOKEN env var are required",
		},
		"invalid color flag": {
			args:           []string{"list", "--github-token", "fake", "--color", "invalid"},
			wantExitCode:   1,
			wantStderrText: `--color must be one of "auto", "always", or "never"`,
		},
		"invalid upgrade mode": {
			args:           []string{"upgrade", "--github-token", "fake", "--mode", "invalid"},
			wantExitCode:   1,
			wantStderrText: `--mode/-m must be one of "compat" or "latest"`,
		},
		"invalid select pattern": {
			args:           []string{"pin", "--github-token", "fake", "--select", "*/invalid"},
			wantExitCode:   1,
			wantStderrText: "invalid --select pattern: wildcards are only supported at the end of patterns",
		},
		"invalid exclude pattern": {
			args:           []string{"pin", "--github-token", "fake", "--exclude", "invalid*pattern"},
			wantExitCode:   1,
			wantStderrText: "invalid --exclude pattern: wildcards are only supported at the end of patterns",
		},
		"multiple wildcards in exclude": {
			args:           []string{"pin", "--github-token", "fake", "--exclude", "actions/*/*/*"},
			wantExitCode:   1,
			wantStderrText: "invalid --exclude pattern: multiple wildcards not supported",
		},
		"help flag works": {
			args:           []string{"--help"},
			wantExitCode:   0,
			wantStderrText: "",
		},
		"subcommand help works": {
			args:           []string{"pin", "--help"},
			wantExitCode:   0,
			wantStderrText: "",
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			// use empty env for testing CLI errors to ensure we don't
			// accidentally grab a real GITHUB_TOKEN or other env vars.
			getenv := func(string) string { return "" }
			app, _, stderr := newTestApp(getenv)

			err := runApp(app, tc.args)

			// Check exit code
			actualExitCode := 0
			if err != nil {
				actualExitCode = 1
			}
			assert.Equal(t, actualExitCode, tc.wantExitCode, "exit code should match expected")

			// Check stderr contains expected error message
			stderrContent := stderr.String()
			if tc.wantStderrText != "" {
				assert.Contains(t, stderrContent, tc.wantStderrText, "stderr should contain expected error text")
			} else {
				assert.Equal(t, stderrContent, "", "stderr should be empty when no error expected")
			}
		})
	}
}
