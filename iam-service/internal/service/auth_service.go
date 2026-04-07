package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
)

// AuthService handles all authentication logic: registration, login, token
// rotation, logout, and force-logout.
type AuthService struct {
	db         *sql.DB // master pool — used to open transactions
	users      *repository.UserRepo
	tokens     *repository.TokenRepo
	jwks       *JWKSService
	accessTTL  time.Duration
	refreshTTL time.Duration
	bcryptCost int
	issuer     string
}

// NewAuthService wires the auth service. db must be the master pool so that
// transactions opened here land on the writable node.
func NewAuthService(
	db *sql.DB,
	users *repository.UserRepo,
	tokens *repository.TokenRepo,
	jwks *JWKSService,
	accessTTL, refreshTTL time.Duration,
	bcryptCost int,
) *AuthService {
	return &AuthService{
		db:         db,
		users:      users,
		tokens:     tokens,
		jwks:       jwks,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		bcryptCost: bcryptCost,
		issuer:     "traffic-iam",
	}
}

// RegisterInput carries validated fields from the register handler.
type RegisterInput struct {
	Name        string
	Email       string
	Password    string
	VehicleType model.VehicleType
	LicenseInfo model.LicenseInfo
}

// AuthResult is returned from every successful auth operation.
type AuthResult struct {
	AccessToken  string
	RefreshToken string
	User         *model.User
}

// Register creates a new user account and issues an initial token pair.
// The user INSERT and the refresh-token INSERT execute inside a single
// transaction so a crash between the two never leaves a user with no session.
func (s *AuthService) Register(ctx context.Context, in RegisterInput, userAgent, ip string) (*AuthResult, error) {
	emailLower := strings.ToLower(in.Email)

	// Hash the password before opening the transaction — bcrypt is CPU-heavy
	// and we must not hold a DB connection while computing it.
	hash, err := hashPassword(in.Password, s.bcryptCost)
	if err != nil {
		return nil, apperrors.InternalError("failed to hash password", err)
	}

	userID := "usr_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, apperrors.InternalError("failed to begin transaction", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	user, err := s.users.CreateTx(ctx, tx, userID, in.Name, in.Email, emailLower, hash, model.RoleDriver, in.VehicleType, in.LicenseInfo)
	if err != nil {
		// Use the structured pq.Error code instead of fragile string matching.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, &apperrors.AppError{
				Code:    "EMAIL_ALREADY_EXISTS",
				Message: "An account with this email already exists.",
				Status:  http.StatusConflict,
			}
		}
		return nil, apperrors.InternalError("failed to create user", err)
	}

	result, err := s.issueTokensTx(ctx, tx, user, userAgent, ip)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperrors.InternalError("failed to commit registration", err)
	}
	return result, nil
}

// Login verifies credentials and issues a new token pair.
// No surrounding transaction is needed: a failure after password verification
// just means the user retries — there is no half-committed state to clean up.
func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string) (*AuthResult, error) {
	emailLower := strings.ToLower(email)
	user, storedHash, err := s.users.GetByEmail(ctx, emailLower)
	if err != nil {
		if err == sql.ErrNoRows {
			// Run a real bcrypt comparison so the response time is
			// indistinguishable from a wrong-password response (timing-safe).
			_ = verifyPassword("dummy", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
			return nil, apperrors.InvalidCredentials("Email or password is incorrect.")
		}
		return nil, apperrors.InternalError("database error", err)
	}
	if err := verifyPassword(password, storedHash); err != nil {
		return nil, apperrors.InvalidCredentials("Email or password is incorrect.")
	}
	return s.issueTokens(ctx, user, userAgent, ip)
}

// Refresh atomically revokes the supplied refresh token and issues a new pair.
// All four DB operations (lock token, revoke token, fetch user, insert new
// token) run inside a single transaction so the user can never be left in a
// state where the old token is gone but no new token was issued.
func (s *AuthService) Refresh(ctx context.Context, rawToken, userAgent, ip string) (*AuthResult, error) {
	tokenHash := hashToken(rawToken)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, apperrors.InternalError("failed to begin transaction", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// SELECT FOR UPDATE locks the row so that two concurrent refresh requests
	// carrying the same token cannot both see it as active.
	stored, err := s.tokens.GetActiveByHashTx(ctx, tx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &apperrors.AppError{
				Code:    "INVALID_REFRESH_TOKEN",
				Message: "Refresh token is invalid, expired, or already revoked.",
				Status:  http.StatusUnauthorized,
			}
		}
		return nil, apperrors.InternalError("database error", err)
	}

	revoked, err := s.tokens.RevokeTx(ctx, tx, tokenHash)
	if err != nil {
		return nil, apperrors.InternalError("failed to revoke token", err)
	}
	if !revoked {
		// The FOR UPDATE prevents this in practice, but handle it defensively.
		return nil, &apperrors.AppError{
			Code:    "INVALID_REFRESH_TOKEN",
			Message: "Refresh token has already been used.",
			Status:  http.StatusUnauthorized,
		}
	}

	user, err := s.users.GetByIDTx(ctx, tx, stored.UserID)
	if err != nil {
		return nil, apperrors.InternalError("user not found", err)
	}

	result, err := s.issueTokensTx(ctx, tx, user, userAgent, ip)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperrors.InternalError("failed to commit token refresh", err)
	}
	return result, nil
}

