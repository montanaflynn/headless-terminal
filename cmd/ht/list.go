package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.List()
	if err != nil {
		return err
	}

	if jsonOut || !stdoutIsTTY() {
		return emitJSON(os.Stdout, res)
	}
	if len(res.Sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATE\tPID\tSIZE\tAGE\tCMD")
	for _, s := range res.Sessions {
		age := time.Since(s.StartedAt).Round(time.Second)
		cmd := strings.Join(s.Cmd, " ")
		name := s.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%dx%d\t%s\t%s\n",
			s.ID, name, s.State, s.PID, s.Cols, s.Rows, age, cmd)
	}
	return tw.Flush()
}
