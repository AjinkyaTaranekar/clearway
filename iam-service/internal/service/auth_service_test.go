package service

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// hashPassword / verifyPassword
// ---------------------------------------------------------------------------

func TestHashAndVerifyPassword_CorrectPassword(t *testing.T) {
	const pw = "Secret123"
	hash, err := hashPassword(pw, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if err := verifyPassword(pw, hash); err != nil {
		t.Errorf("verifyPassword with correct password: %v", err)
	}
}

func TestHashAndVerifyPassword_WrongPassword(t *testing.T) {
	hash, err := hashPassword("RealPass1", bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if err := verifyPassword("WrongPass1", hash); err == nil {
		t.Error("expected error for wrong password, got nil")
	}
}

func TestHashPassword_CostClamping(t *testing.T) {
	if got := clampBcryptCost(-99); got != bcrypt.DefaultCost {
		t.Errorf("clampBcryptCost(-99) = %d, want %d", got, bcrypt.DefaultCost)
	}
	if got := clampBcryptCost(bcrypt.MaxCost + 1000); got != bcrypt.MaxCost {
		t.Errorf("clampBcryptCost(max+1000) = %d, want %d", got, bcrypt.MaxCost)
	}
}

// ---------------------------------------------------------------------------
// hashToken
// ---------------------------------------------------------------------------

func TestHashToken_Deterministic(t *testing.T) {
	raw := "some-opaque-token-value"
	h1 := hashToken(raw)
	h2 := hashToken(raw)
	if h1 != h2 {
		t.Errorf("hashToken must be deterministic: %q != %q", h1, h2)
	}
}

func TestHashToken_Length(t *testing.T) {
	h := hashToken("any-input")
	// SHA-256 → 32 bytes → 64 hex chars
	if len(h) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h))
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	if hashToken("token-a") == hashToken("token-b") {
		t.Error("different inputs must produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// generateOpaqueToken
// ---------------------------------------------------------------------------

func TestGenerateOpaqueToken_Length(t *testing.T) {
	tok, err := generateOpaqueToken()
	if err != nil {
		t.Fatalf("generateOpaqueToken: %v", err)
	}
	// 32 random bytes → 64 hex chars
	if len(tok) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(tok))
	}
}

func TestGenerateOpaqueToken_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok, err := generateOpaqueToken()
		if err != nil {
			t.Fatalf("generateOpaqueToken iteration %d: %v", i, err)
		}
		if seen[tok] {
			t.Errorf("duplicate token generated on iteration %d: %q", i, tok)
		}
		seen[tok] = true
	}
}

// ---------------------------------------------------------------------------
// hashToken round-trip (hash → hash is idempotent, raw ≠ hashed)
// ---------------------------------------------------------------------------

func TestHashToken_RawNotEqualToHash(t *testing.T) {
	raw := "my-secret-refresh-token"
	hashed := hashToken(raw)
	if raw == hashed {
		t.Error("raw token and its hash must differ")
	}
}
