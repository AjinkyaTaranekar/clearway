package model

import "github.com/golang-jwt/jwt/v5"

type Claims struct {
	Sub   string `json:"sub"`
	Role  string `json:"role"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}
