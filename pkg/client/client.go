// Package client is the supported Go SDK for the Micbridge local API.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/recording"
)

// Exit codes for CLI parity.
const (
	ExitOK          = 0
	ExitUsage       = 2
	ExitUnavailable = 3
	ExitConflict    = 4
	ExitSchema      = 5
	ExitCanceled    = 130
)

// Client talks to a Micbridge daemon over a Unix socket.
type Client struct {
	Socket  string
	HTTP    *http.Client
	BaseURL string
}

// New returns a client for socketPath.
func New(socketPath string) *Client {
	return &Client{
		Socket:  socketPath,
		HTTP:    api.DialHTTP(socketPath),
		BaseURL: "http://unix",
	}
}

// APIError is a structured daemon error.
type APIError struct {
	Status int
	Code   string
	Msg    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("micbridge api: %s (%s, http %d)", e.Msg, e.Code, e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body any, headers map[string]string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("daemon unavailable: %w", err)
	}
	return resp, nil
}

func decodeErr(resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	var eb struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&eb)
	return &APIError{Status: resp.StatusCode, Code: eb.Code, Msg: eb.Error}
}

// Status returns /v1/status.
func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// Devices returns /v1/devices.
func (c *Client) Devices(ctx context.Context) ([]core.Device, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/devices", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Devices []core.Device `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Devices, nil
}

// ListBindings returns bindings.
func (c *Client) ListBindings(ctx context.Context) ([]binding.Binding, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/bindings", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Bindings []binding.Binding `json:"bindings"`
	}
	return body.Bindings, json.NewDecoder(resp.Body).Decode(&body)
}

// PutBinding creates or replaces a binding.
func (c *Client) PutBinding(ctx context.Context, b binding.Binding, idem string) (binding.Binding, error) {
	headers := map[string]string{}
	if idem != "" {
		headers["Idempotency-Key"] = idem
	}
	resp, err := c.do(ctx, http.MethodPut, "/v1/bindings", b, headers)
	if err != nil {
		return binding.Binding{}, err
	}
	if resp.StatusCode != 200 {
		return binding.Binding{}, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var out binding.Binding
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// DeleteBinding removes a binding.
func (c *Client) DeleteBinding(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/v1/bindings/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		return decodeErr(resp)
	}
	_ = resp.Body.Close()
	return nil
}

// CreateRecording arms or starts a session.
func (c *Client) CreateRecording(ctx context.Context, action, idem string) (recording.Snapshot, error) {
	headers := map[string]string{}
	if idem != "" {
		headers["Idempotency-Key"] = idem
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/recordings", map[string]string{"action": action}, headers)
	if err != nil {
		return recording.Snapshot{}, err
	}
	if resp.StatusCode != 200 {
		return recording.Snapshot{}, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var snap recording.Snapshot
	return snap, json.NewDecoder(resp.Body).Decode(&snap)
}

// RecordingAction posts start/stop/arm for a session id.
func (c *Client) RecordingAction(ctx context.Context, id, action, idem string) (recording.Snapshot, error) {
	headers := map[string]string{}
	if idem != "" {
		headers["Idempotency-Key"] = idem
	}
	path := "/v1/recordings/" + id + "/actions"
	resp, err := c.do(ctx, http.MethodPost, path, map[string]string{"action": action}, headers)
	if err != nil {
		return recording.Snapshot{}, err
	}
	if resp.StatusCode != 200 {
		return recording.Snapshot{}, decodeErr(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var snap recording.Snapshot
	return snap, json.NewDecoder(resp.Body).Decode(&snap)
}

// Events streams events from cursor (exclusive). Caller must cancel ctx.
func (c *Client) Events(ctx context.Context, cursor event.Sequence) (<-chan event.Envelope, <-chan error) {
	out := make(chan event.Envelope, 8)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		path := fmt.Sprintf("/v1/events?cursor=%d", cursor)
		// Streaming client without overall timeout.
		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", c.Socket)
				},
			},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
		if err != nil {
			errs <- err
			return
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				errs <- ctx.Err()
				return
			}
			errs <- fmt.Errorf("daemon unavailable: %w", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			errs <- decodeErr(resp)
			return
		}
		sc := bufio.NewScanner(resp.Body)
		var dataLines []string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
			if line == "" && len(dataLines) > 0 {
				var env event.Envelope
				if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &env); err == nil {
					select {
					case out <- env:
					case <-ctx.Done():
						errs <- ctx.Err()
						return
					}
				}
				dataLines = nil
			}
			if ctx.Err() != nil {
				errs <- ctx.Err()
				return
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()
	return out, errs
}

// WithTimeout returns a context with timeout (helper for CLI).
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, d)
}
