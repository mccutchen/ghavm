// Package main is the main entrypoint for the ghavm CLI.
package main

import (
	"os"
)

func main() {
	app := newApp(os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	if err := runApp(app, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
