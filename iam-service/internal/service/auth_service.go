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
	db         *sql.DB // master pool - used to open transactions
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
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AuthService.Register").
		Str("email", in.Email).
		Str("vehicle_type", string(in.VehicleType)).
		Str("ip", ip).
		Msg("starting user registration flow")

	// Hash the password before opening the transaction - bcrypt is CPU-heavy
	// and we must not hold a DB connection while computing it.
	hash, err := hashPassword(in.Password, s.bcryptCost)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Register").
			Err(err).
			Str("email", in.Email).
			Msg("password hashing failed during registration")
		return nil, apperrors.InternalError("failed to hash password", err)
	}

	userID := "usr_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	log.Debug().
		Str("service", "AuthService.Register").
		Str("generated_user_id", userID).
		Msg("generated user identifier")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Register").
			Err(err).
			Msg("failed to begin registration transaction")
		return nil, apperrors.InternalError("failed to begin transaction", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	log.Debug().
		Str("service", "AuthService.Register").
		Msg("registration transaction started")

	user, err := s.users.CreateTx(ctx, tx, userID, in.Name, in.Email, emailLower, hash, model.RoleDriver, in.VehicleType, in.LicenseInfo)
	if err != nil {
		// Use the structured pq.Error code instead of fragile string matching.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			log.Warn().
				Str("service", "AuthService.Register").
				Str("email", in.Email).
				Msg("registration rejected: email already exists")
			return nil, &apperrors.AppError{
				Code:    "EMAIL_ALREADY_EXISTS",
				Message: "An account with this email already exists.",
				Status:  http.StatusConflict,
			}
		}
		log.Error().
			Str("service", "AuthService.Register").
			Err(err).
			Str("email", in.Email).
			Msg("failed to insert user during registration")
		return nil, apperrors.InternalError("failed to create user", err)
	}
	log.Info().
		Str("service", "AuthService.Register").
		Str("user_id", user.ID).
		Msg("user row created, issuing initial token pair")

	result, err := s.issueTokensTx(ctx, tx, user, userAgent, ip)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Register").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to issue tokens during registration")
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		log.Error().
			Str("service", "AuthService.Register").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to commit registration transaction")
		return nil, apperrors.InternalError("failed to commit registration", err)
	}
	log.Info().
		Str("service", "AuthService.Register").
		Str("user_id", user.ID).
		Msg("registration flow completed successfully")
	return result, nil
}

