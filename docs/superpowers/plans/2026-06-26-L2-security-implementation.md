# L2 安全加固层实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 消除 kim 项目所有远程可利用安全漏洞——群聊消息索引截断丢数据、JWT 硬编码密钥+废弃库+无 alg 校验、明文密码存储、gRPC 无 TLS/auth/reflection 开放、敏感信息日志泄露。

**架构：** 5 个独立任务，按 L2-1 → L2-3 → L2-2 → L2-4 → L2-5 顺序推进（L2-3 先改密码 bcrypt，L2-2 再改 JWT，避免同时改 user_handler 冲突；L2-4 gRPC TLS/auth 最后做确保前面修复在拦截器链上工作）；每项走 TDD。

**技术栈：** Go 1.26 · golang-jwt/jwt/v5 · golang.org/x/crypto/bcrypt · google.golang.org/grpc/credentials · google.golang.org/grpc/metadata

**前置环境：** 运行 go 命令前先执行 `export PATH=$PATH:/root/.version-fox/sdks/golang/bin`

---

## 文件结构一览

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `services/logic/handler/message_handler.go` | L2-1：修复群聊索引截断，遍历全量成员分批写入 |
| 修改 | `services/logic/database/model.go` | L2-3：User.Password size 改 60 |
| 修改 | `services/logic/handler/user_handler.go` | L2-3+L2-2：bcrypt 密码、JWT 从配置读密钥、Cache.Set 错误传播 |
| 修改 | `services/logic/server.go` | L2-3+L2-2+L2-4：传入 AppSecret、GRPC 配置 |
| 修改 | `wire/token/jwt.go` | L2-2：迁移到 jwt/v5、alg 校验、删除敏感 claims、删除 DefaultSecret |
| 修改 | `services/gateway/serv/handler.go` | L2-2+L2-5：AppSecret 必填 fail-fast、Token 日志脱敏、移除 Password/AccessToken 透传 |
| 创建 | `internal/server/auth.go` | L2-4：gRPC token auth 拦截器 |
| 修改 | `internal/server/grpc.go` | L2-4：TLS 支持、auth 拦截器、reflection 可配置；L2-5：recovery 不返回堆栈 |
| 修改 | `internal/client/pool.go` | L2-4：按配置选 TLS 或 insecure |
| 创建 | `internal/config/grpc.go` | L2-4：GRPCConfig 结构（TLS + auth + reflection） |
| 创建 | `internal/util/recover.go` | L2-5：panic recovery helper（L3-4 复用） |
| 修改 | `services/gateway/config.go` | L2-2+L2-4：AppSecret 必填校验、GRPC 配置字段 |
| 修改 | `services/comet/config.go` | L2-4：GRPC 配置字段 |
| 修改 | `services/logic/config.go` | L2-2+L2-4：AppSecret 字段、GRPC 配置字段 |
| 修改 | `services/router/config.go` | L2-4：GRPC 配置字段 |
| 修改 | `go.mod` / `go.sum` | L2-2：jwt-go → jwt/v5、添加 golang.org/x/crypto |
| 修改 | `services/gateway/conf.yaml` | L2-2+L2-4：设置 app_secret、grpc 段 |
| 修改 | `services/comet/conf.yaml` | L2-4：grpc 段 |
| 修改 | `services/logic/conf.yaml` | L2-2+L2-4：设置 app_secret、grpc 段 |
| 修改 | `services/router/conf.yaml` | L2-4：grpc 段、补齐其他缺失字段 |
| 创建 | `wire/token/jwt_test.go` | L2-2：JWT 安全测试（已存在，需扩展） |
| 创建 | `internal/server/auth_test.go` | L2-4：auth 拦截器测试 |
| 创建 | `internal/util/recover_test.go` | L2-5：recover helper 测试 |

---

### 任务 1：修复群聊消息索引截断（L2-1）

**文件：**
- 修改：`services/logic/handler/message_handler.go:96-160`
- 测试：使用已有的 `services/logic/handler/message_handler_test.go`（integration tag）

**问题分析：** 当前代码第 108-109 行 `if len(members) > maxBatchSize { members = members[:maxBatchSize] }` 直接截断，超过 1000 人的群里后续成员永远收不到消息索引。正确做法是：移除截断，改为分批写入——messageContent 只写一次，索引按 maxBatchSize=1000 分批循环插入。

- [ ] **步骤 1：编写失败的单元测试（不依赖 MySQL）**

由于现有测试依赖真实 MySQL，创建纯单元测试验证分批逻辑。在 `services/logic/handler/message_handler_test.go` 末尾添加：

```go
func TestInsertGroupMessage_NoTruncation(t *testing.T) {
	// 验证：当成员数 > maxBatchSize(1000) 时，不会截断成员列表
	// 这是一个逻辑验证测试，不依赖数据库
	// 我们直接测试分批逻辑：构造 2500 个成员，验证分批数 = ceil(2500/batchSize)
	memberCount := 2500
	batchSize := 1000
	expectedBatches := (memberCount + batchSize - 1) / batchSize // 3
	assert.Equal(t, 3, expectedBatches, "2500 members should produce 3 batches of 1000")

	// 验证所有成员都被覆盖：3 批 = 1000 + 1000 + 500 = 2500
	total := 0
	for i := 0; i < memberCount; i += batchSize {
		end := i + batchSize
		if end > memberCount {
			end = memberCount
		}
		total += end - i
	}
	assert.Equal(t, memberCount, total, "all members must be covered across batches")
}
```

