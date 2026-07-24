package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
)

// Server is the local control API (Unix socket HTTP).
type Server struct {
	Layout   *storage.Layout
	Bus      *event.Bus
	Bindings *binding.Engine
	Machine  *recording.Machine
	Devices  func() []core.Device

	mu      sync.Mutex
	ln      net.Listener
	httpSrv *http.Server
	socket  string
	idem    map[string]idemEntry
}

type idemEntry struct {
	status int
	body   []byte
}

// DefaultSocketPath returns $XDG_RUNTIME_DIR/micbridge/micbridge.sock or a fallback under state.
func DefaultSocketPath(layout *storage.Layout) string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "micbridge", "micbridge.sock")
	}
	if layout != nil {
		return filepath.Join(layout.Root, "micbridge.sock")
	}
	return filepath.Join(os.TempDir(), "micbridge.sock")
}

// Start listens on a Unix socket with 0600 permissions (dir 0700).
func (s *Server) Start(socketPath string) error {
	if socketPath == "" {
		socketPath = DefaultSocketPath(s.Layout)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	// Stale socket handling.
	if fi, err := os.Stat(socketPath); err == nil && fi.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(socketPath)
	} else if err == nil {
		return fmt.Errorf("api: path exists and is not a socket: %s", socketPath)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.socket = socketPath
	s.idem = make(map[string]idemEntry)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/devices", s.handleDevices)
	mux.HandleFunc("/v1/bindings", s.handleBindings)
	mux.HandleFunc("/v1/bindings/", s.handleBindingID)
	mux.HandleFunc("/v1/recordings", s.handleRecordings)
	mux.HandleFunc("/v1/recordings/", s.handleRecordingActions)
	mux.HandleFunc("/v1/events", s.handleEvents)
	s.httpSrv = &http.Server{Handler: mux}
	s.mu.Unlock()

	go func() { _ = s.httpSrv.Serve(ln) }()
	return nil
}

// Socket returns the listening path.
func (s *Server) Socket() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.socket
}

// Close shuts down the server and removes the socket.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	sock := s.socket
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	if sock != "" {
		_ = os.Remove(sock)
	}
	return nil
}

type errorBody struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b = append(b, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func (s *Server) writeErr(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	s.writeJSON(w, status, errorBody{Error: msg, Code: code, RequestID: r.Header.Get("X-Request-Id")})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	st := CurrentStatus()
	payload := map[string]any{
		"schema":  "micbridge.status/v1",
		"name":    st.Name,
		"version": st.Version,
		"healthy": st.Healthy,
	}
	if s.Layout != nil {
		payload["state_dir"] = s.Layout.Root
	}
	if s.Machine != nil {
		snap := s.Machine.Snapshot()
		payload["recording"] = snap
	}
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	var devices []core.Device
	if s.Devices != nil {
		devices = s.Devices()
	}
	if devices == nil {
		devices = []core.Device{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"schema":  "micbridge.devices/v1",
		"devices": devices,
	})
}

func (s *Server) handleBindings(w http.ResponseWriter, r *http.Request) {
	if s.Bindings == nil {
		s.writeErr(w, r, http.StatusServiceUnavailable, "unavailable", "bindings not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, http.StatusOK, map[string]any{
			"schema":   binding.Schema,
			"bindings": s.Bindings.List(),
		})
	case http.MethodPut:
		s.handlePutBinding(w, r)
	default:
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PUT")
	}
}

