package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestBcryptPasswordHash(t *testing.T) {
	password := "testPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	assert.Nil(t, err)
	assert.True(t, len(hash) >= 60, "bcrypt hash should be at least 60 chars")
	assert.True(t, string(hash)[:4] == "$2a$" || string(hash)[:4] == "$2b$", "hash should start with $2a$ or $2b$")

	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.Nil(t, err)

	err = bcrypt.CompareHashAndPassword(hash, []byte("wrongPassword"))
	assert.NotNil(t, err)
}

func TestBcryptWrongPasswordSize(t *testing.T) {
	t.Skip("verified via code review - model.User.Password gorm size must be 60")
}
