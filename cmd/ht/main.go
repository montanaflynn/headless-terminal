package main

import (
	"fmt"
	"os"
)

const usage = `ht - headless terminal, puppeteer for TUIs

Usage:
  ht run <cmd...>              start a session (prints session ID)
  ht list                      list sessions
  ht stop <sid>                graceful shutdown (SIGTERM)
  ht kill <sid>                immediate (SIGKILL)
  ht remove <sid>              remove an exited session
  ht send <sid> <keys...>      send keystrokes (vim-style notation)
                               optional compound: --wait-* and --view
  ht view <sid>                snapshot current screen
  ht watch <sid>               live stream output (Ctrl-C to exit)
  ht wait <sid> <conditions>   block until condition met

  ht debug <cmd...>            foreground: run a command with libghostty
                               in parallel, print final snapshot on exit.
                               No daemon, no session — VT-stack diagnostic.

  ht daemon [stop]             daemon admin (normally auto-started)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	sub := os.Args[1]
	args := os.Args[2:]

	var err error
	switch sub {
	case "run":
		err = cmdRun(args)
	case "list":
		err = cmdList(args)
	case "stop":
		err = cmdStop(args)
	case "kill":
		err = cmdKill(args)
	case "remove":
		err = cmdRemove(args)
	case "send":
		err = cmdSend(args)
	case "view":
		err = cmdView(args)
	case "watch":
		err = cmdWatch(args)
	case "wait":
		err = cmdWait(args)
	case "daemon":
		err = cmdDaemon(args)
	case "debug":
		err = cmdDebug(args)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "ht: unknown subcommand %q (try `ht help`)\n", sub)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ht:", err)
		os.Exit(1)
	}
}
