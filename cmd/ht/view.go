package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht view [--format plain|ansi|html] [--json] <sid>")
	}
	var (
		format  string
		jsonOut bool
	)
	fs.StringVar(&format, "format", "plain", "output format: plain | ansi | html")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON with metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("sid is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.View(fs.Arg(0), format)
	if err != nil {
		return err
	}

	if jsonOut {
		return emitJSON(os.Stdout, res)
	}
	fmt.Print(res.Screen)
	if len(res.Screen) > 0 && res.Screen[len(res.Screen)-1] != '\n' {
		fmt.Println()
	}
	return nil
}