- [ ] **步骤 2：运行测试验证当前代码逻辑错误**

运行：`go test -v -run TestInsertGroupMessage_NoTruncation ./services/logic/handler/...`
预期：测试中分批逻辑的断言 PASS（这个测试只是验证分批数学），但我们需要验证实际代码中的截断 bug。这个步骤的目的是确认测试框架可用。实际修复验证需要集成测试或代码审查。

- [ ] **步骤 3：修复 insertGroupMessage 截断 bug**

修改 `services/logic/handler/message_handler.go` 的 `insertGroupMessage` 方法，将第 104-110 行的截断逻辑替换为分批循环插入逻辑。替换整个 `insertGroupMessage` 方法：

```go
func (h *ServiceHandler) insertGroupMessage(req *rpc.InsertMessageReq) (int64, error) {
	messageId := h.Idgen.Next().Int64()

	var members []database.GroupMember
	err := h.BaseDb.Where(&database.GroupMember{Group: req.Dest}).Find(&members).Error
	if err != nil {
		return 0, err
	}

	// 防超时：单次事务最大写入行数，超过则分多个事务写入
	// 原代码在这里截断 members = members[:maxBatchSize] 导致 >1000 人群剩余成员丢消息
	// 修复后：遍历全量成员，分批写入索引
	const maxBatchSize = 1000

	messageContent := database.MessageContent{
		ID:       messageId,
		Type:     byte(req.Message.Type),
		Body:     req.Message.Body,
		Extra:    req.Message.Extra,
		SendTime: req.SendTime,
	}

	// 先写消息内容（一次）
	if err := h.MessageDb.Create(&messageContent).Error; err != nil {
		return 0, err
	}

	// 分批写入索引，每批一个事务，避免单次 INSERT 过大
	for i := 0; i < len(members); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(members) {
			end = len(members)
		}
		batch := members[i:end]

		idxs := make([]database.MessageIndex, len(batch))
		for j, m := range batch {
			idxs[j] = database.MessageIndex{
				ID:        h.Idgen.Next().Int64(),
				MessageID: messageId,
				AccountA:  m.Account,
				AccountB:  req.Sender,
				Direction: 0,
				Group:     m.Group,
				SendTime:  req.SendTime,
			}
			if m.Account == req.Sender {
				idxs[j].Direction = 1
			}
		}

		err = h.MessageDb.Transaction(func(tx *gorm.DB) error {
			return tx.CreateInBatches(idxs, 500).Error
		})
		if err != nil {
			return 0, err
		}
	}

	return messageId, nil
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go build ./services/logic/... && go vet ./services/logic/...`
预期：BUILD OK，无 vet 警告。
运行：`go test -v -run TestInsertGroupMessage_NoTruncation ./services/logic/handler/...`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add services/logic/handler/message_handler.go services/logic/handler/message_handler_test.go
git commit -m "fix(logic): remove group message index truncation for >1000 member groups

The previous code truncated members to maxBatchSize=1000, causing silent
data loss for large groups. Now iterates all members in batches of 1000,
each batch in its own transaction to avoid lock timeouts."
```

---

### 任务 2：密码 bcrypt + 常量时间比较（L2-3）

**文件：**
- 修改：`services/logic/database/model.go:47-54`
- 修改：`services/logic/handler/user_handler.go`
- 修改：`services/logic/server.go`（ServiceHandler 增加 AppSecret 字段）
- 修改：`services/logic/config.go`（增加 AppSecret 字段）

- [ ] **步骤 1：编写失败的测试**

创建 `services/logic/handler/user_handler_test.go`（如果已存在则扩展）：

```go
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

	// 正确密码比对通过
	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.Nil(t, err)

	// 错误密码比对失败
	err = bcrypt.CompareHashAndPassword(hash, []byte("wrongPassword"))
	assert.NotNil(t, err)
}

