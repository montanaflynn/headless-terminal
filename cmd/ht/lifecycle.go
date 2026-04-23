package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"time"
)

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: ht stop [--timeout 5s] <sid>") }
	var timeout time.Duration
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "grace period before SIGKILL escalation")
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
	return c.Stop(fs.Arg(0), timeout)
}

func cmdKill(args []string) error {
	fs := flag.NewFlagSet("kill", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: ht kill <sid>") }
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
	return c.Kill(fs.Arg(0))
}

func cmdRemove(args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: ht remove [--force] <sid>") }
	var force bool
	fs.BoolVar(&force, "force", false, "remove even if running")
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
	return c.Remove(fs.Arg(0), force)
}
