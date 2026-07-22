package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const maxMessageSize = 128 << 20

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("app-server error %d: %s", e.Code, e.Message)
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type Client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses map[int64]chan response
	mu        sync.Mutex
	writeMu   sync.Mutex
	nextID    atomic.Int64
	done      chan struct{}
	readErr   error
	stderr    lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.b.Len() > 1<<20 {
		b.b.Reset()
	}
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func Start(ctx context.Context, binary string) (*Client, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create app-server stdout: %w", err)
	}
	c := &Client{
		cmd:       cmd,
		stdin:     stdin,
		responses: make(map[int64]chan response),
		done:      make(chan struct{}),
	}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s app-server: %w", binary, err)
	}
	go c.readLoop(stdout)

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var initialized struct {
		UserAgent string `json:"userAgent"`
	}
	if err := c.Request(initCtx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "codex_history",
			"title":   "Codex History",
			"version": "0.1.0",
		},
	}, &initialized); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	defer close(c.done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxMessageSize)
	for scanner.Scan() {
		var envelope struct {
			ID *int64 `json:"id"`
		}
		line := append([]byte(nil), scanner.Bytes()...)
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.ID == nil {
			continue
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.responses[resp.ID]
		delete(c.responses, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
			close(ch)
		}
	}
	c.mu.Lock()
	c.readErr = scanner.Err()
	for id, ch := range c.responses {
		delete(c.responses, id)
		close(ch)
	}
	c.mu.Unlock()
}

func (c *Client) Request(ctx context.Context, method string, params any, target any) error {
	id := c.nextID.Add(1)
	ch := make(chan response, 1)
	c.mu.Lock()
	c.responses[id] = ch
	c.mu.Unlock()

	if err := c.write(map[string]any{"method": method, "id": id, "params": params}); err != nil {
		c.mu.Lock()
		delete(c.responses, id)
		c.mu.Unlock()
		return err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.responses, id)
		c.mu.Unlock()
		return ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return c.closedError()
		}
		if resp.Error != nil {
			return resp.Error
		}
		if target == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, target); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.write(map[string]any{"method": method, "params": params})
}

func (c *Client) write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	payload = append(payload, '\n')
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("write app-server request: %w", err)
	}
	return nil
}

func (c *Client) closedError() error {
	c.mu.Lock()
	err := c.readErr
	c.mu.Unlock()
	parts := "app-server closed"
	if err != nil {
		parts += ": " + err.Error()
	}
	if stderr := c.stderr.String(); stderr != "" {
		parts += ": " + stderr
	}
	return errors.New(parts)
}

func (c *Client) Close() error {
	if c == nil || c.cmd == nil {
		return nil
	}
	_ = c.stdin.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
	err := c.cmd.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
			return err
		}
	}
	return nil
}

func (c *Client) Stderr() string { return c.stderr.String() }
