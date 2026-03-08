package postgresdbtool

import (
	"reflect"
	"testing"
)

type testUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Active   bool   `json:"active"`
}

func TestScanOne(t *testing.T) {
	t.Run("direct type match", func(t *testing.T) {
		row := Row{"id": int64(1), "username": "alice", "email": "alice@example.com", "active": true}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := testUser{ID: 1, Username: "alice", Email: "alice@example.com", Active: true}
		if !reflect.DeepEqual(user, expected) {
			t.Errorf("expected %+v, got %+v", expected, user)
		}
	})

	t.Run("pgx int32 coerced to int64 field", func(t *testing.T) {
		// pgx returns int32 for postgres INTEGER columns — struct field is int64
		row := Row{"id": int32(42), "username": "bob"}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 42 {
			t.Errorf("expected ID=42, got %d", user.ID)
		}
	})

	t.Run("nil row returns zero value, no error", func(t *testing.T) {
		user, err := ScanOne[testUser](nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(user, testUser{}) {
			t.Errorf("expected zero value, got %+v", user)
		}
	})

	t.Run("nil column value skipped", func(t *testing.T) {
		row := Row{"id": int64(1), "username": nil}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 1 || user.Username != "" {
			t.Errorf("unexpected: %+v", user)
		}
	})

	t.Run("partial row — missing columns stay zero", func(t *testing.T) {
		row := Row{"username": "partial"}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Username != "partial" || user.ID != 0 {
			t.Errorf("unexpected: %+v", user)
		}
	})

	t.Run("unknown column ignored", func(t *testing.T) {
		row := Row{"id": int64(1), "ghost_column": "ignored"}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 1 {
			t.Errorf("expected ID=1, got %d", user.ID)
		}
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		row := Row{"id": "not-a-number"}
		_, err := ScanOne[testUser](row)
		if err == nil {
			t.Error("expected error for string→int64, got nil")
		}
	})
}

func TestScan(t *testing.T) {
	t.Run("slice mapping", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1), "username": "alice"},
			{"id": int64(2), "username": "bob"},
		}
		users, err := Scan[testUser](rows)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 2 || users[0].Username != "alice" || users[1].Username != "bob" {
			t.Errorf("unexpected result: %+v", users)
		}
	})

	t.Run("empty slice returns empty slice", func(t *testing.T) {
		users, err := Scan[testUser]([]Row{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 0 {
			t.Errorf("expected empty, got %d items", len(users))
		}
	})

	t.Run("error propagates from invalid row", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1)},
			{"id": "invalid"},
		}
		_, err := Scan[testUser](rows)
		if err == nil {
			t.Error("expected error from invalid row, got nil")
		}
	})
}
