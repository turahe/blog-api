package domain

import "time"

type Dependency struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

type Status struct {
	Status       string       `json:"status"`
	Version      string       `json:"version"`
	Time         time.Time    `json:"time"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}
