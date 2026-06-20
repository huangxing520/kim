# Kim IM 项目 API 文档

本文档整理了 Kim IM 项目所有对外暴露的 API，按 **REST API（HTTP）** 和 **WebSocket/TCP API（自定义二进制协议）** 两大类区分。

---

## 目录

- [一、服务端口总览](#一服务端口总览)
- [二、REST API（HTTP）](#二rest-apihttp)
  - [2.1 Router 路由服务（:8100）](#21-router-路由服务8100)
  - [2.2 Service 数据服务（:8080）](#22-service-数据服务8080)
- [三、WebSocket/TCP API（自定义协议）](#三websockettcp-api自定义协议)
  - [3.1 协议格式](#31-协议格式)
  - [3.2 连接建立与登录](#32-连接建立与登录)
  - [3.3 业务消息命令列表](#33-业务消息命令列表)
- [四、数据结构定义](#四数据结构定义)
- [五、错误码](#五错误码)
- [六、典型调用流程](#六典型调用流程)

---

## 一、服务端口总览

| 服务 | 服务名 | 默认端口 | 协议 | 说明 |
|------|--------|----------|------|------|
| Router | router | :8100 | HTTP | IP 区域路由，返回 gateway 域名 |
| Service | royal | :8080 | HTTP | 数据服务，提供消息/群组/用户/离线消息管理 |
| Server | chat | :8005 | TCP（内部） | 业务逻辑服务，处理 WebSocket 命令（不直接对外） |
| Gateway | wgateway | :8000 | WebSocket / TCP | 客户端接入网关，对外暴露 |

> **客户端接入流程**：先调用 Router 的 HTTP API 获取 gateway 地址，再通过 WebSocket/TCP 连接 Gateway，使用自定义二进制协议进行通信。

---

## 二、REST API（HTTP）

REST API 由 **Router** 和 **Service** 两个服务提供，使用 JSON 或 Protobuf 格式（支持内容协商）。

### 2.1 Router 路由服务（:8100）

Router 服务提供 IP 区域路由功能，根据客户端 IP 和 token 返回最优的 Gateway 域名列表。

#### 健康检查

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查，返回 `ok` |

#### 路由查找

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/lookup/:token` | 根据客户端 IP 和 token 查找最优 Gateway 域名 |

**请求参数**：

| 参数 | 位置 | 类型 | 必填 | 说明 |
|------|------|------|------|------|
| token | path | string | 是 | 客户端生成的唯一标识（用于一致性哈希） |

**响应体**（`LookUpResp`）：

```json
{
  "utc": 1639785600,
  "location": "中国",
  "domains": ["ws://gateway1.example.com", "ws://gateway2.example.com", "ws://gateway3.example.com"]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| utc | int64 | 服务器当前 UTC 时间戳（秒） |
| location | string | 客户端 IP 所在国家/地区 |
| domains | string[] | 推荐的 Gateway 域名列表（最多 3 个） |

**逻辑说明**：
1. 根据 HTTP 请求的 IP 地址查询所属国家
2. 根据国家映射到区域（Region）
3. 在区域内通过 token 哈希选择 IDC（数据中心）
4. 从 IDC 中通过 token 哈希选择最多 3 个 Gateway
5. 返回 Gateway 的域名列表

---

### 2.2 Service 数据服务（:8080）

Service 服务提供消息存储、群组管理、用户管理、离线消息同步等 HTTP API。所有接口支持内容协商（JSON / Protobuf / MsgPack）。

#### 健康检查

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查，返回 `ok` |

#### 2.2.1 消息管理 API

路径前缀：`/api/:app/message`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/:app/message/user` | 保存单聊消息 |
| POST | `/api/:app/message/group` | 保存群聊消息 |
| POST | `/api/:app/message/ack` | 消息已读 ACK |

**POST `/api/:app/message/user`** - 保存单聊消息

请求体（`InsertMessageReq`）：

```json
{
  "sender": "user1",
  "dest": "user2",
  "send_time": 1639785600000000000,
  "message": {
    "type": 1,
    "body": "hello",
    "extra": ""
  }
}
```

响应体（`InsertMessageResp`）：

```json
{
  "message_id": 1234567890
}
```

**POST `/api/:app/message/group`** - 保存群聊消息

请求体同上，`dest` 为群组 ID。

**POST `/api/:app/message/ack`** - 消息已读 ACK

请求体（`AckMessageReq`）：

```json
{
  "account": "user1",
  "message_id": 1234567890
}
```

#### 2.2.2 群组管理 API

路径前缀：`/api/:app/group`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/:app/group` | 创建群组 |
| GET | `/api/:app/group/:id` | 获取群组详情 |
| POST | `/api/:app/group/member` | 加入群组 |
| DELETE | `/api/:app/group/member` | 退出群组 |
| GET | `/api/:app/group/members/:id` | 获取群成员列表 |

**POST `/api/:app/group`** - 创建群组

请求体（`CreateGroupReq`）：

```json
{
  "name": "群组名称",
  "avatar": "头像URL",
  "introduction": "群简介",
  "owner": "群主账号",
  "members": ["user1", "user2", "user3"]
}
```

响应体（`CreateGroupResp`）：

```json
{
  "group_id": "abc123"
}
```

**GET `/api/:app/group/:id`** - 获取群组详情

响应体（`GetGroupResp`）：

```json
{
  "id": "abc123",
  "name": "群组名称",
  "avatar": "头像URL",
  "introduction": "群简介",
  "owner": "群主账号",
  "created_at": 1639785600
}
```

**POST `/api/:app/group/member`** - 加入群组

请求体（`JoinGroupReq`）：

```json
{
  "account": "user1",
  "group_id": "abc123"
}
```

**DELETE `/api/:app/group/member`** - 退出群组

请求体（`QuitGroupReq`）：

```json
{
  "account": "user1",
  "group_id": "abc123"
}
```

**GET `/api/:app/group/members/:id`** - 获取群成员列表

响应体（`GroupMembersResp`）：

```json
{
  "users": [
    {
      "account": "user1",
      "alias": "用户1",
      "avatar": "头像URL",
      "join_time": 1639785600
    }
  ]
}
```

#### 2.2.3 用户管理 API

路径前缀：`/api/:app/user`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/:app/user/login` | 用户登录 |

**POST `/api/:app/user/login`** - 用户登录

请求体（`LoginReq`）：

```json
{
  "account": "user1",
  "password": "password123"
}
```

响应体（`LoginResp`）：

```json
{
  "AccessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

#### 2.2.4 离线消息 API

路径前缀：`/api/:app/offline`（启用 Gzip 压缩）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/:app/offline/index` | 同步离线消息索引 |
| POST | `/api/:app/offline/content` | 同步离线消息内容 |

**POST `/api/:app/offline/index`** - 同步离线消息索引

请求体（`GetOfflineMessageIndexReq`）：

```json
{
  "account": "user1",
  "message_id": 1234567890
}
```

> `message_id` 为上次同步的最后一条消息 ID，传 0 表示从上次 ACK 位置开始同步。

响应体（`GetOfflineMessageIndexResp`）：

```json
{
  "list": [
    {
      "message_id": 1234567891,
      "direction": 0,
      "send_time": 1639785600000000000,
      "accountB": "user2",
      "group": ""
    }
  ]
}
```

| 字段 | 说明 |
|------|------|
| direction | 0=接收的消息，1=发送的消息 |
| accountB | 消息对方账号（单聊时） |
| group | 群组 ID（群聊时） |

**POST `/api/:app/offline/content`** - 同步离线消息内容

请求体（`GetOfflineMessageContentReq`）：

```json
{
  "message_ids": [1234567891, 1234567892]
}
```

> 单次最多 200 个 message_id。

响应体（`GetOfflineMessageContentResp`）：

```json
{
  "list": [
    {
      "id": 1234567891,
      "type": 1,
      "body": "hello",
      "extra": ""
    }
  ]
}
```

---

## 三、WebSocket/TCP API（自定义协议）

客户端通过 WebSocket 或 TCP 连接 Gateway（:8000），使用自定义二进制协议通信。Gateway 将消息转发给 Server 服务处理。

### 3.1 协议格式

协议包分为两种：**LogicPkt（业务包）** 和 **BasicPkt（心跳包）**，通过 4 字节 Magic Number 区分。

#### Magic Number

| 类型 | Magic | 用途 |
|------|-------|------|
| LogicPkt | `0xc3 0x11 0xa3 0x65` | 业务消息 |
| BasicPkt | `0xc3 0x15 0xa7 0x65` | 心跳 Ping/Pong |

#### LogicPkt 结构

```
[Magic 4字节] [Header长度 2字节] [Header Protobuf] [Body长度 2字节] [Body Protobuf]
```

Header（Protobuf 编码）：

| 字段 | 类型 | 说明 |
|------|------|------|
| command | string | 命令名（见下表） |
| channelId | string | 发送方 Channel ID（服务端填充） |
| sequence | uint32 | 消息序列号（客户端生成，用于请求-响应匹配） |
| flag | enum | 0=Request, 1=Response, 2=Push |
| status | enum | 响应状态码（见错误码表） |
| dest | string | 目标（单聊为账号，群聊为群 ID） |
| meta | Meta[] | 元数据键值对 |

#### BasicPkt 结构

```
[Magic 4字节] [Code 2字节] [Length 2字节] [Body]
```

| Code | 说明 |
|------|------|
| 1 | Ping（客户端发送） |
| 2 | Pong（服务端响应） |

### 3.2 连接建立与登录

#### 步骤 1：建立连接

- **WebSocket**：连接 `ws://<gateway>:8000`
- **TCP**：连接 `<gateway>:8000`

#### 步骤 2：发送登录包

连接建立后，**必须首先发送登录包**，否则后续消息会被拒绝。

登录包为 LogicPkt，command 为 `login.signin`，Body 为 `LoginReq`。

**LoginReq**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| token | string | 是 | JWT Token（包含 account、app、exp 等信息） |
| isp | string | 否 | 运营商 |
| zone | string | 否 | 区域代码 |
| tags | string[] | 否 | 标签 |

**JWT Token 结构**（HS256 签名）：

```json
{
  "acc": "user1",        // 账号
  "app": "kim",          // 应用名
  "exp": 1639872000,     // 过期时间戳（秒）
  "passwd": "password",  // 密码（可选，密码登录时使用）
  "access": ""           // AccessToken（可选，Token 登录时使用）
}
```

**LoginResp**（登录成功响应）：

| 字段 | 类型 | 说明 |
|------|------|------|
| channelId | string | 服务端分配的全局唯一 Channel ID |
| account | string | 账号 |

#### 步骤 3：心跳保活

登录后需定期发送 BasicPkt Ping（Code=1），服务端会响应 Pong（Code=2）。默认心跳间隔 10 秒。

### 3.3 业务消息命令列表

以下命令通过 LogicPkt 发送，`command` 字段对应下表的命令名。

#### 登录管理

| 命令 | 方向 | 请求体 | 响应体 | 说明 |
|------|------|--------|--------|------|
| `login.signin` | C→S | `LoginReq` | `LoginResp` | 登录签到 |
| `login.signout` | C→S | 无 | 无 | 退出登录 |

#### 聊天消息

| 命令 | 方向 | 请求体 | 响应体 | 说明 |
|------|------|--------|--------|------|
| `chat.user.talk` | C→S | `MessageReq` | `MessageResp` | 发送单聊消息 |
| `chat.group.talk` | C→S | `MessageReq` | `MessageResp` | 发送群聊消息 |
| `chat.talk.ack` | C→S | `MessageAckReq` | 无 | 消息已读 ACK |
| `chat.user.talk` | S→C（Push） | - | `MessagePush` | 接收单聊消息推送 |
| `chat.group.talk` | S→C（Push） | - | `MessagePush` | 接收群聊消息推送 |

**MessageReq**（发送消息请求）：

| 字段 | 类型 | 说明 |
|------|------|------|
| type | int32 | 消息类型：1=文本, 2=图片, 3=语音, 4=视频 |
| body | string | 消息内容 |
| extra | string | 附加信息 |

> 单聊时 Header 的 `dest` 为接收方账号；群聊时 `dest` 为群组 ID。

**MessageResp**（发送消息响应）：

| 字段 | 类型 | 说明 |
|------|------|------|
| messageId | int64 | 消息 ID（雪花算法生成） |
| sendTime | int64 | 发送时间（纳秒时间戳） |

**MessagePush**（消息推送，服务端主动下发）：

| 字段 | 类型 | 说明 |
|------|------|------|
| messageId | int64 | 消息 ID |
| type | int32 | 消息类型 |
| body | string | 消息内容 |
| extra | string | 附加信息 |
| sender | string | 发送者账号 |
| sendTime | int64 | 发送时间（纳秒时间戳） |

**MessageAckReq**（消息已读 ACK）：

| 字段 | 类型 | 说明 |
|------|------|------|
| messageId | int64 | 已读消息 ID |

#### 离线消息同步

| 命令 | 方向 | 请求体 | 响应体 | 说明 |
|------|------|--------|--------|------|
| `chat.offline.index` | C→S | `MessageIndexReq` | `MessageIndexResp` | 同步离线消息索引 |
| `chat.offline.content` | C→S | `MessageContentReq` | `MessageContentResp` | 同步离线消息内容 |

**MessageIndexReq**：

| 字段 | 类型 | 说明 |
|------|------|------|
| message_id | int64 | 上次同步的最后一条消息 ID（0 表示从 ACK 位置开始） |

**MessageIndexResp**：

| 字段 | 类型 | 说明 |
|------|------|------|
| indexes | MessageIndex[] | 消息索引列表 |

**MessageIndex**：

| 字段 | 类型 | 说明 |
|------|------|------|
| message_id | int64 | 消息 ID |
| direction | int32 | 0=接收, 1=发送 |
| send_time | int64 | 发送时间（纳秒） |
| accountB | string | 对方账号（单聊） |
| group | string | 群组 ID（群聊） |

**MessageContentReq**：

| 字段 | 类型 | 说明 |
|------|------|------|
| message_ids | int64[] | 消息 ID 列表（最多 200 个） |

**MessageContentResp**：

| 字段 | 类型 | 说明 |
|------|------|------|
| contents | MessageContent[] | 消息内容列表 |

**MessageContent**：

| 字段 | 类型 | 说明 |
|------|------|------|
| messageId | int64 | 消息 ID |
| type | int32 | 消息类型 |
| body | string | 消息内容 |
| extra | string | 附加信息 |

#### 群组管理

| 命令 | 方向 | 请求体 | 响应体 | 说明 |
|------|------|--------|--------|------|
| `chat.group.create` | C→S | `GroupCreateReq` | `GroupCreateResp` | 创建群组 |
| `chat.group.join` | C→S | `GroupJoinReq` | 无 | 加入群组 |
| `chat.group.quit` | C→S | `GroupQuitReq` | 无 | 退出群组 |
| `chat.group.detail` | C→S | `GroupGetReq` | `GroupGetResp` | 获取群详情 |
| `chat.group.create` | S→C（Push） | - | `GroupCreateNotify` | 群创建通知 |

**GroupCreateReq**：

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 群名称 |
| avatar | string | 群头像 URL |
| introduction | string | 群简介 |
| owner | string | 群主账号 |
| members | string[] | 初始成员账号列表 |

**GroupCreateResp**：

| 字段 | 类型 | 说明 |
|------|------|------|
| group_id | string | 群组 ID（Base36 编码） |

**GroupCreateNotify**（推送通知）：

| 字段 | 类型 | 说明 |
|------|------|------|
| group_id | string | 群组 ID |
| members | string[] | 成员账号列表 |

**GroupJoinReq / GroupQuitReq**：

| 字段 | 类型 | 说明 |
|------|------|------|
| account | string | 账号 |
| group_id | string | 群组 ID |

**GroupGetReq**：

| 字段 | 类型 | 说明 |
|------|------|------|
| group_id | string | 群组 ID |

**GroupGetResp**：

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 群组 ID |
| name | string | 群名称 |
| avatar | string | 群头像 URL |
| introduction | string | 群简介 |
| owner | string | 群主账号 |
| members | Member[] | 成员列表 |
| created_at | int64 | 创建时间（秒） |

**Member**：

| 字段 | 类型 | 说明 |
|------|------|------|
| account | string | 账号 |
| alias | string | 群昵称 |
| avatar | string | 头像 URL |
| join_time | int64 | 加入时间（秒） |

#### 踢人通知

| 命令 | 方向 | 请求体 | 说明 |
|------|------|--------|------|
| `login.signin` | S→C（Push） | `KickoutNotify` | 账号在其他地方登录，被踢下线 |

**KickoutNotify**：

| 字段 | 类型 | 说明 |
|------|------|------|
| channelId | string | 被踢的 Channel ID |

---

## 四、数据结构定义

### 消息类型

| 值 | 类型 |
|----|------|
| 1 | 文本 |
| 2 | 图片 |
| 3 | 语音 |
| 4 | 视频 |

### 消息方向

| 值 | 说明 |
|----|------|
| 0 | 接收的消息 |
| 1 | 发送的消息 |

### Header Flag

| 值 | 说明 |
|----|------|
| 0 | Request（请求） |
| 1 | Response（响应） |
| 2 | Push（服务端推送） |

---

## 五、错误码

Status 枚举值（Protobuf 定义）：

| 状态码 | 名称 | 说明 |
|--------|------|------|
| 0 | Success | 成功 |
| 100 | NoDestination | 缺少目标 dest |
| 101 | InvalidPacketBody | 消息体无效 |
| 103 | InvalidCommand | 命令无效（如首包非登录包） |
| 105 | Unauthorized | 未授权（Token 无效） |
| 300 | SystemException | 系统异常 |
| 301 | NotImplemented | 功能未实现 |
| 404 | SessionNotFound | 会话不存在（Session 丢失） |

---

## 六、典型调用流程

### 6.1 客户端完整接入流程

```
客户端                        Router(:8100)              Gateway(:8000)              Server(:8005)         Service(:8080)
  │                              │                          │                          │                     │
  │  GET /api/lookup/{token}     │                          │                          │                     │
  ├─────────────────────────────►│                          │                          │                     │
  │  {domains: [ws://...]}       │                          │                          │                     │
  │◄─────────────────────────────┤                          │                          │                     │
  │                              │                          │                          │                     │
  │  WebSocket 连接              │                          │                          │                     │
  ├──────────────────────────────────────────────────────►│                          │                     │
  │                              │                          │                          │                     │
  │  LogicPkt: login.signin      │                          │                          │                     │
  ├──────────────────────────────────────────────────────►│  转发 login.signin        │                     │
  │                              │                          ├─────────────────────────►│  HTTP 验证用户      │
  │                              │                          │                          ├────────────────────►│
  │                              │                          │                          │  ◄───────────────────┤
  │                              │                          │  ◄────────────────────────┤                     │
  │  LogicPkt: LoginResp         │                          │                          │                     │
  │◄──────────────────────────────────────────────────────┤                          │                     │
  │                              │                          │                          │                     │
  │  BasicPkt: Ping              │                          │                          │                     │
  ├──────────────────────────────────────────────────────►│                          │                     │
  │  BasicPkt: Pong              │                          │                          │                     │
  │◄──────────────────────────────────────────────────────┤                          │                     │
```

### 6.2 单聊消息流程

```
发送方                        Gateway                     Server                      Service
  │                              │                          │                          │
  │  LogicPkt: chat.user.talk    │                          │                          │
  │  dest=接收方账号             │                          │                          │
  ├─────────────────────────────►│  转发 chat.user.talk     │                          │
  │                              ├─────────────────────────►│  POST /message/user      │
  │                              │                          ├─────────────────────────►│
  │                              │                          │  ◄───────────────────────┤
  │                              │                          │  查询接收方 Location      │
  │                              │                          │  Dispatch MessagePush    │
  │                              │  ◄────────────────────────┤  到接收方 Gateway        │
  │  LogicPkt: MessageResp       │                          │                          │
  │◄─────────────────────────────┤                          │                          │
  │                              │                          │                          │
  │  （接收方在线时）             │                          │                          │
  │  LogicPkt: MessagePush       │                          │                          │
  │  （推送到接收方）             │                          │                          │
```

### 6.3 群聊消息流程

```
发送方                        Gateway                     Server                      Service
  │                              │                          │                          │
  │  LogicPkt: chat.group.talk   │                          │                          │
  │  dest=群组ID                 │                          │                          │
  ├─────────────────────────────►│  转发 chat.group.talk    │                          │
  │                              ├─────────────────────────►│  POST /message/group     │
  │                              │                          ├─────────────────────────►│
  │                              │                          │  ◄───────────────────────┤
  │                              │                          │  GET /group/members/:id  │
  │                              │                          ├─────────────────────────►│
  │                              │                          │  ◄───────────────────────┤
  │                              │                          │  批量查询成员 Location    │
  │                              │                          │  Dispatch MessagePush    │
  │                              │  ◄────────────────────────┤  到所有在线成员          │
  │  LogicPkt: MessageResp       │                          │                          │
  │◄─────────────────────────────┤                          │                          │
```

### 6.4 离线消息同步流程

```
客户端                        Gateway                     Server                      Service
  │                              │                          │                          │
  │  1. LogicPkt: chat.offline.index                        │                          │
  │     message_id=上次同步ID   │                          │                          │
  ├─────────────────────────────►│─────────────────────────►│  POST /offline/index     │
  │                              │                          ├─────────────────────────►│
  │                              │                          │  ◄───────────────────────┤
  │  LogicPkt: MessageIndexResp  │◄─────────────────────────┤                          │
  │◄─────────────────────────────┤                          │                          │
  │                              │                          │                          │
  │  2. LogicPkt: chat.offline.content                      │                          │
  │     message_ids=[id1,id2...] │                          │                          │
  ├─────────────────────────────►│─────────────────────────►│  POST /offline/content   │
  │                              │                          ├─────────────────────────►│
  │                              │                          │  ◄───────────────────────┤
  │  LogicPkt: MessageContentResp│◄─────────────────────────┤                          │
  │◄─────────────────────────────┤                          │                          │
  │                              │                          │                          │
  │  3. LogicPkt: chat.talk.ack  │                          │                          │
  │     message_id=最后一条ID    │                          │                          │
  ├─────────────────────────────►│─────────────────────────►│  POST /message/ack       │
  │                              │                          ├─────────────────────────►│
```

---

## 附录：命令速查表

### WebSocket/TCP 命令一览

| 命令 | 类型 | 说明 |
|------|------|------|
| `login.signin` | 请求/响应 | 登录 |
| `login.signout` | 请求/响应 | 退出登录 |
| `chat.user.talk` | 请求/响应/Push | 单聊消息 |
| `chat.group.talk` | 请求/响应/Push | 群聊消息 |
| `chat.talk.ack` | 请求/响应 | 消息已读 ACK |
| `chat.offline.index` | 请求/响应 | 离线消息索引同步 |
| `chat.offline.content` | 请求/响应 | 离线消息内容同步 |
| `chat.group.create` | 请求/响应/Push | 创建群组 |
| `chat.group.join` | 请求/响应 | 加入群组 |
| `chat.group.quit` | 请求/响应 | 退出群组 |
| `chat.group.detail` | 请求/响应 | 获取群详情 |

### REST API 一览

| 方法 | 路径 | 服务 | 说明 |
|------|------|------|------|
| GET | `/health` | Router/Service | 健康检查 |
| GET | `/api/lookup/:token` | Router | 路由查找 |
| POST | `/api/:app/message/user` | Service | 保存单聊消息 |
| POST | `/api/:app/message/group` | Service | 保存群聊消息 |
| POST | `/api/:app/message/ack` | Service | 消息 ACK |
| POST | `/api/:app/group` | Service | 创建群组 |
| GET | `/api/:app/group/:id` | Service | 获取群详情 |
| POST | `/api/:app/group/member` | Service | 加入群组 |
| DELETE | `/api/:app/group/member` | Service | 退出群组 |
| GET | `/api/:app/group/members/:id` | Service | 获取群成员 |
| POST | `/api/:app/user/login` | Service | 用户登录 |
| POST | `/api/:app/offline/index` | Service | 离线消息索引 |
| POST | `/api/:app/offline/content` | Service | 离线消息内容 |
