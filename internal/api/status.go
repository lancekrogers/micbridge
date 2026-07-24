package api

import "github.com/lancekrogers/micbridge/internal/core"

// Status is the daemon health payload for /v1/status and CLI status.
type Status struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Healthy bool   `json:"healthy"`
}

// CurrentStatus returns a minimal bootstrap status snapshot.
func CurrentStatus() Status {
	return Status{
		Name:    core.Name,
		Version: core.Version,
		Healthy: true,
	}
}
