package service

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
)

type JWKSService struct {
	privateKey     *rsa.PrivateKey
	signingKID     string
	previousKID    string
	previousPubPEM string
}

type jwkKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwkKey `json:"keys"`
}

func NewJWKSService(privateKeyPath, signingKID, previousKID, previousPubPEM string) (*JWKSService, error) {
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", privateKeyPath, err)
	}
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %q", privateKeyPath)
	}
	var privateKey *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, fmt.Errorf("parse PKCS8: %w", e)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA key")
		}
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse RSA key: %w", err)
	}
	return &JWKSService{privateKey: privateKey, signingKID: signingKID, previousKID: previousKID, previousPubPEM: previousPubPEM}, nil
}

func (s *JWKSService) PrivateKey() *rsa.PrivateKey { return s.privateKey }
func (s *JWKSService) SigningKID() string           { return s.signingKID }

// IsReady returns true when the RSA private key has been loaded successfully.
// Used by the /ready endpoint to gate traffic until the key is available.
func (s *JWKSService) IsReady() bool { return s.privateKey != nil }

func (s *JWKSService) BuildJWKS() ([]byte, error) {
	set := jwkSet{}
	set.Keys = append(set.Keys, pubKeyToJWK(&s.privateKey.PublicKey, s.signingKID))
	if s.previousKID != "" && s.previousPubPEM != "" {
		if key, err := parsePubPEM(s.previousPubPEM); err == nil {
			set.Keys = append(set.Keys, pubKeyToJWK(key, s.previousKID))
		}
	}
	return json.Marshal(set)
}

func pubKeyToJWK(pub *rsa.PublicKey, kid string) jwkKey {
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	return jwkKey{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   base64URLEncode(nBytes),
		E:   base64URLEncode(eBytes),
	}
}

func base64URLEncode(b []byte) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (len(b)*4+2)/3)
	for i := 0; i < len(b); i += 3 {
		var b0, b1, b2 byte
		b0 = b[i]
		if i+1 < len(b) {
			b1 = b[i+1]
		}
		if i+2 < len(b) {
			b2 = b[i+2]
		}
		result = append(result, chars[b0>>2])
		result = append(result, chars[((b0&0x3)<<4)|b1>>4])
		if i+1 < len(b) {
			result = append(result, chars[((b1&0xf)<<2)|b2>>6])
		}
		if i+2 < len(b) {
			result = append(result, chars[b2&0x3f])
		}
	}
	return string(result)
}

func parsePubPEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not RSA")
	}
	return rsaKey, nil
}