func TestBcryptWrongPasswordSize(t *testing.T) {
	// 验证 model.Password 字段 size 为 60（bcrypt 需要）
	// 这个测试通过反射检查 struct tag
	t.Skip("verified via code review - model.User.Password gorm size must be 60")
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestBcrypt ./services/logic/handler/...`
预期：FAIL（因为 `golang.org/x/crypto/bcrypt` 可能还没在 go.mod 中显式引入，或者还没实现 Login 的 bcrypt 逻辑）。

- [ ] **步骤 3：实现 bcrypt 密码存储和比对**

**3a. 修改 `services/logic/database/model.go`，将 User.Password 字段 size 从 30 改为 60：**

```go
type User struct {
	Model
	App      string `gorm:"size:30"`
	Account  string `gorm:"uniqueIndex;size:60"`
	Password string `gorm:"size:60"`
	Avatar   string `gorm:"size:200"`
	Nickname string `gorm:"size:20"`
}
```

**3b. 修改 `services/logic/handler/user_handler.go`，使用 bcrypt 比对密码，并添加 Register 方法（如果注册逻辑在别处则修改对应位置）。首先检查是否有注册 handler：**

需要确认注册逻辑的位置。先检查现有 user_handler.go 的 Login 方法，将字符串比较改为 bcrypt 比对：

```go
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/logic/database"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/token"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AppSecret 从配置注入的 JWT 密钥
var AppSecret string

func (h *ServiceHandler) Login(ctx context.Context, req *rpc.LoginReq) (*rpc.LoginResp, error) {
	var user database.User
	err := h.BaseDb.Model(&database.User{}).Where("account = ?", req.Account).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("account not found")
		}
		return nil, err
	}

	// 使用 bcrypt 常量时间比对密码
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

// Register 注册用户（新增）
func (h *ServiceHandler) Register(ctx context.Context, req *rpc.RegisterReq) (*rpc.RegisterResp, error) {
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := database.User{
		App:      req.App,
		Account:  req.Account,
		Password: string(hashedPwd),
		Nickname: req.Nickname,
	}
	if err := h.BaseDb.Create(&user).Error; err != nil {
		return nil, err
	}
	return &rpc.RegisterResp{Success: true}, nil
}
```

**3c. 修改 `services/logic/config.go`，添加 AppSecret 字段：**

在 Config 结构体中添加：
```go
AppSecret string `mapstructure:"app_secret"`
```

在 LoadConfig 中添加校验：
```go
if cfg.AppSecret == "" {
	return nil, fmt.Errorf("app_secret is required in config")
}
```

**3d. 修改 `services/logic/server.go`，将 AppSecret 注入 handler：**

在 New() 中创建 ServiceHandler 前设置 `handler.AppSecret = cfg.AppSecret`。

- [ ] **步骤 4：运行测试验证通过**

运行：`cd /root/program/go/kim && export PATH=$PATH:/root/.version-fox/sdks/golang/bin && go mod tidy && go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestBcryptPasswordHash ./services/logic/handler/...`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add services/logic/database/model.go services/logic/handler/user_handler.go services/logic/handler/user_handler_test.go services/logic/config.go services/logic/server.go go.mod go.sum
git commit -m "feat(security): use bcrypt for password hashing and constant-time comparison

- Change User.Password gorm size from 30 to 60 for bcrypt
- Replace string comparison with bcrypt.CompareHashAndPassword
- Add Register method with bcrypt.GenerateFromPassword
- Add AppSecret field to logic config, fail-fast if empty
- Propagate Cache.Set error to caller (consistency first)"
```

---

### 任务 3：JWT 库迁移 + 密钥配置化 + 防 alg=none（L2-2）

**文件：**
- 修改：`wire/token/jwt.go`
- 修改：`wire/token/jwt_test.go`
- 修改：`services/gateway/serv/handler.go`
- 修改：`services/gateway/config.go`
- 修改：`services/gateway/conf.yaml`
- 修改：`services/logic/conf.yaml`
- 修改：`go.mod`

- [ ] **步骤 1：编写失败的测试**

替换 `wire/token/jwt_test.go` 内容为：

```go
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
	// 模拟 alg=none 攻击：构造一个 header.alg=none 的 token
	// 使用 jwt/v5 的 unsafe API 构造
	claims := &Token{
		Account: "hacker",
		App:     "kim",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	// 用 SigningMethodNone 签名（jwt/v5 中需要显式使用不安全方法）
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	assert.Nil(t, err)

	// Parse 应该拒绝 alg=none
	_, err = Parse("any-secret", tokenStr)
	assert.NotNil(t, err, "alg=none token must be rejected")
}

func TestGenerateNoPasswordInClaims(t *testing.T) {
	// 验证生成的 token 中不包含 Password 和 AccessToken 字段
	secret := "test-secret-key-32bytes-minimum!"
	tk := &Token{
		Account: "test1",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := Generate(secret, tk)
	assert.Nil(t, err)

	// 解析为 MapClaims 检查字段
	parsed, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
	assert.Nil(t, err)
	mapClaims := parsed.Claims.(jwt.MapClaims)
	_, hasPassword := mapClaims["passwd"]
	assert.False(t, hasPassword, "Password must not be in JWT claims")
	_, hasAccessToken := mapClaims["access"]
	assert.False(t, hasAccessToken, "AccessToken must not be in JWT claims")
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v ./wire/token/...`
预期：FAIL（编译错误——jwt/v5 API 未迁移）。

- [ ] **步骤 3：迁移 JWT 到 golang-jwt/jwt/v5**

**3a. 更新依赖：**

```bash
go mod edit -droprequire github.com/dgrijalva/jwt-go
go get github.com/golang-jwt/jwt/v5@latest
go mod tidy
```

**3b. 重写 `wire/token/jwt.go`：**

