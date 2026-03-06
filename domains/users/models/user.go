package models

// User represents a user in the database.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"` // Never serialize password
	CreatedAt    string `json:"created_at"`
}
