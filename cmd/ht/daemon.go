package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"headless-terminal/internal/client"
	"headless-terminal/internal/daemon"
)

// cmdDaemon implements `ht daemon` (run the server) and `ht daemon stop`.
// Normally users never invoke this directly; clients auto-start the
// daemon when the socket is not reachable.
func cmdDaemon(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "stop":
			return daemonStop()
		default:
			return fmt.Errorf("ht daemon: unknown subcommand %q", args[0])
		}
	}
	return daemonRun()
}

func daemonRun() error {
	socket := os.Getenv("HT_SOCKET")
	if socket == "" {
		socket = daemon.DefaultSocketPath()
	}

	d := daemon.New(socket)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Shut down gracefully on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "ht daemon listening on %s\n", socket)
	if err := d.Run(ctx); err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	return nil
}

func daemonStop() error {
	c := client.New(os.Getenv("HT_SOCKET"))
	err := c.DaemonStop()
	if err == nil {
		return nil
	}
	// If the daemon is already down, treat as success.
	var opErr *os.PathError
	if errors.As(err, &opErr) {
		return nil
	}
	return err
}
