/*
Auth Tool — Go-First implementation for MicroCoreOS
====================================================

PUBLIC CONTRACT (what plugins use):

	hash,  err := auth.HashPassword("secret")
	ok         := auth.VerifyPassword("secret", hash)
	token, err := auth.CreateToken(map[string]any{"sub": userID})
	claims, err := auth.DecodeToken(token)
	claims      := auth.ValidateToken(token) // returns nil on invalid

Configuration (env vars):
  - AUTH_SECRET_KEY         — required, JWT signing secret
  - AUTH_ALGORITHM          — optional, default "HS256"
  - AUTH_TOKEN_EXPIRE_MINS  — optional, default 60
*/
package authtool

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"microcoreos-go/core"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ─── AuthTool interface ───────────────────────────────────────────────────────

// AuthTool is the interface plugins use for authentication and JWT management.
// Resolve in Inject() using:
//
//	p.auth, err = core.GetTool[authtool.AuthTool](c, "auth")
type AuthTool interface {
	// HashPassword hashes a plain-text password using bcrypt.
	HashPassword(password string) (string, error)
	// VerifyPassword checks a plain-text password against a bcrypt hash.
	VerifyPassword(password, hash string) bool
	// CreateToken creates a signed JWT with the given claims.
	// Adds "exp" automatically based on AUTH_TOKEN_EXPIRE_MINS.
	CreateToken(claims map[string]any) (string, error)
	// DecodeToken verifies and decodes a JWT. Returns the claims or an error.
	DecodeToken(token string) (map[string]any, error)
	// ValidateToken decodes a JWT, returning nil if invalid or expired.
	// Prefer this over DecodeToken in HTTP middleware guards.
	ValidateToken(token string) map[string]any
}

// ─── JWTAuthTool ─────────────────────────────────────────────────────────────

// JWTAuthTool implements AuthTool using bcrypt + golang-jwt.
type JWTAuthTool struct {
	core.BaseToolDefaults
	secretKey  []byte
	algorithm  *jwt.SigningMethodHMAC
	expMinutes int
}

func init() {
	core.RegisterTool(func() core.Tool { return NewJWTAuthTool() })
}

// NewJWTAuthTool creates a JWTAuthTool. Configuration is read in Setup().
func NewJWTAuthTool() *JWTAuthTool {
	return &JWTAuthTool{}
}

func (a *JWTAuthTool) Name() string { return "auth" }

func (a *JWTAuthTool) Setup() error {
	fmt.Println("[AuthTool] Initializing Security Infrastructure...")

	secret := os.Getenv("AUTH_SECRET_KEY")
	if secret == "" {
		return errors.New("AUTH_SECRET_KEY is required — set it in your .env")
	}
	a.secretKey = []byte(secret)

	algo := os.Getenv("AUTH_ALGORITHM")
	switch algo {
	case "HS384":
		a.algorithm = jwt.SigningMethodHS384
	case "HS512":
		a.algorithm = jwt.SigningMethodHS512
	default:
		a.algorithm = jwt.SigningMethodHS256
	}

	expStr := os.Getenv("AUTH_TOKEN_EXPIRE_MINS")
	if expStr == "" {
		a.expMinutes = 60
	} else {
		mins, err := strconv.Atoi(expStr)
		if err != nil || mins <= 0 {
			return fmt.Errorf("AUTH_TOKEN_EXPIRE_MINS must be a positive integer, got %q", expStr)
		}
		a.expMinutes = mins
	}

	fmt.Printf("[AuthTool] Ready (algorithm=%s, expiry=%dmin).\n", a.algorithm.Alg(), a.expMinutes)
	return nil
}

func (a *JWTAuthTool) GetInterfaceDescription() string {
	return `Auth Tool (auth): Password hashing (bcrypt) and JWT management.
- HashPassword(password) → (hash, error)       — bcrypt hash.
- VerifyPassword(password, hash) → bool        — bcrypt verify.
- CreateToken(claims map) → (token, error)     — signed JWT.
- DecodeToken(token) → (claims map, error)     — verify + decode.
- ValidateToken(token) → claims map | nil      — safe decode (nil on invalid).`
}

// ─── AuthTool implementation ──────────────────────────────────────────────────

func (a *JWTAuthTool) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func (a *JWTAuthTool) VerifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *JWTAuthTool) CreateToken(claims map[string]any) (string, error) {
	jwtClaims := jwt.MapClaims{}
	for k, v := range claims {
		jwtClaims[k] = v
	}
	jwtClaims["exp"] = time.Now().Add(time.Duration(a.expMinutes) * time.Minute).Unix()
	jwtClaims["iat"] = time.Now().Unix()

	token := jwt.NewWithClaims(a.algorithm, jwtClaims)
	return token.SignedString(a.secretKey)
}

func (a *JWTAuthTool) DecodeToken(tokenStr string) (map[string]any, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != a.algorithm.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secretKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	result := make(map[string]any, len(claims))
	for k, v := range claims {
		result[k] = v
	}
	return result, nil
}

func (a *JWTAuthTool) ValidateToken(tokenStr string) map[string]any {
	claims, err := a.DecodeToken(tokenStr)
	if err != nil {
		return nil
	}
	return claims
}
