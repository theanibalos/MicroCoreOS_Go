package dbtool

import (
	"reflect"
	"testing"
)

type TestUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Active   bool   `json:"active"`
}

func TestScanOne(t *testing.T) {
	t.Run("Valid row mapping", func(t *testing.T) {
		row := Row{
			"id":       int64(1),
			"username": "alice",
			"email":    "alice@example.com",
			"active":   true,
		}

		user, err := ScanOne[TestUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := TestUser{ID: 1, Username: "alice", Email: "alice@example.com", Active: true}
		if !reflect.DeepEqual(user, expected) {
			t.Errorf("expected %+v, got %+v", expected, user)
		}
	})

	t.Run("Nil row returns zero value", func(t *testing.T) {
		user, err := ScanOne[TestUser](nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := TestUser{}
		if !reflect.DeepEqual(user, expected) {
			t.Errorf("expected zero value %+v, got %+v", expected, user)
		}
	})

	t.Run("Type mismatch error", func(t *testing.T) {
		// id should be int64, but we provide a string
		row := Row{
			"id": "not-a-number",
		}

		_, err := ScanOne[TestUser](row)
		if err == nil {
			t.Error("expected error due to type mismatch, got nil")
		}
	})

	t.Run("Partial row mapping", func(t *testing.T) {
		row := Row{
			"username": "bob",
		}

		user, err := ScanOne[TestUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if user.Username != "bob" || user.ID != 0 {
			t.Errorf("unexpected mapping for partial row: %+v", user)
		}
	})
}

func TestScan(t *testing.T) {
	t.Run("Valid slice mapping", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1), "username": "alice"},
			{"id": int64(2), "username": "bob"},
		}

		users, err := Scan[TestUser](rows)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(users))
		}

		if users[0].Username != "alice" || users[1].Username != "bob" {
			t.Error("incorrect mapping in slice")
		}
	})

	t.Run("Empty slice returns empty slice", func(t *testing.T) {
		users, err := Scan[TestUser]([]Row{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 0 {
			t.Errorf("expected empty slice, got %d items", len(users))
		}
	})

	t.Run("Error propagation from ScanOne", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1)},
			{"id": "invalid"},
		}

		_, err := Scan[TestUser](rows)
		if err == nil {
			t.Error("expected error propagated from invalid row, got nil")
		}
	})
}
