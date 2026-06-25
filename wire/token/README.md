# wire/token 模块 - JWT Token 认证

## 1. 模块概述

`wire/token` 模块提供基于 JWT（JSON Web Token）的认证功能，用于客户端登录时的身份验证和 Token 签发/校验。使用 HMAC-SHA256 签名算法。

## 2. 协议/架构设计

### JWT Claims 结构

Token 中包含以下自定义 Claims：

```json
{
  "acc": "user_account",   // Account - 用户账号
  "app": "app_id",         // App - 应用标识
  "exp": 1735689600        // Exp - 过期时间戳（Unix 秒）
}
```

### 认证流程

```
客户端                          服务端
  │                               │
  │  1. 发送登录请求(账号+密码)    │
  │ ─────────────────────────────>│
  │                               │ 2. 验证账号密码
  │                               │ 3. 生成 JWT Token
  │  4. 返回 Token                │
  │ <─────────────────────────────│
  │                               │
  │  5. 后续请求携带 Token        │
  │ ─────────────────────────────>│
  │                               │ 6. 验证 Token 有效性/过期时间
```

## 3. 关键组件

### 3.1 Token 结构体

```go
type Token struct {
    Account string `json:"acc,omitempty"`  // 用户账号
    App     string `json:"app,omitempty"`  // 应用标识
    Exp     int64  `json:"exp,omitempty"`  // 过期时间戳（Unix 秒）
}
```

`Token` 实现了 `jwt.Claims` 接口，可直接用于 `jwt.NewWithClaims` 和 `jwt.ParseWithClaims`。

### 3.2 核心函数

| 函数 | 说明 |
|------|------|
| `Generate(secret string, t *Token) (string, error)` | 使用密钥签发 JWT Token |
| `Parse(secret, tokenStr string) (*Token, error)` | 解析并验证 JWT Token |

### 3.3 Token 方法

| 方法 | 说明 |
|------|------|
| `Validate() error` | 验证 Token 是否过期、Account 是否为空 |
| `GetExpirationTime()` | 获取过期时间（jwt.NumericDate） |
| `String() string` | 返回 Token 的字符串表示 |

### 3.4 错误定义

| 错误变量 | 说明 |
|---------|------|
| `ErrExpiredToken` | Token 已过期 |
| `ErrInvalidToken` | Token 无效 |
| `ErrWrongMethod` | 签名算法错误（非 HMAC） |

## 4. 核心数据结构

无额外复杂数据结构，核心为 `Token` 结构体（见 3.1）。

## 5. 使用示例

### 5.1 签发 Token

```go
import (
    "time"
    "github.com/klintcheng/kim/wire"
    "github.com/klintcheng/kim/wire/token"
)

// 定义密钥（生产环境从配置读取）
const secret = "your-secret-key"

// 创建 Token
t := &token.Token{
    Account: "user123",
    App:     "kim",
    Exp:     time.Now().Add(wire.AccessTokenExpiresIn).Unix(), // 24小时后过期
}

// 签发 Token 字符串
tokenStr, err := token.Generate(secret, t)
if err != nil {
    // 处理错误
}
fmt.Println("Token:", tokenStr)
```

### 5.2 解析和验证 Token

```go
import "github.com/klintcheng/kim/wire/token"

// 解析 Token
t, err := token.Parse(secret, tokenStr)
if err != nil {
    if errors.Is(err, token.ErrExpiredToken) {
        // Token 已过期，需要重新登录
    } else if errors.Is(err, token.ErrInvalidToken) {
        // Token 无效
    }
    return
}

// 使用 Token 信息
fmt.Println("Account:", t.Account)
fmt.Println("App:", t.App)
```

### 5.3 手动验证 Token

```go
t := &token.Token{
    Account: "user123",
    Exp:     time.Now().Add(time.Hour).Unix(),
}

if err := t.Validate(); err != nil {
    // Token 无效或已过期
}
```

### 5.4 与登录流程集成示例

```go
// 服务端登录处理
func LoginHandler(account, password string) (string, error) {
    // 1. 验证账号密码（伪代码）
    user, err := db.FindUser(account)
    if err != nil || !checkPassword(password, user.Password) {
        return "", errors.New("invalid credentials")
    }

    // 2. 生成 Token
    t := &token.Token{
        Account: account,
        App:     "kim",
        Exp:     time.Now().Add(wire.AccessTokenExpiresIn).Unix(),
    }
    return token.Generate(jwtSecret, t)
}
```

## 6. 注意事项

### 安全注意事项

1. **密钥管理**：`secret` 必须保密，不要硬编码在代码中，应通过配置文件或环境变量注入
2. **签名算法**：固定使用 HMAC-SHA256（`jwt.SigningMethodHS256`），`Parse` 时会校验算法防止算法篡改攻击
3. **过期时间**：默认 Token 过期时间为 24 小时（`wire.AccessTokenExpiresIn`），根据业务需求调整
4. **HTTPS**：Token 在网络传输时必须使用 HTTPS/WSS 加密，防止 Token 被窃听

### 使用注意事项

- `Generate` 和 `Parse` 的 `secret` 参数不能为空，否则返回 error
- `Parse` 会自动验证签名和 Token 有效性，无效时返回 `ErrInvalidToken`
- `Validate()` 方法仅检查过期时间和 Account 是否为空，不验证签名（适用于已解析后的本地校验）
- Token 的其他 JWT Claims（Issuer、Subject、Audience 等）返回空值，未使用
- 建议客户端在 Token 过期前主动刷新，或在收到认证失败响应后重新登录
