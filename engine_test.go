package main

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mccutchen/ghavm/internal/testing/assert"
	"github.com/spf13/cobra"
)

//go:embed testdata/golden/*.outdir
var goldenDirs embed.FS

func newTestApp(getenv func(string) string) (app *cobra.Command, stdout *bytes.Buffer, stderr *bytes.Buffer) {
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	app = newApp(nil, stdout, stderr, getenv)
	return
}

func TestIntegrationTests(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skipf("set GITHUB_TOKEN to run integration tests")
	}

	t.Parallel()
	t.Logf("Note: to update golden tests, run:\n\n    make test-reset-golden-fixtures\n\n")

	// for testing `ghavm list` we just capture stdout and compare it to
	// a snapshot stored on disk
	for _, forceColor := range []bool{false, true} {
		arg := "--color=never"
		pathSuffix := "-plain"
		if forceColor {
			arg = "--color=always"
			pathSuffix = "-color"
		}
		t.Run("ghavm list "+arg, func(t *testing.T) {
			t.Parallel()

			args := []string{
				"list",
				filepath.Join("testdata", "workflows"),
				"--workers=1", // 1 worker to serialize output for consistency across test runs
			}
			if arg != "" {
				args = append(args, arg)
			}

			app, stdout, _ := newTestApp(os.Getenv) // integration tests use real env
			app.SetArgs(args)
			assert.NilError(t, app.Execute())

			goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("cmd-list%s.stdout", pathSuffix))
			want, err := os.ReadFile(goldenPath)
			assert.NilError(t, err)

			if stdout.String() != string(want) {
				diff := diffStrings(t, string(want), stdout.String())
				t.Errorf("golden test failed: %s:\n\n%s\n\n", goldenPath, diff)
			}
		})
	}

	// for testing `ghavm pin` and `ghavm upgrade`, we need to be able to
	// write to multiple files and compare the results.
	//
	// this setup is a wee bit convoluted, but at a high level:
	//
	// 1. we have golden *directory* snapshots stored under testdata/golden/*.outdir,
	//    created copying pristine testdata from testdata/workflows into each
	//    golden output dir and then running the relevant `ghavm` command to
	//    update the golden output dir in place.
	//
	//    (see `make test-reset-golden-fixtures` for how this happens)
	//
	// 2. we embed those golden directory snapshots in the test binary via
	//    embed.FS
	//
	// 3. each golden dir name (e.g. cmd-pin.outdir) must have an entry in the
	//    goldenDirToArgs map, defining the args needed to recreate the golden
	//    output
	//
	// 4. for each golden dir name in the embedded FS, copy the pristine data
	//    into a new temporary golden dir, then run the specified command
	//    against that golden dir to transform it into its expected state.
	tmpDir := t.TempDir()
	pristineDir := filepath.Join("testdata", "workflows")

	dirEntries, err := goldenDirs.ReadDir(filepath.Join("testdata", "golden"))
	assert.NilError(t, err)

	var goldenDirs []string
	for _, d := range dirEntries {
		if d.IsDir() {
			goldenDirs = append(goldenDirs, d.Name())
		}
	}

	// map golden dir name to the command we need to execute.
	goldenDirToArgs := map[string][]string{
		"cmd-pin.outdir":             {"pin"},
		"cmd-upgrade-default.outdir": {"upgrade"},
		"cmd-upgrade-compat.outdir":  {"upgrade", "--mode", "compat"},
		"cmd-upgrade-latest.outdir":  {"upgrade", "--mode=latest"},
	}

	for _, goldenDirName := range goldenDirs {
		goldenDirName := goldenDirName
		t.Run("golden/"+goldenDirName, func(t *testing.T) {
			t.Parallel()

			goldenDir := filepath.Join("testdata", "golden", goldenDirName)
			testDir := filepath.Join(tmpDir, goldenDir)
			assert.NilError(t, os.CopyFS(testDir, os.DirFS(pristineDir)))

			args := goldenDirToArgs[goldenDirName]
			if args == nil {
				t.Fatalf("no cmd args found for golden dir %s", goldenDirName)
			}
			args = append(args, testDir)
			t.Logf("cli args: %v", args)

			app, _, _ := newTestApp(os.Getenv) // integration tests use real env
			app.SetArgs(args)
			assert.NilError(t, app.Execute())

			if diff := diffDirs(t, goldenDir, testDir); diff != "" {
				t.Fatalf("golden test failed, recursive diff output:\n\n%s\n\n", diff)
			}
		})
	}
}

func diffDirs(t testing.TB, a, b string) string {
	t.Helper()
	cmd := exec.Command("diff", "-u", "-r", a, b)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// an exit code of 1 from `diff` is expected when the inputs differ
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			t.Errorf("diff command failed: %s", err)
		}
	}
	return string(out)
}

func diffStrings(t testing.TB, a, b string) string {
	bashCmd := `exec 3<<<"$1" 4<<<"$2"; diff -u --label want --label got /dev/fd/3 /dev/fd/4`
	cmd := exec.Command("bash", "-c", bashCmd, "bash", a, b)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// an exit code of 1 from `diff` is expected when the inputs differ
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			t.Errorf("diff command failed: %s", err)
		}
	}
	return stdout.String()
}
