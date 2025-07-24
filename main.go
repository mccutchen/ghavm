// Package main is the main entrypoint for the ghavm CLI.
package main

import (
	"os"

	"github.com/mccutchen/ghavm/internal/ghavm"
)

func main() {
	app := ghavm.NewApp(os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	if err := ghavm.RunApp(app, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
