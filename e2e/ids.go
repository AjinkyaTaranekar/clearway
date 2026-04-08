package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func newID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UTC().UnixNano()
		if prefix == "" {
			return fmt.Sprintf("id-%d", now)
		}
		return fmt.Sprintf("%s-%d", sanitizePrefix(prefix), now)
	}

	token := hex.EncodeToString(buf)
	if prefix == "" {
		return "id-" + token
	}
	return sanitizePrefix(prefix) + "-" + token
}

func sanitizePrefix(prefix string) string {
	clean := strings.ToLower(strings.TrimSpace(prefix))
	if clean == "" {
		return "id"
	}
	clean = strings.ReplaceAll(clean, " ", "-")
	clean = strings.ReplaceAll(clean, "_", "-")
	return clean
}

func isJWTLike(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}
