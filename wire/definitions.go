// 文件：definitions.go
// 职责：协议常量定义——消息命令字、Meta Key、服务名、协议类型、魔法数字、过期时间等全局常量。
//
// 常量分组：
//   - Algorithm*：路由算法（目前仅 hashslots）
//   - Command*：客户端与服务端之间的命令字（login.signin, chat.user.talk, chat.group.create 等）
//   - MetaDest*：消息包 Meta 中的标准 key（dest.server / dest.channels）
//   - Protocol*：通信协议类型常量（tcp / websocket）
//   - SN*：各服务的唯一服务名常量（wgateway / tgateway / chat / royal）
//   - Magic*：两种协议包的魔法数字（LogicPkt: 0xc311a365, BasicPkt: 0xc315a765）
//   - Offline* / Message* / AccessToken*：离线消息、分页、Token 过期等业务参数
//   - MessageType*：消息内容类型枚举（文本/图片/语音/视频）
//
// 类型：
//   - Protocol 类型（string 别名）
//   - ServiceID 类型（string 别名）
//   - SessionID 类型（string 别名）
//   - Magic 类型（[4]byte 别名）

package wire

import "time"

// ---------- 路由算法 ----------

const (
	AlgorithmHashSlots = "hashslots"
)

// ---------- 客户端命令字 ----------

const (
	// 登录
	CommandLoginSignIn  = "login.signin"
	CommandLoginSignOut = "login.signout"

	// 聊天
	CommandChatUserTalk  = "chat.user.talk"
	CommandChatGroupTalk = "chat.group.talk"
	CommandChatTalkAck   = "chat.talk.ack"

	// 离线消息
	CommandOfflineIndex   = "chat.offline.index"
	CommandOfflineContent = "chat.offline.content"

	// 群管理
	CommandGroupCreate  = "chat.group.create"
	CommandGroupJoin    = "chat.group.join"
	CommandGroupQuit    = "chat.group.quit"
	CommandGroupMembers = "chat.group.members"
	CommandGroupDetail  = "chat.group.detail"
)

// ---------- 消息包 Meta Key ----------

const (
	// MetaDestServer 消息目标网关的 ServiceName
	MetaDestServer = "dest.server"
	// MetaDestChannels 消息目标 Channel 列表
	MetaDestChannels = "dest.channels"
)

// Protocol Protocol
type Protocol string

// Protocol
const (
	ProtocolTCP       Protocol = "tcp"
	ProtocolWebsocket Protocol = "websocket"
)

// Service Name 定义统一的服务名
const (
	SNWGateway = "wgateway"
	SNTGateway = "tgateway"
	SNLogin    = "chat"  //login
	SNChat     = "chat"  //chat
	SNService  = "royal" //rpc service
)

// ServiceID ServiceID
type ServiceID string

// SessionID SessionID
type SessionID string

type Magic [4]byte

var (
	MagicLogicPkt = Magic{0xc3, 0x11, 0xa3, 0x65}
	MagicBasicPkt = Magic{0xc3, 0x15, 0xa7, 0x65}
)

const (
	OfflineReadIndexExpiresIn = time.Hour * 24 * 30 // 读索引在缓存中的过期时间
	OfflineSyncIndexCount     = 2000                //单次同步消息索引的数量
	OfflineMessageExpiresIn   = 15                  // 离线消息过期时间
	MessageMaxCountPerPage    = 200                 // 同步消息内容时每页的最大数据
	AccessTokenExpiresIn      = time.Hour * 24
)

const (
	MessageTypeText  = 1
	MessageTypeImage = 2
	MessageTypeVoice = 3
	MessageTypeVideo = 4
)
