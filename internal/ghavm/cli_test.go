package ghavm

import (
	"strings"
	"testing"

	"github.com/mccutchen/ghavm/internal/testing/assert"
)

func TestCLI(t *testing.T) {
	testCases := map[string]struct {
		args       []string
		env        map[string]string
		wantErr    bool
		wantStderr string
	}{
		// basic functionality
		"no command": {
			args:       []string{},
			wantErr:    false, // help is shown, no error
			wantStderr: "",
		},
		"help flag works": {
			args:       []string{"--help"},
			wantErr:    false,
			wantStderr: "",
		},
		"version flag works": {
			args:       []string{"--version"},
			wantErr:    false,
			wantStderr: "",
		},
		"subcommand help works": {
			args:       []string{"pin", "--help"},
			wantErr:    false,
			wantStderr: "",
		},
		// arg validation
		"invalid command": {
			args:       []string{"invalid"},
			wantErr:    true,
			wantStderr: "Error: unknown command \"invalid\" for \"ghavm\"\nRun 'ghavm --help' for usage.",
		},
		"missing github token": {
			args:       []string{"list"},
			wantErr:    true,
			wantStderr: "Error: either --github-token/-g flag or GITHUB_TOKEN env var are required",
		},
		"invalid color flag": {
			args:       []string{"list", "--github-token", "fake", "--color", "invalid"},
			wantErr:    true,
			wantStderr: "Error: --color must be one of: auto, always, never",
		},
		"invalid COLOR env var": {
			args:       []string{"list", "--github-token", "fake"},
			env:        map[string]string{"COLOR": "invalid"},
			wantErr:    true,
			wantStderr: "Error: --color must be one of: auto, always, never",
		},
		"invalid upgrade mode": {
			args:       []string{"upgrade", "--github-token", "fake", "--mode", "invalid"},
			wantErr:    true,
			wantStderr: `Error: --mode/-m must be one of "compat" or "latest"`,
		},
		"invalid select pattern": {
			args:       []string{"pin", "--github-token", "fake", "--select", "*/invalid"},
			wantErr:    true,
			wantStderr: `Error: invalid --select pattern: wildcards are only supported at the end of patterns, got: "*/invalid"`,
		},
		"invalid exclude pattern": {
			args:       []string{"pin", "--github-token", "fake", "--exclude", "invalid*pattern"},
			wantErr:    true,
			wantStderr: `Error: invalid --exclude pattern: wildcards are only supported at the end of patterns, got: "invalid*pattern"`,
		},
		"multiple wildcards in exclude": {
			args:       []string{"pin", "--github-token", "fake", "--exclude", "actions/*/*/*"},
			wantErr:    true,
			wantStderr: `Error: invalid --exclude pattern: multiple wildcards not supported, got: "actions/*/*/*"`,
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			// use fake env for testing CLI errors to ensure we don't
			// accidentally grab a real GITHUB_TOKEN or other env vars.
			getenv := func(key string) string {
				return tc.env[key]
			}
			app, _, stderr := newTestApp(getenv)

			err := RunApp(app, tc.args)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error but got none for args: %v", tc.args)
			} else if !tc.wantErr {
				assert.NilError(t, err)
			}
			got := strings.TrimSpace(stderr.String())
			// t.Logf("\ngot:  %q\nwant: %q", stderr.String(), tc.wantStderr)
			assert.Equal(t, got, tc.wantStderr, "stderr should match expected output for args: %v", tc.args)
		})
	}
}
