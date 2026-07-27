// Package model defines the domain entities shared across layers.
package model

// User is the core domain entity of the service.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