func (s *Server) handlePutBinding(w http.ResponseWriter, r *http.Request) {
	if hit, ok := s.checkIdem(w, r); ok && hit {
		return
	}
	var b binding.Binding
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	if b.Schema == "" {
		b.Schema = binding.Schema
	}
	out, err := s.Bindings.Upsert(b, nil)
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	s.storeIdem(r, http.StatusOK, out)
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBindingID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/bindings/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", "invalid binding id")
		return
	}
	if s.Bindings == nil {
		s.writeErr(w, r, http.StatusServiceUnavailable, "unavailable", "bindings not configured")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.Bindings.Delete(binding.ID(id)); err != nil {
			s.writeErr(w, r, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		b, ok := s.Bindings.Get(binding.ID(id))
		if !ok {
			s.writeErr(w, r, http.StatusNotFound, "not_found", "binding not found")
			return
		}
		s.writeJSON(w, http.StatusOK, b)
	default:
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET or DELETE")
	}
}

func (s *Server) handleRecordings(w http.ResponseWriter, r *http.Request) {
	if s.Machine == nil {
		s.writeErr(w, r, http.StatusServiceUnavailable, "unavailable", "recording not configured")
		return
	}
	if r.Method != http.MethodPost {
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	if hit, ok := s.checkIdem(w, r); ok && hit {
		return
	}
	var body struct {
		Action string `json:"action"` // arm|start
		Title  string `json:"title"`
		Output string `json:"output_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, context.Canceled) {
		// empty body ok
	}
	if body.Output != "" {
		if err := s.containPath(body.Output); err != nil {
			s.writeErr(w, r, http.StatusBadRequest, "path_error", err.Error())
			return
		}
	}
	cmd := recording.Command{Name: recording.CmdArm, Actor: recording.ActorAPI, IdempotencyKey: r.Header.Get("Idempotency-Key")}
	if body.Action == "start" {
		cmd.Name = recording.CmdStart
	}
	snap, err := s.Machine.Submit(r.Context(), cmd)
	if err != nil {
		s.writeErr(w, r, http.StatusConflict, "transition_error", err.Error())
		return
	}
	s.storeIdem(r, http.StatusOK, snap)
	s.writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleRecordingActions(w http.ResponseWriter, r *http.Request) {
	if s.Machine == nil {
		s.writeErr(w, r, http.StatusServiceUnavailable, "unavailable", "recording not configured")
		return
	}
	// /v1/recordings/{id}/actions
	path := strings.TrimPrefix(r.URL.Path, "/v1/recordings/")
	if !strings.HasSuffix(path, "/actions") || r.Method != http.MethodPost {
		s.writeErr(w, r, http.StatusNotFound, "not_found", "use POST /v1/recordings/{id}/actions")
		return
	}
	if hit, ok := s.checkIdem(w, r); ok && hit {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	var name recording.CommandName
	switch body.Action {
	case "start":
		name = recording.CmdStart
	case "stop":
		name = recording.CmdStop
	case "arm":
		name = recording.CmdArm
	default:
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", "unknown action")
		return
	}
	snap, err := s.Machine.Submit(r.Context(), recording.Command{
		Name: name, Actor: recording.ActorAPI, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		s.writeErr(w, r, http.StatusConflict, "transition_error", err.Error())
		return
	}
	s.storeIdem(r, http.StatusOK, snap)
	s.writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.Bus == nil {
		s.writeErr(w, r, http.StatusServiceUnavailable, "unavailable", "events not configured")
		return
	}
	if r.Method != http.MethodGet {
		s.writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	var from event.Sequence
	if v := r.URL.Query().Get("cursor"); v != "" {
		var n uint64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			s.writeErr(w, r, http.StatusBadRequest, "schema_error", "invalid cursor")
			return
		}
		from = event.Sequence(n)
	}
	ch, unsub, err := s.Bus.Subscribe(16, from)
	if err != nil {
		s.writeErr(w, r, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	defer unsub()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeErr(w, r, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(env)
			fmt.Fprintf(w, "id: %d\n", env.Sequence)
			fmt.Fprintf(w, "event: %s\n", env.Type)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (s *Server) containPath(p string) error {
	if s.Layout == nil {
		return fmt.Errorf("no state root configured")
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(s.Layout.Root, clean)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("output path escapes state root")
		}
		return nil
	}
	// relative paths join under recordings dir
	full := filepath.Join(s.Layout.RecordingsDir(), clean)
	rel, err := filepath.Rel(s.Layout.Root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("output path escapes state root")
	}
	return nil
}

func (s *Server) checkIdem(w http.ResponseWriter, r *http.Request) (hit bool, hasKey bool) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.idem[key]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		_, _ = w.Write(e.body)
		return true, true
	}
	return false, true
}

func (s *Server) storeIdem(r *http.Request, status int, v any) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return
	}
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	s.mu.Lock()
	s.idem[key] = idemEntry{status: status, body: append([]byte(nil), b...)}
	s.mu.Unlock()
}

// DialHTTP returns an http.Client dialing the unix socket.
func DialHTTP(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}
}