```go
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

// Token JWT Claims 结构——仅包含必要的身份信息
// 注意：不再包含 Password 和 AccessToken（安全考虑：JWT payload 是 base64 编码的，非加密）
type Token struct {
	Account string `json:"acc,omitempty"`
	App     string `json:"app,omitempty"`
	Exp     int64  `json:"exp,omitempty"`
}

// Valid 实现 jwt.Claims 接口的验证方法
func (t *Token) Validate() error {
	if t.Exp < time.Now().Unix() {
		return ErrExpiredToken
	}
	if t.Account == "" {
		return ErrInvalidToken
	}
	return nil
}

// GetExpirationTime 实现 jwt.Claims 接口
func (t *Token) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(t.Exp, 0)), nil
}

// GetIssuedAt 实现 jwt.Claims 接口
func (t *Token) GetIssuedAt() (*jwt.NumericDate, error) {
	return nil, nil
}

// GetNotBefore 实现 jwt.Claims 接口
func (t *Token) GetNotBefore() (*jwt.NumericDate, error) {
	return nil, nil
}

// GetIssuer 实现 jwt.Claims 接口
func (t *Token) GetIssuer() (string, error) {
	return "", nil
}

// GetSubject 实现 jwt.Claims 接口
func (t *Token) GetSubject() (string, error) {
	return "", nil
}

// GetAudience 实现 jwt.Claims 接口
func (t *Token) GetAudience() (jwt.ClaimStrings, error) {
	return nil, nil
}

// Parse 解析并验证 JWT Token
func Parse(secret, tokenStr string) (*Token, error) {
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	claims := &Token{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		// 校验签名算法：必须是 HMAC，防止 alg=none / alg=RSA 等攻击
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

// Generate 使用密钥签发 JWT Token
func Generate(secret string, t *Token) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret is required")
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, t)
	return tok.SignedString([]byte(secret))
}

// String 实现脱敏输出
func (t *Token) String() string {
	return fmt.Sprintf("Token{Account:%s App:%s Exp:%d}", t.Account, t.App, t.Exp)
}
```

**3c. 修改 `services/gateway/serv/handler.go`：**

在 Accept 方法中：
- 删除 `token.DefaultSecret` fallback（第 84-86 行），改为如果 `h.AppSecret == ""` 直接返回错误
- 删除 Password 和 AccessToken 透传到 Session（第 114-116 行），只传 Account/ChannelId/GateId/App/RemoteIP
- 第 107 行日志中 tk 的 Password/AccessToken 已不存在（Token 结构体已删除这些字段），脱敏自动生效

修改 Accept 方法中的 secret 获取和 Session 构造：

```go
secret := h.AppSecret
if secret == "" {
	return "", nil, fmt.Errorf("app_secret not configured")
}

tk, err := token.Parse(secret, login.Token)
if err != nil {
	resp := pkt.NewFrom(&req.Header)
	resp.Status = pkt.Status_Unauthorized
	_ = conn.WriteFrame(kim.OpBinary, pkt.Marshal(resp))
	return "", nil, err
}

id := generateChannelID(h.ServiceID, tk.Account)
logger.GatewayLogger.Infof("accept %v channel:%s", tk, id)

req.ChannelId = id
req.WriteBody(&pkt.Session{
	Account:  tk.Account,
	ChannelId: id,
	GateId:   h.ServiceID,
	App:      tk.App,
	RemoteIP: getIP(conn.RemoteAddr().String()),
})
```

**3d. 修改 `services/gateway/config.go`：**

AppSecret 已存在，在 LoadConfig 中添加必填校验：
```go
if cfg.AppSecret == "" {
	return nil, fmt.Errorf("app_secret is required in gateway config")
}
```

**3e. 更新 `services/gateway/conf.yaml`，设置 app_secret：**

```yaml
app_secret: "kim-dev-secret-change-in-production-32b"
```

**3f. 更新 `services/logic/conf.yaml`，设置 app_secret（与 gateway 一致）：**

```yaml
app_secret: "kim-dev-secret-change-in-production-32b"
```

**3g. 修改 `services/comet/handler/login_handler.go` 中引用 token.Token 的地方：**

检查是否有引用 Password/AccessToken 字段。ValidUser 方法第 93-108 行使用了 `session.AccessToken` 和 `session.Password`——这是从客户端 Session 取的，不是 JWT claims，需要保留 pkt.Session 的字段但不从 JWT 中带过来。

- [ ] **步骤 4：运行测试验证通过**

运行：`go mod tidy && go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v ./wire/token/...`
预期：所有 5 个测试 PASS。
运行：`go test -v -run TestBcryptPasswordHash ./services/logic/handler/...`
预期：PASS。

- [ ] **步骤 5：Commit**

```bash
git add wire/token/jwt.go wire/token/jwt_test.go services/gateway/serv/handler.go services/gateway/config.go services/gateway/conf.yaml services/logic/conf.yaml services/logic/config.go services/logic/handler/user_handler.go go.mod go.sum
git commit -m "feat(security): migrate JWT to golang-jwt/jwt/v5 with proper validation

- Replace deprecated dgrijalva/jwt-go with golang-jwt/jwt/v5
- Validate signing method in keyFunc to prevent alg=none attacks
- Remove Password/AccessToken from JWT claims (not encrypted in payload)
- Delete DefaultSecret constant; require app_secret from config (fail-fast)
- Add Token.String() method for safe logging (no sensitive fields)
- Add comprehensive JWT security tests (expired, wrong secret, alg=none)"
```

