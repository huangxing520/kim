// 文件：user_handler.go
// 职责：用户 gRPC 处理器——处理用户登录（验证密码 + 签发 JWT AccessToken）。
//
// 方法（均为 ServiceHandler 的方法）：
//   - Login(ctx, req) → 用户登录：查询用户 → 验证密码 → 生成 JWT → 缓存 AccessToken → 返回

package handler

import (
	"context"
	"fmt"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/logic/database"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/token"
)

func (h *ServiceHandler) Login(ctx context.Context, req *rpc.LoginReq) (*rpc.LoginResp, error) {
	var contents *database.User
	err := h.BaseDb.Model(&database.User{}).Where("account = ?", req.Account).First(&contents).Error
	if err != nil {
		return nil, err
	}
	if contents != nil && contents.Password == req.Password {
		value, _ := token.Generate("secret", &token.Token{
			Account: req.Account,
		})
		h.Cache.Set(req.Account, value, wire.AccessTokenExpiresIn)
		return &rpc.LoginResp{AccessToken: value}, nil
	}
	return nil, fmt.Errorf("login failed")
}
