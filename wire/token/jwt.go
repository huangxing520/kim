package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidToken = errors.New("invalid token")
	ErrWrongMethod  = errors.New("wrong signing method")
)

type Token struct {
	Account string `json:"acc,omitempty"`
	App     string `json:"app,omitempty"`
	Exp     int64  `json:"exp,omitempty"`
}

func (t *Token) Validate() error {
	if t.Exp < time.Now().Unix() {
		return ErrExpiredToken
	}
	if t.Account == "" {
		return ErrInvalidToken
	}
	return nil
}

func (t *Token) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(t.Exp, 0)), nil
}

func (t *Token) GetIssuedAt() (*jwt.NumericDate, error) {
	return nil, nil
}

func (t *Token) GetNotBefore() (*jwt.NumericDate, error) {
	return nil, nil
}

func (t *Token) GetIssuer() (string, error) {
	return "", nil
}

func (t *Token) GetSubject() (string, error) {
	return "", nil
}

func (t *Token) GetAudience() (jwt.ClaimStrings, error) {
	return nil, nil
}

func Parse(secret, tokenStr string) (*Token, error) {
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	claims := &Token{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: %v", ErrWrongMethod, t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !tok.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func Generate(secret string, t *Token) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret is required")
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, t)
	return tok.SignedString([]byte(secret))
}

func (t *Token) String() string {
	return fmt.Sprintf("Token{Account:%s App:%s Exp:%d}", t.Account, t.App, t.Exp)
}
