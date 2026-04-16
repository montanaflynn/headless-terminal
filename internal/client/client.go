// Package client is the thin RPC client used by `ht` subcommands to
// talk to the daemon. It opens a fresh connection per call, which
// matches the daemon's one-op-per-connection model.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"headless-terminal/internal/daemon"
	"headless-terminal/internal/protocol"
)

// Client performs one RPC round-trip per method call.
type Client struct {
	socketPath string
}

func New(socketPath string) *Client {
	if socketPath == "" {
		socketPath = daemon.DefaultSocketPath()
	}
	return &Client{socketPath: socketPath}
}

// dial opens a fresh connection to the daemon.
func (c *Client) dial() (net.Conn, error) {
	return net.Dial("unix", c.socketPath)
}

// EnsureDaemon makes sure a daemon is listening at the socket. If not,
// it forks `ht daemon` as a detached background process and waits up to
// 3 seconds for it to become reachable.
func (c *Client) EnsureDaemon() error {
	if conn, err := c.dial(); err == nil {
		_ = conn.Close()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Stdin = nil
	// Discard daemon stdio so it doesn't intermingle with client output.
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), "HT_SOCKET="+c.socketPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Detach: we don't want to wait on the daemon process.
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := c.dial(); err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("daemon started but socket did not become reachable within 3s")
}

// call performs a single request/response RPC.
func (c *Client) call(op string, params any, result any) error {
	conn, err := c.dial()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	req := protocol.Request{Op: op}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = b
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}
	reqBytes = append(reqBytes, '\n')
	if _, err := conn.Write(reqBytes); err != nil {
		return err
	}

	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if result != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	return nil
}

// Public op wrappers.

func (c *Client) Run(p protocol.RunParams) (*protocol.RunResult, error) {
	var r protocol.RunResult
	if err := c.call(protocol.OpRun, p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) List() (*protocol.ListResult, error) {
	var r protocol.ListResult
	if err := c.call(protocol.OpList, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) Stop(id string, timeout time.Duration) error {
	return c.call(protocol.OpStop, protocol.StopParams{
		ID:        id,
		TimeoutMs: int(timeout / time.Millisecond),
	}, nil)
}

func (c *Client) Kill(id string) error {
	return c.call(protocol.OpKill, protocol.SessionRef{ID: id}, nil)
}

func (c *Client) Remove(id string, force bool) error {
	return c.call(protocol.OpRemove, protocol.RemoveParams{ID: id, Force: force}, nil)
}

func (c *Client) Send(id string, data []byte) error {
	return c.call(protocol.OpSend, protocol.SendParams{ID: id, Data: data}, nil)
}

// SendExt sends bytes with optional compound wait and view. Returns the
// SendResult populated with whichever compound actions were requested.
func (c *Client) SendExt(p protocol.SendParams) (*protocol.SendResult, error) {
	var r protocol.SendResult
	if err := c.call(protocol.OpSend, p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) View(id, format string) (*protocol.ViewResult, error) {
	var r protocol.ViewResult
	if err := c.call(protocol.OpView, protocol.ViewParams{ID: id, Format: format}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WatchConfig configures a streaming watch session.
type WatchConfig struct {
	ID      string
	OnReady func(*protocol.SessionInfo) // called once after the ack, before any data frame
	OnFrame func(protocol.WatchFrame) error
}

// Watch streams session output. If the session does not yet exist, the
// daemon holds the request until a session with the given name appears.
// OnReady fires after the ack (so callers can clear any "waiting" UI);
// OnFrame fires for each data/EOF frame.
func (c *Client) Watch(cfg WatchConfig) error {
	conn, err := c.dial()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	req := protocol.Request{Op: protocol.OpWatch}
	if b, err := json.Marshal(protocol.WatchParams{ID: cfg.ID}); err == nil {
		req.Params = b
	}
	reqBytes, _ := json.Marshal(req)
	reqBytes = append(reqBytes, '\n')
	if _, err := conn.Write(reqBytes); err != nil {
		return err
	}

	r := bufio.NewReader(conn)

	// First line is the ack Response. With Follow, this can block
	// indefinitely while the daemon waits for a matching session.
	ack, err := r.ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp protocol.Response
	if err := json.Unmarshal(ack, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	var info protocol.SessionInfo
	if len(resp.Data) > 0 {
		_ = json.Unmarshal(resp.Data, &info)
	}
	if cfg.OnReady != nil {
		cfg.OnReady(&info)
	}

	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil
		}
		var frame protocol.WatchFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return err
		}
		if err := cfg.OnFrame(frame); err != nil {
			return err
		}
		if frame.EOF {
			return nil
		}
	}
}

// Wait blocks in the daemon until the given conditions are met or the
// timeout fires.
func (c *Client) Wait(p protocol.WaitParams) (*protocol.WaitResult, error) {
	var r protocol.WaitResult
	if err := c.call(protocol.OpWait, p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// DaemonStop requests a graceful daemon shutdown.
func (c *Client) DaemonStop() error {
	return c.call(protocol.OpDaemonStop, nil, nil)
}
