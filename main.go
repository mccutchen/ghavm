// Package main is the main entrypoint for the ghavm CLI.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/mccutchen/ghavm/internal/ghavm"
)

// Release information populated by goreleaser at build time
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	versionInfo := fmt.Sprintf("ghavm version %s %s %s", version, commit, runtime.Version())
	app := ghavm.NewApp(os.Stdin, os.Stdout, os.Stderr, os.Getenv, versionInfo)
	if err := ghavm.RunApp(app, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
