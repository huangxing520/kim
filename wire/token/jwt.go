// 文件：jwt.go
// 职责：JWT 令牌生成与解析——基于 dgrijalva/jwt-go 的 Token 签发和验证。
//
// 常量：
//   - DefaultSecret：默认测试密钥
//
// 定义的类型：
//   - Token 结构体：JWT Claims（Account / App / Exp / Password / AccessToken）
//
// 方法：
//   - (Token).Valid()                    → 验证 Token 是否过期
//   - Parse(secret, tokenStr)            → 解析并验证 JWT Token 字符串
//   - Generate(secret, token)            → 使用密钥签发 JWT Token 字符串

package token

import (
	"errors"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
)

// DefaultSecret 默认测试密钥
const (
	DefaultSecret = "jwt-1sNzdiSgnNuxyq2g7xml2JvLArU"
)

// Token JWT Claims 结构
type Token struct {
	Account     string `json:"acc,omitempty"`
	App         string `json:"app,omitempty"`
	Exp         int64  `json:"exp,omitempty"`
	Password    string `json:"passwd,omitempty"`
	AccessToken string `json:"access,omitempty"`
}

var errExpiredToken = errors.New("expired token")

// Valid 验证 Token 是否过期
func (t *Token) Valid() error {
	if t.Exp < time.Now().Unix() {
		return errExpiredToken
	}
	return nil
}

// Parse ParseJwtToken
func Parse(secret, tk string) (*Token, error) {
	var token = new(Token)
	_, err := jwtgo.ParseWithClaims(tk, token, func(jwttk *jwtgo.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	return token, nil
}

// Generate a JWT token
func Generate(secret string, token *Token) (string, error) {
	jtk := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, token)
	return jtk.SignedString([]byte(secret))
}
