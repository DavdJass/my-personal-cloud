package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey int

const userCtxKey ctxKey = 0

// User is the authenticated user information attached to a request context.
type User struct {
	ID       int64
	Username string
}

// Service handles user creation, login and JWT issuance/verification.
type Service struct {
	db     *sql.DB
	secret []byte
	expiry time.Duration
}

// NewService constructs an auth service backed by the given SQLite handle.
func NewService(db *sql.DB, secret []byte, expiry time.Duration) *Service {
	return &Service{db: db, secret: secret, expiry: expiry}
}

// EnsureUser creates a user with the given credentials if it does not yet
// exist. It is intended for bootstrapping the first administrator account on
// fresh installations and is invoked from main with values from env vars.
func (s *Service) EnsureUser(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return errors.New("username and password are required")
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if exists > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash) VALUES (?, ?)`,
		username, string(hash),
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// Login validates credentials and returns a signed JWT on success.
func (s *Service) Login(ctx context.Context, username, password string) (string, *User, error) {
	var (
		id   int64
		hash string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE username = ?`, username,
	).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, errInvalidCredentials
	}
	if err != nil {
		return "", nil, fmt.Errorf("lookup user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", nil, errInvalidCredentials
	}

	token, err := s.issueToken(id, username)
	if err != nil {
		return "", nil, err
	}
	return token, &User{ID: id, Username: username}, nil
}

func (s *Service) issueToken(id int64, username string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": fmt.Sprintf("%d", id),
		"usr": username,
		"iat": now.Unix(),
		"exp": now.Add(s.expiry).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.secret)
}

// Middleware verifies the Authorization: Bearer <token> header on protected
// routes and injects the resolved user into the request context.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			// Fall back to a query token for endpoints used by <img>/<a> tags
			// where setting an Authorization header is not possible.
			if t := r.URL.Query().Get("token"); t != "" {
				header = "Bearer " + t
			}
		}
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		raw := strings.TrimPrefix(header, "Bearer ")

		user, err := s.parseToken(raw)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) parseToken(raw string) (*User, error) {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	sub, _ := claims["sub"].(string)
	usr, _ := claims["usr"].(string)
	if sub == "" || usr == "" {
		return nil, errors.New("missing claims")
	}

	var id int64
	if _, err := fmt.Sscanf(sub, "%d", &id); err != nil {
		return nil, errors.New("bad subject")
	}
	return &User{ID: id, Username: usr}, nil
}

// FromContext returns the authenticated user attached by Middleware. The
// boolean is false when the request was not authenticated.
func FromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userCtxKey).(*User)
	return u, ok
}

// LoginHandler handles POST /api/auth/login.
func (s *Service) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, user, err := s.Login(r.Context(), body.Username, body.Password)
	if errors.Is(err, errInvalidCredentials) {
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "login failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
		},
		"expires_in_seconds": int(s.expiry.Seconds()),
	})
}

// MeHandler returns the current authenticated user (used by the frontend to
// re-hydrate session state on page reload).
func (s *Service) MeHandler(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       u.ID,
		"username": u.Username,
	})
}

var errInvalidCredentials = errors.New("invalid credentials")

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
