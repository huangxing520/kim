package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/logic/data"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/token"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var AppSecret string

func (h *ServiceHandler) Login(ctx context.Context, req *rpc.LoginReq) (*rpc.LoginResp, error) {
	var user data.User
	err := h.BaseDb.Model(&data.User{}).Where("account = ?", req.Account).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("account not found")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	if AppSecret == "" {
		return nil, fmt.Errorf("app_secret not configured")
	}

	value, err := token.Generate(AppSecret, &token.Token{
		Account: req.Account,
		App:     user.App,
		Exp:     time.Now().Add(wire.AccessTokenExpiresIn).Unix(),
	})
	if err != nil {
		return nil, err
	}
	if err := h.Cache.Set(req.Account, value, wire.AccessTokenExpiresIn).Err(); err != nil {
		return nil, fmt.Errorf("cache set failed: %w", err)
	}
	return &rpc.LoginResp{AccessToken: value}, nil
}
