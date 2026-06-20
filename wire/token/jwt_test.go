package token

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseJwtToken(t *testing.T) {
	tk1 := &Token{
		Account: "test1",
		App:     "kim",
	}
	secret := "123456"

	tokenString, err := Generate(secret, tk1)
	assert.Nil(t, err)
	t.Log(tokenString)
	Parse("123456", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBY2NvdW50IjoidGVzdDEiLCJBcHAiOiJraW0ifQ.ZB9X5bs3Xpm8ouclJkDP-w3h_vr42aEVCaI2S_XDgcQ")
	tk2, err := Parse(secret, tokenString)
	assert.Nil(t, err)
	assert.Equal(t, "test1", tk2.Account)
}
