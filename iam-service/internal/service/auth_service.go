package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"github.com/google/uuid"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
)

type AuthService struct {
	users      *repository.UserRepo
	tokens     *repository.TokenRepo
	jwks       *JWKSService
	accessTTL  time.Duration
	refreshTTL time.Duration
	bcryptCost int
	issuer     string
}

func NewAuthService(users *repository.UserRepo, tokens *repository.TokenRepo, jwks *JWKSService, accessTTL, refreshTTL time.Duration, bcryptCost int) *AuthService {
	return &AuthService{users: users, tokens: tokens, jwks: jwks, accessTTL: accessTTL, refreshTTL: refreshTTL, bcryptCost: bcryptCost, issuer: "traffic-iam"}
}

type RegisterInput struct {
	Name        string
	Email       string
	Password    string
	VehicleType model.VehicleType
	LicenseInfo model.LicenseInfo
}

type AuthResult struct {
	AccessToken  string
	RefreshToken string
	User         *model.User
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput, userAgent, ip string) (*AuthResult, error) {
	emailLower := strings.ToLower(in.Email)
	hash, err := hashPassword(in.Password, s.bcryptCost)
	if err != nil {
		return nil, apperrors.InternalError("failed to hash password", err)
	}
	userID := "usr_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	user, err := s.users.Create(ctx, userID, in.Name, in.Email, emailLower, hash, model.RoleDriver, in.VehicleType, in.LicenseInfo)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return nil, &apperrors.AppError{Code: "EMAIL_ALREADY_EXISTS", Message: "An account with this email already exists.", Status: http.StatusConflict}
		}
		return nil, apperrors.InternalError("failed to create user", err)
	}
	return s.issueTokens(ctx, user, userAgent, ip)
}

func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string) (*AuthResult, error) {
	emailLower := strings.ToLower(email)
	user, storedHash, err := s.users.GetByEmail(ctx, emailLower)
	if err != nil {
		if err == sql.ErrNoRows {
			// Valid-format bcrypt so CompareHashAndPassword does real work (timing).
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

func (s *AuthService) Refresh(ctx context.Context, rawToken, userAgent, ip string) (*AuthResult, error) {
	tokenHash := hashToken(rawToken)
	stored, err := s.tokens.GetActiveByHash(ctx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &apperrors.AppError{Code: "INVALID_REFRESH_TOKEN", Message: "Refresh token is invalid, expired, or already revoked.", Status: http.StatusUnauthorized}
		}
		return nil, apperrors.InternalError("database error", err)
	}
	revoked, err := s.tokens.Revoke(ctx, tokenHash)
	if err != nil {
		return nil, apperrors.InternalError("failed to revoke token", err)
	}
	if !revoked {
		return nil, &apperrors.AppError{Code: "INVALID_REFRESH_TOKEN", Message: "Refresh token has already been used.", Status: http.StatusUnauthorized}
	}
	user, err := s.users.GetByID(ctx, stored.UserID)
	if err != nil {
		return nil, apperrors.InternalError("user not found", err)
	}
	return s.issueTokens(ctx, user, userAgent, ip)
}

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

func (s *AuthService) ForceLogout(ctx context.Context, userID string) error {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NotFound("User not found.")
		}
		return apperrors.InternalError("database error", err)
	}
	return s.tokens.RevokeAllForUser(ctx, userID)
}

func (s *AuthService) issueTokens(ctx context.Context, user *model.User, userAgent, ip string) (*AuthResult, error) {
	now := time.Now()
	claims := model.Claims{
		Sub: user.ID, Role: string(user.Role), Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: s.issuer, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.jwks.SigningKID()
	accessToken, err := token.SignedString(s.jwks.PrivateKey())
	if err != nil {
		return nil, apperrors.InternalError("failed to sign token", err)
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

func hashPassword(password string, cost int) (string, error) {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}
	if cost > bcrypt.MaxCost {
		cost = bcrypt.MaxCost
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(b), err
}

func verifyPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