// Login verifies credentials and issues a new token pair.
// No surrounding transaction is needed: a failure after password verification
// just means the user retries - there is no half-committed state to clean up.
func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string) (*AuthResult, error) {
	emailLower := strings.ToLower(email)
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AuthService.Login").
		Str("email", email).
		Str("ip", ip).
		Msg("starting login flow")

	user, storedHash, err := s.users.GetByEmail(ctx, emailLower)
	if err != nil {
		if err == sql.ErrNoRows {
			// Run a real bcrypt comparison so the response time is
			// indistinguishable from a wrong-password response (timing-safe).
			_ = verifyPassword("dummy", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
			log.Warn().
				Str("service", "AuthService.Login").
				Str("email", email).
				Msg("login failed: user not found")
			return nil, apperrors.InvalidCredentials("Email or password is incorrect.")
		}
		log.Error().
			Str("service", "AuthService.Login").
			Err(err).
			Str("email", email).
			Msg("database error while loading user for login")
		return nil, apperrors.InternalError("database error", err)
	}
	if err := verifyPassword(password, storedHash); err != nil {
		log.Warn().
			Str("service", "AuthService.Login").
			Str("user_id", user.ID).
			Str("email", email).
			Msg("login failed: password verification failed")
		return nil, apperrors.InvalidCredentials("Email or password is incorrect.")
	}

	log.Info().
		Str("service", "AuthService.Login").
		Str("user_id", user.ID).
		Msg("credentials verified, issuing token pair")
	return s.issueTokens(ctx, user, userAgent, ip)
}

// Refresh atomically revokes the supplied refresh token and issues a new pair.
// All four DB operations (lock token, revoke token, fetch user, insert new
// token) run inside a single transaction so the user can never be left in a
// state where the old token is gone but no new token was issued.
func (s *AuthService) Refresh(ctx context.Context, rawToken, userAgent, ip string) (*AuthResult, error) {
	tokenHash := hashToken(rawToken)
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AuthService.Refresh").
		Str("ip", ip).
		Msg("starting refresh token flow")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Msg("failed to begin refresh transaction")
		return nil, apperrors.InternalError("failed to begin transaction", err)
	}
	defer tx.Rollback() //nolint:errcheck
	log.Debug().Str("service", "AuthService.Refresh").Msg("refresh transaction started")

	// SELECT FOR UPDATE locks the row so that two concurrent refresh requests
	// carrying the same token cannot both see it as active.
	stored, err := s.tokens.GetActiveByHashTx(ctx, tx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AuthService.Refresh").
				Msg("refresh rejected: token not active or already revoked")
			return nil, &apperrors.AppError{
				Code:    "INVALID_REFRESH_TOKEN",
				Message: "Refresh token is invalid, expired, or already revoked.",
				Status:  http.StatusUnauthorized,
			}
		}
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Msg("database error while loading active refresh token")
		return nil, apperrors.InternalError("database error", err)
	}
	log.Debug().
		Str("service", "AuthService.Refresh").
		Str("user_id", stored.UserID).
		Msg("active refresh token loaded and locked")

	revoked, err := s.tokens.RevokeTx(ctx, tx, tokenHash)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Str("user_id", stored.UserID).
			Msg("failed to revoke existing refresh token")
		return nil, apperrors.InternalError("failed to revoke token", err)
	}
	if !revoked {
		// The FOR UPDATE prevents this in practice, but handle it defensively.
		log.Warn().
			Str("service", "AuthService.Refresh").
			Str("user_id", stored.UserID).
			Msg("refresh rejected: token already consumed")
		return nil, &apperrors.AppError{
			Code:    "INVALID_REFRESH_TOKEN",
			Message: "Refresh token has already been used.",
			Status:  http.StatusUnauthorized,
		}
	}
	log.Debug().
		Str("service", "AuthService.Refresh").
		Str("user_id", stored.UserID).
		Msg("existing refresh token revoked")

	user, err := s.users.GetByIDTx(ctx, tx, stored.UserID)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Str("user_id", stored.UserID).
			Msg("failed to load user for token refresh")
		return nil, apperrors.InternalError("user not found", err)
	}
	log.Debug().
		Str("service", "AuthService.Refresh").
		Str("user_id", user.ID).
		Msg("user loaded for token refresh")

	result, err := s.issueTokensTx(ctx, tx, user, userAgent, ip)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to issue replacement token pair")
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		log.Error().
			Str("service", "AuthService.Refresh").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to commit refresh transaction")
		return nil, apperrors.InternalError("failed to commit token refresh", err)
	}
	log.Info().
		Str("service", "AuthService.Refresh").
		Str("user_id", user.ID).
		Msg("refresh token flow completed successfully")
	return result, nil
}

// Logout revokes a single refresh token after verifying the caller owns it.
func (s *AuthService) Logout(ctx context.Context, userID, rawToken string) error {
	tokenHash := hashToken(rawToken)
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AuthService.Logout").
		Str("user_id", userID).
		Msg("starting logout flow")

	stored, err := s.tokens.GetActiveByHash(ctx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AuthService.Logout").
				Str("user_id", userID).
				Msg("logout token not found")
			return &apperrors.AppError{Code: "TOKEN_NOT_FOUND", Message: "Refresh token not found.", Status: http.StatusNotFound}
		}
		log.Error().
			Str("service", "AuthService.Logout").
			Err(err).
			Str("user_id", userID).
			Msg("database error during logout token lookup")
		return apperrors.InternalError("database error", err)
	}
	if stored.UserID != userID {
		log.Warn().
			Str("service", "AuthService.Logout").
			Str("user_id", userID).
			Str("token_owner_user_id", stored.UserID).
			Msg("logout denied: token does not belong to caller")
		return &apperrors.AppError{Code: "TOKEN_NOT_FOUND", Message: "Refresh token not found.", Status: http.StatusNotFound}
	}
	log.Debug().
		Str("service", "AuthService.Logout").
		Str("user_id", userID).
		Msg("revoking caller refresh token")
	_, err = s.tokens.Revoke(ctx, tokenHash)
	if err != nil {
		log.Error().
			Str("service", "AuthService.Logout").
			Err(err).
			Str("user_id", userID).
			Msg("failed to revoke refresh token during logout")
		return err
	}

	log.Info().
		Str("service", "AuthService.Logout").
		Str("user_id", userID).
		Msg("logout flow completed")
	return err
}

