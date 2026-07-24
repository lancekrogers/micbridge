package recording

// State is a session lifecycle state.
type State string

const (
	// StateIdle means no active session.
	StateIdle State = "idle"
	// StateArmed means capture is prepared but not writing yet.
	StateArmed State = "armed"
)

// Session is a compile-time placeholder for recording lifecycle ownership.
type Session struct {
	State State
}

// NewSession returns an idle session shell.
func NewSession() *Session {
	return &Session{State: StateIdle}
}
