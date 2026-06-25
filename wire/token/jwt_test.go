package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestGenerateAndParse(t *testing.T) {
	secret := "test-secret-key-32bytes-minimum!"
	tk := &Token{
		Account: "test1",
		App:     "kim",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := Generate(secret, tk)
	assert.Nil(t, err)
	assert.NotEmpty(t, tokenStr)

	parsed, err := Parse(secret, tokenStr)
	assert.Nil(t, err)
	assert.Equal(t, "test1", parsed.Account)
	assert.Equal(t, "kim", parsed.App)
}

func TestParseExpiredToken(t *testing.T) {
	secret := "test-secret-key-32bytes-minimum!"
	tk := &Token{
		Account: "test1",
		Exp:     time.Now().Add(-time.Hour).Unix(),
	}
	tokenStr, err := Generate(secret, tk)
	assert.Nil(t, err)

	_, err = Parse(secret, tokenStr)
	assert.NotNil(t, err)
}

func TestParseWrongSecret(t *testing.T) {
	secret := "correct-secret-key-32bytes!!"
	wrongSecret := "wrong-secret-key-32bytes!!!!"
	tk := &Token{
		Account: "test1",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := Generate(secret, tk)
	assert.Nil(t, err)

	_, err = Parse(wrongSecret, tokenStr)
	assert.NotNil(t, err)
}

func TestParseAlgNoneAttack(t *testing.T) {
	claims := &Token{
		Account: "hacker",
		App:     "kim",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	assert.Nil(t, err)

	_, err = Parse("any-secret", tokenStr)
	assert.NotNil(t, err, "alg=none token must be rejected")
}

func TestGenerateNoPasswordInClaims(t *testing.T) {
	secret := "test-secret-key-32bytes-minimum!"
	tk := &Token{
		Account: "test1",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := Generate(secret, tk)
	assert.Nil(t, err)

	parsed, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
	assert.Nil(t, err)
	mapClaims := parsed.Claims.(jwt.MapClaims)
	_, hasPassword := mapClaims["passwd"]
	assert.False(t, hasPassword, "Password must not be in JWT claims")
	_, hasAccessToken := mapClaims["access"]
	assert.False(t, hasAccessToken, "AccessToken must not be in JWT claims")
}
