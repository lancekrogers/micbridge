package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	Health   func() (bool, []string)

	mu         sync.Mutex
	mutationMu sync.Mutex
	ln         net.Listener
	httpSrv    *http.Server
	socket     string
	socketInfo os.FileInfo
	idem       map[string]idemEntry
	idemOrder  []string
}

type idemEntry struct {
	signature [32]byte
	status    int
	body      []byte
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
		conn, dialErr := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return fmt.Errorf("api: live daemon already owns socket: %s", socketPath)
		}
		if removeErr := os.Remove(socketPath); removeErr != nil {
			return fmt.Errorf("api: remove stale socket: %w", removeErr)
		}
	} else if err == nil {
		return fmt.Errorf("api: path exists and is not a socket: %s", socketPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if chmodErr := os.Chmod(socketPath, 0o600); chmodErr != nil {
		_ = ln.Close()
		return chmodErr
	}
	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		_ = ln.Close()
		return err
	}
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("api: server already started")
	}
	s.ln = ln
	s.socket = socketPath
	s.socketInfo = socketInfo
	s.idem = make(map[string]idemEntry)
	s.idemOrder = nil
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/devices", s.handleDevices)
	mux.HandleFunc("/v1/bindings", s.handleBindings)
	mux.HandleFunc("/v1/bindings/", s.handleBindingID)
	mux.HandleFunc("/v1/recordings", s.handleRecordings)
	mux.HandleFunc("/v1/recordings/", s.handleRecordingActions)
	mux.HandleFunc("/v1/events", s.handleEvents)
	httpSrv := &http.Server{Handler: mux}
	s.httpSrv = httpSrv
	s.mu.Unlock()

	go func() { _ = httpSrv.Serve(ln) }()
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
	ln := s.ln
	sock := s.socket
	ownedInfo := s.socketInfo
	s.ln = nil
	s.httpSrv = nil
	s.socket = ""
	s.socketInfo = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	var shutdownErr error
	if srv != nil {
		shutdownErr = srv.Shutdown(ctx)
	}
	if sock != "" {
		if current, err := os.Stat(sock); err == nil && ownedInfo != nil && os.SameFile(current, ownedInfo) {
			_ = os.Remove(sock)
		}
	}
	return shutdownErr
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
	healthy := s.Bus != nil && s.Bindings != nil && s.Machine != nil && s.Devices != nil
	var reasons []string
	if s.Health != nil {
		healthy, reasons = s.Health()
	}
	payload := map[string]any{
		"schema":  "micbridge.status/v1",
		"name":    st.Name,
		"version": st.Version,
		"healthy": healthy,
	}
	if len(reasons) > 0 {
		payload["health_reasons"] = reasons
	}
	if s.Layout != nil {
		payload["state_dir"] = s.Layout.Root
		payload["recordings_dir"] = s.Layout.RecordingsDir()
		payload["journals_dir"] = s.Layout.JournalsDir()
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
	var b binding.Binding
	_, err := decodeStrict(r, &b)
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	if b.Schema == "" {
		b.Schema = binding.Schema
	}
	body := canonicalJSON(b)
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if hit, proceed := s.checkIdem(w, r, body); hit || !proceed {
		return
	}
	out, err := s.Bindings.Upsert(b, s.capabilitiesFor(b.DeviceID))
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	s.storeIdem(r, body, http.StatusOK, out)
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
	var body struct {
		Action string `json:"action"` // arm|start
		Title  string `json:"title"`
		Output string `json:"output_path"`
	}
	_, err := decodeStrict(r, &body)
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	raw := canonicalJSON(body)
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if hit, proceed := s.checkIdem(w, r, raw); hit || !proceed {
		return
	}
	var outputPath string
	if body.Output != "" {
		outputPath, err = s.containOutputPath(body.Output)
		if err != nil {
			s.writeErr(w, r, http.StatusBadRequest, "path_error", err.Error())
			return
		}
	}
	cmd := recording.Command{
		Name: recording.CmdArm, Actor: recording.ActorAPI,
		IdempotencyKey: idemCommandKey(r, raw), OutputPath: outputPath,
	}
	switch body.Action {
	case "arm":
	case "start":
		cmd.Name = recording.CmdStart
	default:
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", "action must be arm or start")
		return
	}
	snap, err := s.Machine.Submit(r.Context(), cmd)
	if err != nil {
		s.writeErr(w, r, http.StatusConflict, "transition_error", err.Error())
		return
	}
	s.storeIdem(r, raw, http.StatusOK, snap)
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
	sessionID := strings.TrimSuffix(path, "/actions")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", "invalid recording id")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	_, err := decodeStrict(r, &body)
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "schema_error", err.Error())
		return
	}
	raw := canonicalJSON(body)
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if hit, proceed := s.checkIdem(w, r, raw); hit || !proceed {
		return
	}
	current := s.Machine.Snapshot()
	if sessionID == "current" {
		sessionID = current.SessionID
	}
	if sessionID == "" || sessionID != current.SessionID {
		s.writeErr(w, r, http.StatusConflict, "session_mismatch", "recording id is not the current session")
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
		Name: name, Actor: recording.ActorAPI, SessionID: sessionID, IdempotencyKey: idemCommandKey(r, raw),
	})
	if err != nil {
		s.writeErr(w, r, http.StatusConflict, "transition_error", err.Error())
		return
	}
	s.storeIdem(r, raw, http.StatusOK, snap)
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
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			s.writeErr(w, r, http.StatusBadRequest, "schema_error", "invalid cursor")
			return
		}
		n = parsed
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
			b, err := json.Marshal(env)
			if err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "id: %d\n", env.Sequence); err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "event: %s\n", env.Type); err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) containOutputPath(p string) (string, error) {
	if s.Layout == nil {
		return "", fmt.Errorf("no state root configured")
	}
	root, err := filepath.Abs(s.Layout.RecordingsDir())
	if err != nil {
		return "", fmt.Errorf("resolve recordings root: %w", err)
	}
	if mkdirErr := os.MkdirAll(root, 0o700); mkdirErr != nil {
		return "", fmt.Errorf("prepare recordings root: %w", mkdirErr)
	}
	clean := filepath.Clean(p)
	full := clean
	if !filepath.IsAbs(full) {
		full = filepath.Join(root, full)
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	if !pathWithin(root, full) {
		return "", fmt.Errorf("output path escapes recordings root")
	}
	if !strings.EqualFold(filepath.Ext(full), ".wav") {
		return "", fmt.Errorf("output path must end in .wav")
	}
	base := strings.TrimSuffix(filepath.Base(full), filepath.Ext(full))
	if strings.TrimSpace(base) == "" || strings.ContainsAny(base, `/\`) {
		return "", fmt.Errorf("output path has an invalid filename")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve recordings root symlinks: %w", err)
	}
	existing := filepath.Dir(full)
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect output parent: %w", statErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("output parent has no existing ancestor")
		}
		existing = parent
	}
	resolvedParent, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve output parent symlinks: %w", err)
	}
	if !pathWithin(resolvedRoot, resolvedParent) {
		return "", fmt.Errorf("output parent escapes recordings root through a symlink")
	}
	return full, nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func decodeStrict(r *http.Request, dst any) ([]byte, error) {
	const maxBody = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("request body is required")
	}
	if len(raw) > maxBody {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxBody)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("request body must contain one JSON value")
		}
		return nil, err
	}
	return raw, nil
}

func requestSignature(r *http.Request, body []byte) [32]byte {
	payload := append([]byte(r.Method+"\x00"+r.URL.Path+"\x00"), body...)
	return sha256.Sum256(payload)
}

func canonicalJSON(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("api: canonical JSON: %v", err))
	}
	return body
}

func idemCommandKey(r *http.Request, body []byte) string {
	if r.Header.Get("Idempotency-Key") == "" {
		return ""
	}
	sig := requestSignature(r, body)
	return fmt.Sprintf("api:%x", sig[:])
}

func (s *Server) checkIdem(w http.ResponseWriter, r *http.Request, body []byte) (hit bool, proceed bool) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return false, true
	}
	signature := requestSignature(r, body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.idem[key]; ok {
		if e.signature != signature {
			s.writeErr(w, r, http.StatusConflict, "idempotency_conflict", "idempotency key was already used for a different request")
			return false, false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		_, _ = w.Write(e.body)
		return true, false
	}
	return false, true
}

func (s *Server) storeIdem(r *http.Request, body []byte, status int, v any) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return
	}
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	s.mu.Lock()
	if _, exists := s.idem[key]; !exists {
		s.idemOrder = append(s.idemOrder, key)
	}
	s.idem[key] = idemEntry{signature: requestSignature(r, body), status: status, body: append([]byte(nil), b...)}
	const maxIdempotencyEntries = 1024
	if len(s.idemOrder) > maxIdempotencyEntries {
		oldest := s.idemOrder[0]
		s.idemOrder = s.idemOrder[1:]
		delete(s.idem, oldest)
	}
	s.mu.Unlock()
}

func (s *Server) capabilitiesFor(deviceID core.DeviceID) []core.Capability {
	if s.Devices == nil {
		return nil
	}
	for _, device := range s.Devices() {
		if device.Identity.DeviceID == deviceID {
			return device.Capabilities
		}
	}
	return []core.Capability{}
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