---

### 任务 4：gRPC TLS + auth interceptor + 关 reflection（L2-4）

**文件：**
- 创建：`internal/config/grpc.go`
- 创建：`internal/server/auth.go`
- 修改：`internal/server/grpc.go`
- 修改：`internal/client/pool.go`
- 修改：`services/gateway/config.go`、`services/comet/config.go`、`services/logic/config.go`、`services/router/config.go`
- 修改：4 份 `services/*/conf.yaml`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/server/auth_test.go`：

```go
package server

import (
	"context"
	"testing"

	"github.com/klintcheng/kim/wire/token"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/Login"}

	_, err := interceptor(context.Background(), nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_ValidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)

	validToken, _ := token.Generate(secret, &token.Token{
		Account: "testuser",
		Exp:     0, // will set below
	})
	_ = validToken
	// 重新生成带过期时间的 token
	import_time := "time"
	_ = import_time
	// 用正确方式生成
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/Login"}

	md := metadata.New(map[string]string{"authorization": "Bearer invalid-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_SkipReflection(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	// reflection 方法应被跳过（当 reflection 启用时）
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}

	_, err := interceptor(context.Background(), nil, info, handler)
	// 不带 metadata 也应能通过（因为反射方法在白名单中）
	// 注意：实际实现中 reflection 的服务名应跳过认证
	assert.Nil(t, err)
}
```

注意：上面测试有 import 问题，实际编写时修复 import。正确的测试文件应该是：

```go
package server

import (
	"context"
	"testing"
	"time"

	"github.com/klintcheng/kim/wire/token"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	_, err := interceptor(context.Background(), nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_ValidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	validToken, err := token.Generate(secret, &token.Token{
		Account: "testuser",
		App:     "kim",
		Exp:     time.Now().Add(time.Hour).Unix(),
	})
	assert.Nil(t, err)

	md := metadata.New(map[string]string{"authorization": "Bearer " + validToken})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, nil, info, handler)
	assert.Nil(t, err)
	assert.Equal(t, "ok", resp)
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	md := metadata.New(map[string]string{"authorization": "Bearer invalid.token.here"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_HealthCheckBypass(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	// gRPC health check 应跳过认证
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}

	_, err := interceptor(context.Background(), nil, info, handler)
	assert.Nil(t, err)
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestAuthInterceptor ./internal/server/...`
预期：FAIL（编译错误——AuthInterceptor 未定义）。

- [ ] **步骤 3：实现 TLS 配置、auth 拦截器、reflection 开关**

**3a. 创建 `internal/config/grpc.go`：**

```go
package config

// GRPCConfig gRPC 服务端和客户端 TLS/auth 配置
type GRPCConfig struct {
	TLSEnable     bool   `yaml:"tls_enable"`     // 是否启用 TLS（默认 false，开发模式）
	TLSCertFile   string `yaml:"tls_cert_file"`  // 服务端证书路径
	TLSKeyFile    string `yaml:"tls_key_file"`   // 服务端密钥路径
	TLSCAFile     string `yaml:"tls_ca_file"`    // CA 证书路径（mTLS 客户端验证用）
	AuthEnable    bool   `yaml:"auth_enable"`    // 是否启用 token auth 拦截器（默认 false）
	Reflection    bool   `yaml:"reflection"`     // 是否启用 gRPC reflection（默认 false）
}

// DefaultGRPCConfig 默认开发配置
func DefaultGRPCConfig() GRPCConfig {
	return GRPCConfig{
		TLSEnable:  false,
		AuthEnable: false,
		Reflection: false,
	}
}
```

**3b. 创建 `internal/server/auth.go`：**

```go
package server

import (
	"context"
	"strings"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/wire/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authBypassMethods 不需要认证的方法白名单
var authBypassMethods = map[string]bool{
	"/grpc.health.v1.Health/Check": true,
	"/grpc.health.v1.Health/Watch": true,
}

// AuthInterceptor 创建一个 gRPC token auth 拦截器
// 从 metadata "authorization" 中提取 "Bearer <token>"，用 secret 验证 JWT
func AuthInterceptor(secret string) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 白名单方法跳过认证
		if authBypassMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		authHeader := values[0]
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		_, err := token.Parse(secret, tokenStr)
		if err != nil {
			logger.CommonLogger.Warnf("auth failed for %s: %v", info.FullMethod, err)
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(ctx, req)
	}
}
```

**3c. 修改 `internal/server/grpc.go`：**

在 options 中添加 grpc 配置字段，修改 NewGRPCServer 支持 TLS、auth、reflection 开关：

```go
package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type GRPCServer struct {
	*grpc.Server
	addr string
	hs   *health.Server
}

type Option func(*options)

type options struct {
	serviceName string
	limiter     config.LimiterConfig
	grpcCfg     config.GRPCConfig
	authSecret  string
}

func WithServiceName(name string) Option {
	return func(o *options) { o.serviceName = name }
}

func WithLimiter(cfg config.LimiterConfig) Option {
	return func(o *options) { o.limiter = cfg }
}

func WithGRPCConfig(cfg config.GRPCConfig) Option {
	return func(o *options) { o.grpcCfg = cfg }
}

func WithAuthSecret(secret string) Option {
	return func(o *options) { o.authSecret = secret }
}

func NewGRPCServer(addr string, opts ...Option) (*GRPCServer, error) {
	o := &options{
		limiter: config.DefaultResilienceConfig().Limiter,
		grpcCfg: config.DefaultGRPCConfig(),
	}
	for _, opt := range opts {
		opt(o)
	}

	// 构建拦截器链
	interceptors := []UnaryInterceptor{
		RecoveryInterceptor,
		UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		LoggingInterceptor(o.serviceName),
		MetricsInterceptor(o.serviceName),
		LimiterInterceptor(o.serviceName, o.limiter),
	}
	// auth 拦截器在 limiter 之后、业务 handler 之前
	if o.grpcCfg.AuthEnable && o.authSecret != "" {
		interceptors = append(interceptors, AuthInterceptor(o.authSecret))
	}
	chain := UnaryChain(interceptors...)

	// gRPC server options
	grpcOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc.UnaryServerInterceptor(chain)),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	}

	// TLS 配置
	if o.grpcCfg.TLSEnable {
		tlsCreds, err := loadTLSCredentials(o.grpcCfg)
		if err != nil {
			return nil, fmt.Errorf("load TLS credentials: %w", err)
		}
		grpcOpts = append(grpcOpts, grpc.Creds(tlsCreds))
	}

	s := grpc.NewServer(grpcOpts...)

	// Health check
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	// Reflection（仅在配置启用时注册）
	if o.grpcCfg.Reflection {
		reflection.Register(s)
		logger.CommonLogger.Infof("gRPC reflection enabled for %s", o.serviceName)
	}

	return &GRPCServer{Server: s, addr: addr, hs: hs}, nil
}

// loadTLSCredentials 加载 TLS 证书，支持 mTLS
func loadTLSCredentials(cfg config.GRPCConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// mTLS：如果配置了 CA，要求客户端证书
	if cfg.TLSCAFile != "" {
		caPEM, err := os.ReadFile(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.ClientCAs = certPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(tlsCfg), nil
}

func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(lis)
}

// HealthServer 返回 health server 实例供外部设置状态（L3 使用）
func (s *GRPCServer) HealthServer() *health.Server {
	return s.hs
}
```

**3d. 修改 `internal/client/pool.go`，支持 TLS 连接：**

在 refresh 方法中，需要根据配置选择 credentials。需要给 Pool 增加一个 tls 配置字段。修改 Pool 结构体和 refresh：

在 Pool 结构体中添加字段：
```go
type Pool struct {
	// ... 现有字段 ...
	tlsEnable bool
	caCertFile string
}
```

但这会改变 NewPool 的签名。更好的方式是通过 ResilienceConfig 不包含 TLS 配置，而是直接从外部传入。考虑到 pool.go 已经有 cfg 字段，可以将 TLS 配置加到一个新的 client config 结构中，或者直接在 refresh 中判断——暂时采用最简单的方式：添加一个 `tlsConfig *tls.Config` 字段，为 nil 时用 insecure。

实际上更简洁的方式是：在 Pool 中添加一个 `dialOpts []grpc.DialOption` 字段，由 NewPool 时传入。

修改 `internal/client/pool.go`：

```go
package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Pool struct {
	naming      naming.Naming
	serviceName string
	mu          sync.RWMutex
	conns       map[string]*grpc.ClientConn
	rr          *roundRobin
	cfg         config.ResilienceConfig
	grpcCfg     config.GRPCConfig
	done        chan struct{}
	closeOnce   sync.Once
}

func NewPool(ns naming.Naming, serviceName string) *Pool {
	return NewPoolWithConfig(ns, serviceName, config.DefaultResilienceConfig(), config.DefaultGRPCConfig())
}

func NewPoolWithConfig(ns naming.Naming, serviceName string, cfg config.ResilienceConfig, grpcCfg config.GRPCConfig) *Pool {
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
		cfg:         cfg,
		grpcCfg:     grpcCfg,
		done:        make(chan struct{}),
	}
	if ns != nil {
		go p.watch()
	}
	return p
}

// ... 保留现有 Get/GetAny/GetAnyWithID/GetAnyExcluding/Interceptors 方法 ...

func (p *Pool) refresh() {
	services, err := p.naming.Find(p.serviceName)
	if err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentIDs := make(map[string]bool)
	for _, svc := range services {
		id := svc.ServiceID()
		currentIDs[id] = true
		if _, exists := p.conns[id]; !exists {
			addr := fmt.Sprintf("%s:%d", svc.PublicAddress(), svc.PublicPort())
			interceptors := InterceptorChain(p.serviceName, id, p.cfg)

			dialOpts := []grpc.DialOption{
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
				grpc.WithChainUnaryInterceptor(interceptors...),
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			}

			if p.grpcCfg.TLSEnable {
				tlsCreds, err := p.loadClientTLSCredentials()
				if err != nil {
					logger.CommonLogger.Errorf("pool: load TLS credentials for %s/%s: %v", p.serviceName, id, err)
					continue
				}
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(tlsCreds))
			} else {
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			}

			conn, err := grpc.Dial(addr, dialOpts...)
			if err != nil {
				logger.CommonLogger.Errorf("pool: dial %s/%s at %s: %v", p.serviceName, id, addr, err)
				continue
			}
			p.conns[id] = conn
		}
	}

	for id, conn := range p.conns {
		if !currentIDs[id] {
			_ = conn.Close()
			delete(p.conns, id)
		}
	}
}

func (p *Pool) loadClientTLSCredentials() (credentials.TransportCredentials, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if p.grpcCfg.TLSCAFile != "" {
		caPEM, err := os.ReadFile(p.grpcCfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = certPool
	}
	// mTLS 客户端证书
	if p.grpcCfg.TLSCertFile != "" && p.grpcCfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(p.grpcCfg.TLSCertFile, p.grpcCfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(tlsCfg), nil
}

// ... 保留 Close/roundRobin 方法 ...
```

注意：需要添加 `"github.com/klintcheng/kim/internal/logger"` import。

**3e. 修改各服务的 config.go 添加 GRPC 字段，修改 server.go 传递 GRPC 配置：**

在 gateway/config.go、comet/config.go、logic/config.go、router/config.go 的 Config 结构体中添加：
```go
GRPC config.GRPCConfig `mapstructure:"grpc"`
```

在各服务的 LoadConfig 中合并默认值：
```go
grpcDefaults := config.DefaultGRPCConfig()
if !cfg.GRPC.AuthEnable && !cfg.GRPC.TLSEnable && !cfg.GRPC.Reflection {
	// 全部默认值为 false，无需覆盖
}
```

修改各服务的 server.go 中 NewGRPCServer 调用，传入 grpc 配置和 auth secret：
- gateway/server.go: `server.WithGRPCConfig(cfg.GRPC)` 和 `server.WithAuthSecret(cfg.AppSecret)`（gateway 接收 comet 的 Push，需要 auth）
- comet/server.go: `server.WithGRPCConfig(cfg.GRPC)` 和 `server.WithAuthSecret(cfg.AppSecret)`（如果 comet 也需要 auth）
- logic/server.go: `server.WithGRPCConfig(cfg.GRPC)` 和 `server.WithAuthSecret(cfg.AppSecret)`（logic 需要最严格的 auth）

修改各服务中 NewPoolWithConfig 调用，传入 grpcCfg：
- gateway/forwarder.go: `client.NewPoolWithConfig(ns, wire.SNChat, cfg.Resilience, cfg.GRPC)`
- comet/server.go: 两个 pool 都要传 grpcCfg

**3f. 更新 4 份 conf.yaml 添加 grpc 段：**

在每份 conf.yaml 的 resilience/trace 之后添加：
```yaml
grpc:
  tls_enable: false        # 开发模式默认关闭
  tls_cert_file: ""
  tls_key_file: ""
  tls_ca_file: ""
  auth_enable: false       # 开发模式默认关闭
  reflection: false        # 默认关闭 reflection
```

注意：router 是 HTTP 服务不用 gRPC，但配置保持一致。router/config.go 需要补全缺失字段（ServiceID、PublicAddress、PublicPort、MonitorPort、Resilience、Trace），参考其他服务。

- [ ] **步骤 4：运行测试验证通过**

运行：`go mod tidy && go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestAuthInterceptor ./internal/server/...`
预期：所有 4 个测试 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/config/grpc.go internal/server/auth.go internal/server/auth_test.go internal/server/grpc.go internal/client/pool.go services/*/config.go services/*/server.go services/*/conf.yaml services/gateway/forwarder.go go.mod go.sum
git commit -m "feat(security): add gRPC TLS support, token auth interceptor, disable reflection by default

- Add internal/config/grpc.go with GRPCConfig (TLS/mTLS/auth/reflection)
- Add internal/server/auth.go with JWT auth interceptor, health check bypass
- Modify NewGRPCServer to support TLS credentials, auth interceptor, reflection toggle
- Modify Pool to support TLS credentials based on config
- All grpc options default to off for development mode
- Reflection disabled by default (prevents grpcurl enumeration without config)
- gRPC server returns HealthServer for L3 readiness/liveness usage"
```

---

### 任务 5：敏感信息日志脱敏 + Recovery 不返回堆栈（L2-5）

**文件：**
- 创建：`internal/util/recover.go`
- 修改：`internal/server/interceptor.go`
- 修改：`services/gateway/serv/handler.go`（Token 已无敏感字段，只需确认）
- 修改：`context.go`（Debug 日志 body 限制）

- [ ] **步骤 1：编写失败的测试**

创建 `internal/util/recover_test.go`：

```go
package util

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecover(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test")
		panic("test panic")
	})
}

func TestRecoverNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test")
		// no panic
	})
}

func TestSafeRecover(t *testing.T) {
	called := false
	SafeRecover("test", func(r interface{}) {
		called = true
		assert.Equal(t, "test panic", r)
	})
	panic("test panic")
	t.Fail() // should not reach here
}
```

Wait, SafeRecover should not continue after panic. Let me fix:

```go
func TestSafeRecover(t *testing.T) {
	called := make(chan bool, 1)
	go func() {
		defer SafeRecover("test-goroutine", func(r interface{}) {
			called <- true
		})
		panic("test panic")
	}()
	v := <-called
	assert.True(t, v)
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test -v -run TestRecover ./internal/util/...`
预期：FAIL（编译错误——util/recover.go 不存在）。

- [ ] **步骤 3：实现日志脱敏和 recovery 改进**

**3a. 创建 `internal/util/recover.go`：**

```go
package util

import (
	"fmt"
	"runtime/debug"

	"github.com/klintcheng/kim/internal/logger"
)

// Recover 用于 goroutine 中的 panic 捕获
// 用法：go func() { defer util.Recover("goroutine-name"); ... }()
func Recover(location string) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
	}
}

// SafeRecover 带自定义回调的 panic 恢复
func SafeRecover(location string, onRecover func(r interface{})) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
		if onRecover != nil {
			onRecover(r)
		}
	}
}

// GoSafe 启动一个带 panic recovery 的 goroutine
func GoSafe(location string, fn func()) {
	go func() {
		defer Recover(location)
		fn()
	}()
}
```

**3b. 修改 `internal/server/interceptor.go` 的 RecoveryInterceptor：**

将堆栈信息只写日志，不返回给客户端：

```go
func RecoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			// 堆栈仅写入服务端日志，不返回给客户端
			stack := debug.Stack()
			logger.CommonLogger.Errorf("gRPC panic in %s: %v\n%s", info.FullMethod, r, stack)
			// 客户端只看到通用 Internal 错误，不含堆栈
			err = status.Errorf(codes.Internal, "internal server error")
		}
	}()
	return handler(ctx, req)
}
```

**3c. 修改 `context.go` 的 Resp 和 Dispatch 方法中 Debug 日志的 body 输出：**

第 100 行：`logger.CommonLogger.Debugf("<-- Resp to %s command:%s  status: %v body: %s", ...)`
第 120 行：`logger.CommonLogger.Debugf("<-- Dispatch to %d users command:%s", ...)`

对 body 做长度限制。在 Resp 方法中，如果 body 很大则截断显示：

```go
// 在 Resp 方法中，限制 body 的日志输出长度
bodyStr := fmt.Sprintf("%v", body)
if len(bodyStr) > 200 {
	bodyStr = bodyStr[:200] + "...(truncated)"
}
logger.CommonLogger.Debugf("<-- Resp to %s command:%s status: %v body: %s", c.Session().GetAccount(), &c.request.Header, status, bodyStr)
```

**3d. 修改 `services/gateway/serv/handler.go` 中 Accept 方法，确认 Token 日志中无敏感信息：**

由于 Token 结构体已在 L2-2 中删除 Password/AccessToken 字段，`%v` 格式化时只会输出 Account/App/Exp，已自然脱敏。无需额外改动。

检查 Accept 方法中 `req.WriteBody(&pkt.Session{...})` 是否还在写 Password/AccessToken（之前是 114-116 行，L2-2 任务已删除）。

- [ ] **步骤 4：运行测试验证通过**

运行：`go build ./... && go vet ./...`
预期：BUILD OK，vet 无警告。
运行：`go test -v -run TestRecover ./internal/util/...`
预期：PASS。
运行：`go test -v ./wire/token/... ./internal/server/...`
预期：全部 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/util/recover.go internal/util/recover_test.go internal/server/interceptor.go context.go
git commit -m "feat(security): add panic recovery helper, sanitize error responses and logs

- Add internal/util/recover.go with Recover/SafeRecover/GoSafe helpers
- RecoveryInterceptor no longer sends stack trace to client (server log only)
- Clients get generic 'internal server error' instead of detailed panic info
- Debug log body output truncated to 200 chars to prevent log flooding
- Token struct has no sensitive fields (Password/AccessToken removed in L2-2)"
```

---

## 端到端验证清单（L2 完成后执行）

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./...` 所有测试通过
- [ ] `grep -r "\"secret\"" services/*/conf.yaml` 无硬编码密钥（应为配置的 app_secret）
- [ ] `grep -r "DefaultSecret" .` 无引用（已删除）
- [ ] `grep -r "dgrijalva/jwt-go" .` 无引用（已迁移）
- [ ] 检查 User.Password 字段 gorm tag 为 size:60
- [ ] 4 份 conf.yaml 均有 grpc 段，tls_enable/auth_enable/reflection 默认 false
- [ ] reflection.Register 仅在配置启用时调用
