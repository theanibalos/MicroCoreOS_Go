package authtool

import (
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHashing(t *testing.T) {
	auth := NewJWTAuthTool()
	password := "super-secret"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected error hashing: %v", err)
	}

	if hash == password {
		t.Error("hash should not be equal to plain text password")
	}

	if !auth.VerifyPassword(password, hash) {
		t.Error("verification failed for correct password")
	}

	if auth.VerifyPassword("wrong-password", hash) {
		t.Error("verification succeeded for incorrect password")
	}
}

func TestJWTLifecycle(t *testing.T) {
	// Setup environment for testing
	os.Setenv("AUTH_SECRET_KEY", "test-secret")
	defer os.Unsetenv("AUTH_SECRET_KEY")

	auth := NewJWTAuthTool()
	if err := auth.Setup(); err != nil {
		t.Fatalf("failed to setup auth tool: %v", err)
	}

	claims := map[string]any{"user_id": 123, "role": "admin"}
	token, err := auth.CreateToken(claims)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	t.Run("Valid token", func(t *testing.T) {
		decoded, err := auth.DecodeToken(token)
		if err != nil {
			t.Fatalf("failed to decode valid token: %v", err)
		}

		if decoded["user_id"].(float64) != 123 {
			t.Errorf("expected user_id 123, got %v", decoded["user_id"])
		}
	})

	t.Run("Invalid signature", func(t *testing.T) {
		badToken := token + "tampered"
		_, err := auth.DecodeToken(badToken)
		if err == nil {
			t.Error("expected error for tampered token, got nil")
		}

		if auth.ValidateToken(badToken) != nil {
			t.Error("ValidateToken should return nil for tampered token")
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		// Mock a tool with expired configuration
		expiredAuth := NewJWTAuthTool()
		expiredAuth.secretKey = []byte("test-secret")
		expiredAuth.algorithm = jwt.SigningMethodHS256
		expiredAuth.expMinutes = -1 // expired in the past

		expiredToken, _ := expiredAuth.CreateToken(claims)

		_, err := auth.DecodeToken(expiredToken) // Using original auth to verify
		if err == nil {
			t.Error("expected error for expired token, got nil")
		}
	})
}

func TestAlgorithmMismatch(t *testing.T) {
	os.Setenv("AUTH_SECRET_KEY", "test-secret")
	os.Setenv("AUTH_ALGORITHM", "HS256")
	defer os.Unsetenv("AUTH_SECRET_KEY")
	defer os.Unsetenv("AUTH_ALGORITHM")

	auth := NewJWTAuthTool()
	auth.Setup()

	// Create a token with HS512 but the tool expects HS256
	otherClaims := jwt.MapClaims{"sub": "123"}
	otherTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS512, otherClaims)
	otherToken, _ := otherTokenObj.SignedString([]byte("test-secret"))

	_, err := auth.DecodeToken(otherToken)
	if err == nil {
		t.Error("expected error for algorithm mismatch, got nil")
	}
}
