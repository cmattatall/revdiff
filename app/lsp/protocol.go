package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	result json.RawMessage
	err    error
}

type connection struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	cancel    context.CancelFunc
	writeMu   sync.Mutex
	mu        sync.Mutex
	next      int64
	pending   map[int64]chan response
	done      chan struct{}
	err       error
	closeOnce sync.Once
}

func newConnection(command string, cmd *exec.Cmd, in io.WriteCloser, out io.Reader, cancel context.CancelFunc) *connection {
	c := &connection{cmd: cmd, in: in, cancel: cancel, pending: make(map[int64]chan response), done: make(chan struct{})}
	go c.readLoop(out)
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errors.New(command + " exited")
		}
		c.fail(fmt.Errorf("lsp: %s exited: %w", command, err))
	}()
	return c
}

func (c *connection) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	select {
	case <-c.done:
		err := c.err
		c.mu.Unlock()
		return nil, err
	default:
	}
	c.next++
	id := c.next
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { _ = c.close() })
	defer stop()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.remove(id)
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.remove(id)
		return nil, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
}

func (c *connection) notify(ctx context.Context, method string, params any) error {
	stop := context.AfterFunc(ctx, func() { _ = c.close() })
	defer stop()
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *connection) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		c.mu.Lock()
		err = c.err
		c.mu.Unlock()
		return err
	default:
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n", len(b))
	if err == nil {
		_, err = c.in.Write(b)
	}
	if err != nil {
		return fmt.Errorf("lsp: write message: %w", err)
	}
	return nil
}

func (c *connection) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		length := -1
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				c.fail(fmt.Errorf("lsp: read header: %w", err))
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			name, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				length, _ = strconv.Atoi(strings.TrimSpace(value))
			}
		}
		if length < 0 || length > 64<<20 {
			c.fail(errors.New("lsp: invalid Content-Length"))
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(br, body); err != nil {
			c.fail(fmt.Errorf("lsp: read body: %w", err))
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			c.fail(fmt.Errorf("lsp: invalid JSON: %w", err))
			return
		}
		if msg.Method != "" && len(msg.ID) != 0 {
			c.rejectRequest(msg)
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			if msg.Error != nil {
				ch <- response{err: fmt.Errorf("lsp: server error %d: %s", msg.Error.Code, msg.Error.Message)}
			} else {
				ch <- response{result: msg.Result}
			}
		}
	}
}

func (c *connection) rejectRequest(msg rpcMessage) {
	var reply map[string]any
	var id any
	_ = json.Unmarshal(msg.ID, &id)
	if msg.Method == "workspace/applyEdit" {
		reply = map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
			"applied": false, "failureReason": "revdiff is read-only",
		}}
	} else {
		reply = map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{
			"code": -32601, "message": "method not supported by read-only client",
		}}
	}
	_ = c.send(reply)
}

func (c *connection) remove(id int64) { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }

func (c *connection) fail(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = errors.New("lsp: connection closed")
		}
		c.mu.Lock()
		c.err = err
		pending := c.pending
		c.pending = make(map[int64]chan response)
		close(c.done)
		c.mu.Unlock()
		for _, ch := range pending {
			ch <- response{err: err}
		}
	})
}

func (c *connection) close() error {
	c.cancel()
	_ = c.in.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.fail(errors.New("lsp: connection closed"))
	return nil
}