// ForceLogout revokes all active refresh tokens for a user (admin action).
func (s *AuthService) ForceLogout(ctx context.Context, userID string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AuthService.ForceLogout").
		Str("user_id", userID).
		Msg("starting force logout flow")

	if _, err := s.users.GetByID(ctx, userID); err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AuthService.ForceLogout").
				Str("user_id", userID).
				Msg("force logout failed: user not found")
			return apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "AuthService.ForceLogout").
			Err(err).
			Str("user_id", userID).
			Msg("database error while validating user for force logout")
		return apperrors.InternalError("database error", err)
	}
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		log.Error().
			Str("service", "AuthService.ForceLogout").
			Err(err).
			Str("user_id", userID).
			Msg("failed to revoke all user tokens")
		return err
	}

	log.Info().
		Str("service", "AuthService.ForceLogout").
		Str("user_id", userID).
		Msg("force logout flow completed")
	return nil
}

// --- internal helpers ---

// issueTokens signs a new JWT and inserts a refresh token (auto-committed).
// Used by Login where no surrounding transaction exists.
func (s *AuthService) issueTokens(ctx context.Context, user *model.User, userAgent, ip string) (*AuthResult, error) {
	now := time.Now()
	log := logWithTrace(ctx)
	log.Debug().
		Str("service", "AuthService.issueTokens").
		Str("user_id", user.ID).
		Msg("issuing access and refresh tokens")

	accessToken, err := s.signAccessToken(user, now)
	if err != nil {
		log.Error().
			Str("service", "AuthService.issueTokens").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to sign access token")
		return nil, err
	}
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		log.Error().
			Str("service", "AuthService.issueTokens").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to generate refresh token")
		return nil, apperrors.InternalError("failed to generate refresh token", err)
	}
	if err := s.tokens.Create(ctx, user.ID, hashToken(rawRefresh), userAgent, ip, now.Add(s.refreshTTL)); err != nil {
		log.Error().
			Str("service", "AuthService.issueTokens").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to persist refresh token")
		return nil, apperrors.InternalError("failed to store refresh token", err)
	}
	log.Debug().
		Str("service", "AuthService.issueTokens").
		Str("user_id", user.ID).
		Msg("token pair issued successfully")
	return &AuthResult{AccessToken: accessToken, RefreshToken: rawRefresh, User: user}, nil
}

// issueTokensTx signs a new JWT and inserts a refresh token inside tx.
// Used by Register and Refresh so the token write is part of their transaction.
func (s *AuthService) issueTokensTx(ctx context.Context, tx *sql.Tx, user *model.User, userAgent, ip string) (*AuthResult, error) {
	now := time.Now()
	log := logWithTrace(ctx)
	log.Debug().
		Str("service", "AuthService.issueTokensTx").
		Str("user_id", user.ID).
		Msg("issuing transactional access and refresh tokens")

	accessToken, err := s.signAccessToken(user, now)
	if err != nil {
		log.Error().
			Str("service", "AuthService.issueTokensTx").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to sign access token in transaction")
		return nil, err
	}
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		log.Error().
			Str("service", "AuthService.issueTokensTx").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to generate refresh token in transaction")
		return nil, apperrors.InternalError("failed to generate refresh token", err)
	}
	if err := s.tokens.CreateTx(ctx, tx, user.ID, hashToken(rawRefresh), userAgent, ip, now.Add(s.refreshTTL)); err != nil {
		log.Error().
			Str("service", "AuthService.issueTokensTx").
			Err(err).
			Str("user_id", user.ID).
			Msg("failed to persist refresh token in transaction")
		return nil, apperrors.InternalError("failed to store refresh token", err)
	}
	log.Debug().
		Str("service", "AuthService.issueTokensTx").
		Str("user_id", user.ID).
		Msg("transactional token pair issued successfully")
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
