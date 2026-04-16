// Package protocol defines the wire format spoken between `ht` clients
// and the daemon over a Unix socket. The format is newline-delimited JSON:
// one Request per line from the client, one Response per line from the
// server. For streaming operations (watch), the server writes multiple
// WatchFrame JSON lines after the initial ack and the client reads until
// the connection closes or an EOF frame is seen.
package protocol

import (
	"encoding/json"
	"time"
)

// Op names. Wire strings are stable; changing them is a breaking change.
const (
	OpRun        = "run"
	OpList       = "list"
	OpStop       = "stop"
	OpKill       = "kill"
	OpRemove     = "remove"
	OpSend       = "send"
	OpView       = "view"
	OpWatch      = "watch"
	OpWait       = "wait"
	OpDaemonStop = "daemon-stop"
)

// Request is a single RPC request.
type Request struct {
	Op     string          `json:"op"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is a single RPC response. For streaming ops, the first
// response is a non-streaming ack; subsequent lines are stream frames
// specific to the op.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// SessionInfo is the canonical session summary used in list and run results.
type SessionInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	PID       int       `json:"pid"`
	State     string    `json:"state"` // "starting" | "running" | "exited"
	Cols      uint16    `json:"cols"`
	Rows      uint16    `json:"rows"`
	StartedAt time.Time `json:"started_at"`
	Cmd       []string  `json:"cmd"`
	ExitCode  *int      `json:"exit_code,omitempty"`
}

// Params and results per op.

type RunParams struct {
	Cmd  []string `json:"cmd"`
	Cols uint16   `json:"cols,omitempty"`
	Rows uint16   `json:"rows,omitempty"`
	Cwd  string   `json:"cwd,omitempty"`
	Env  []string `json:"env,omitempty"`
	Name string   `json:"name,omitempty"`
}

type RunResult struct {
	Session SessionInfo `json:"session"`
}

type ListResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionRef is used by ops that take just an ID (stop/kill/remove/watch).
type SessionRef struct {
	ID string `json:"id"`
}

type SendParams struct {
	ID   string `json:"id"`
	Data []byte `json:"data"` // base64-encoded in JSON
	// InterKeyDelayMs paces the send by inserting a delay of this many
	// milliseconds between logical keystrokes (VT escape sequences and
	// multi-byte UTF-8 runes remain atomic). Zero means no pacing.
	InterKeyDelayMs int `json:"inter_key_delay_ms,omitempty"`
	// SettleMs is a plain post-send sleep applied before any Wait or
	// View runs. Useful when a keystroke triggers slow work (character
	// creation, emacs startup, etc.) and the default pacing gap isn't
	// enough to capture the full response. Zero means no settle.
	SettleMs int `json:"settle_ms,omitempty"`
	// Optional compound actions. If Wait is set, the daemon blocks on
	// the condition after delivering Data. If View is set, a snapshot
	// is captured after the send (and after Wait, if also set).
	Wait *WaitParams   `json:"wait,omitempty"`
	View *SendViewOpts `json:"view,omitempty"`
}

// SendViewOpts requests a snapshot be returned alongside the send result.
type SendViewOpts struct {
	Format string `json:"format,omitempty"` // "plain" | "ansi" | "html"
}

// SendResult bundles any compound-action outputs. Fields are only set
// when their corresponding option was requested.
type SendResult struct {
	Wait *WaitResult `json:"wait,omitempty"`
	View *ViewResult `json:"view,omitempty"`
}

// StopParams supports an optional grace timeout before SIGKILL escalation.
type StopParams struct {
	ID         string `json:"id"`
	TimeoutMs  int    `json:"timeout_ms,omitempty"`
}

// RemoveParams allows removing a running session when Force is set.
type RemoveParams struct {
	ID    string `json:"id"`
	Force bool   `json:"force,omitempty"`
}

type ViewParams struct {
	ID     string `json:"id"`
	Format string `json:"format,omitempty"` // "plain" (default) | "ansi" | "html"
	Trim   *bool  `json:"trim,omitempty"`
}

type ViewResult struct {
	ID     string  `json:"id"`
	Cols   uint16  `json:"cols"`
	Rows   uint16  `json:"rows"`
	Screen string  `json:"screen"`
	Cursor *Cursor `json:"cursor,omitempty"`
}

// Cursor reports the cursor state at the time of the View call. Row/Col
// are 1-indexed. Omitted from ViewResult (via omitempty) when the cursor
// is not in the visible viewport.
type Cursor struct {
	Row     uint16 `json:"row"`
	Col     uint16 `json:"col"`
	Visible bool   `json:"visible"`
}

// WatchParams selects a session to watch. If the session does not yet
// exist, the daemon parks the request in a pending queue and fulfills
// it the moment a session with the requested name (exact match) is
// created. Watch is always interactive; clients abort by disconnecting.
type WatchParams struct {
	ID string `json:"id"`
}

// WatchFrame is one streamed chunk of PTY output to a watcher.
// EOF=true marks the final frame (session ended or server closing).
type WatchFrame struct {
	Data []byte `json:"data,omitempty"`
	EOF  bool   `json:"eof,omitempty"`
}

// WaitParams are the conditions to block on. Zero-valued fields are not
// checked. Multiple fields compose as AND.
type WaitParams struct {
	ID        string  `json:"id"`
	IdleMs    int     `json:"idle_ms,omitempty"`
	Change    bool    `json:"change,omitempty"`
	Text      string  `json:"text,omitempty"`
	Regex     bool    `json:"regex,omitempty"`
	Cursor    *Cursor `json:"cursor,omitempty"` // only Row/Col used
	Exit      bool    `json:"exit,omitempty"`
	TimeoutMs int     `json:"timeout_ms,omitempty"`
}

// WaitResult reports how the wait completed.
type WaitResult struct {
	Matched   bool   `json:"matched"`
	Trigger   string `json:"trigger,omitempty"` // idle|text|cursor|exit|timeout
	ElapsedMs int64  `json:"elapsed_ms"`
}