// Logout revokes a single refresh token after verifying the caller owns it.
func (s *AuthService) Logout(ctx context.Context, userID, rawToken string) error {
	tokenHash := hashToken(rawToken)
	stored, err := s.tokens.GetActiveByHash(ctx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return &apperrors.AppError{Code: "TOKEN_NOT_FOUND", Message: "Refresh token not found.", Status: http.StatusNotFound}
		}
		return apperrors.InternalError("database error", err)
	}
	if stored.UserID != userID {
		return &apperrors.AppError{Code: "TOKEN_NOT_FOUND", Message: "Refresh token not found.", Status: http.StatusNotFound}
	}
	_, err = s.tokens.Revoke(ctx, tokenHash)
	return err
}

// ForceLogout revokes all active refresh tokens for a user (admin action).
func (s *AuthService) ForceLogout(ctx context.Context, userID string) error {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NotFound("User not found.")
		}
		return apperrors.InternalError("database error", err)
	}
	return s.tokens.RevokeAllForUser(ctx, userID)
}

// --- internal helpers ---

// issueTokens signs a new JWT and inserts a refresh token (auto-committed).
// Used by Login where no surrounding transaction exists.
func (s *AuthService) issueTokens(ctx context.Context, user *model.User, userAgent, ip string) (*AuthResult, error) {
	now := time.Now()
	accessToken, err := s.signAccessToken(user, now)
	if err != nil {
		return nil, err
	}
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		return nil, apperrors.InternalError("failed to generate refresh token", err)
	}
	if err := s.tokens.Create(ctx, user.ID, hashToken(rawRefresh), userAgent, ip, now.Add(s.refreshTTL)); err != nil {
		return nil, apperrors.InternalError("failed to store refresh token", err)
	}
	return &AuthResult{AccessToken: accessToken, RefreshToken: rawRefresh, User: user}, nil
}

// issueTokensTx signs a new JWT and inserts a refresh token inside tx.
// Used by Register and Refresh so the token write is part of their transaction.
func (s *AuthService) issueTokensTx(ctx context.Context, tx *sql.Tx, user *model.User, userAgent, ip string) (*AuthResult, error) {
	now := time.Now()
	accessToken, err := s.signAccessToken(user, now)
	if err != nil {
		return nil, err
	}
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		return nil, apperrors.InternalError("failed to generate refresh token", err)
	}
	if err := s.tokens.CreateTx(ctx, tx, user.ID, hashToken(rawRefresh), userAgent, ip, now.Add(s.refreshTTL)); err != nil {
		return nil, apperrors.InternalError("failed to store refresh token", err)
	}
	return &AuthResult{AccessToken: accessToken, RefreshToken: rawRefresh, User: user}, nil
}

// signAccessToken builds and RS256-signs a JWT for the given user.
func (s *AuthService) signAccessToken(user *model.User, now time.Time) (string, error) {
	claims := model.Claims{
		Sub:   user.ID,
		Role:  string(user.Role),
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.jwks.SigningKID()
	signed, err := token.SignedString(s.jwks.PrivateKey())
	if err != nil {
		return "", apperrors.InternalError("failed to sign access token", err)
	}
	return signed, nil
}

func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func clampBcryptCost(cost int) int {
	if cost < bcrypt.MinCost {
		return bcrypt.DefaultCost
	}
	if cost > bcrypt.MaxCost {
		return bcrypt.MaxCost
	}
	return cost
}

func hashPassword(password string, cost int) (string, error) {
	cost = clampBcryptCost(cost)
	b, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(b), err
}

func verifyPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
