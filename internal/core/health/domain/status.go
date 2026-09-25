// Package domain holds health check result types.
package domain

import "time"

// Dependency is the health of one external dependency.
type Dependency struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	// Critical dependencies fail readiness when unhealthy; the others are only reported.
	Critical bool   `json:"critical"`
	Message  string `json:"message,omitempty"`
	// Err is the check failure; it is logged but never serialized to clients.
	Err error `json:"-"`
}

// Status is a liveness or readiness report.
type Status struct {
	Status       string       `json:"status"`
	Version      string       `json:"version"`
	Time         time.Time    `json:"time"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}
