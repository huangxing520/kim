// 文件：server.go
// 职责：核心接口定义——整个 KIM 系统的抽象层，定义 Server、Acceptor、MessageListener、StateListener、Agent、Conn、Channel 等基础接口/类型。
//
// 常量：
//   - DefaultReadWait / DefaultWriteWait / DefaultLoginWait / DefaultHeartbeat：默认超时和心跳参数
//   - DefaultMessageReadPool / DefaultConnectionPool：默认协程池大小
//
// 定义的类型：
//   - Service 接口：基础服务抽象（ServiceID / ServiceName / GetMeta）
//   - ServiceRegistration 接口：服务注册抽象（组合 Service + 地址/端口/标签/协议/命名空间）
//   - Server 接口：服务端核心接口（组合 ServiceRegistration + SetAcceptor / SetMessageListener / SetStateListener / Start / Push / Shutdown）
//   - Channel 接口：单个连接的抽象（ID / Push / Meta / Readloop / Close 等）
//   - Acceptor 接口：连接接收器（Accept 握手返回 channelID + Meta）
//   - MessageListener 接口：消息监听器（Receive 回调）
//   - StateListener 接口：状态监听器（Disconnect 回调）
//   - Agent 接口：客户端代理（ID / Push / GetMeta）
//   - Conn 接口：WebSocket/协议连接（组合 net.Conn + ReadFrame / WriteFrame / Flush）
//   - ChannelImpl 引用了 channel.go 中定义的 Channel 实现
//
// 方法（接口定义，实现在各处）：
//   - 详见各接口内部的注释签名

package kim

import (
	"context"
	"net"
	"time"
)

// ---------- 默认超时/心跳参数 ----------

const (
	DefaultReadWait  = time.Minute * 3
	DefaultWriteWait = time.Second * 10
	DefaultLoginWait = time.Second * 10
	DefaultHeartbeat = time.Second * 55
)

// ---------- 默认协程池参数 ----------

const (
	DefaultMessageReadPool = 5000 // 消息处理协程池默认大小
	DefaultConnectionPool  = 5000 // 连接处理协程池默认大小
)

// ---------- 核心接口定义 ----------

// Service 基础服务抽象接口
type Service interface {
	ServiceID() string
	ServiceName() string
	GetMeta() map[string]string
}

// ServiceRegistration 服务注册抽象接口（组合 Service + 注册信息）
type ServiceRegistration interface {
	Service
	PublicAddress() string
	PublicPort() int
	DialURL() string
	GetTags() []string
	GetProtocol() string
	GetNamespace() string
	String() string
}

// Server TCP/WebSocket 通用服务端接口
type Server interface {
	ServiceRegistration
	// SetAcceptor 设置Acceptor
	SetAcceptor(Acceptor)
	//SetMessageListener 设置上行消息监听器
	SetMessageListener(MessageListener)
	//SetStateListener 设置连接状态监听服务
	SetStateListener(StateListener)
	// SetReadWait 设置读超时
	SetReadWait(time.Duration)
	// SetChannelMap 设置Channel管理服务
	SetChannelMap(ChannelMap)

	// Start 用于在内部实现网络端口的监听和接收连接，
	// 并完成一个Channel的初始化过程。
	Start() error
	// Push 消息到指定的Channel中
	//  string channelID
	//  []byte 序列化之后的消息数据
	Push(string, []byte) error
	// Shutdown 服务下线，关闭连接
	Shutdown(context.Context) error
}

// Acceptor 连接接收器
type Acceptor interface {
	// Accept 返回一个握手完成的Channel对象或者一个error。
	// 业务层需要处理不同协议和网络环境下的连接握手协议
	Accept(Conn, time.Duration) (string, Meta, error)
}

// MessageListener 监听消息
type MessageListener interface {
	// 收到消息回调
	Receive(Agent, []byte)
}

// StateListener 状态监听器
type StateListener interface {
	// 连接断开回调
	Disconnect(string) error
}

type Meta map[string]string

// Agent is interface of client side
type Agent interface {
	ID() string
	Push([]byte) error
	GetMeta() Meta
}

// Conn Connection
type Conn interface {
	net.Conn
	ReadFrame() (Frame, error)
	WriteFrame(OpCode, []byte) error
	Flush() error
}

// Channel is interface of client side
type Channel interface {
	Conn
	Agent
	// Close 关闭连接
	Close() error
	Readloop(lst MessageListener) error
	// SetWriteWait 设置写超时
	SetWriteWait(time.Duration)
	SetReadWait(time.Duration)
}

// Client is interface of client side
type Client interface {
	Service
	// connect to server
	Connect(string) error
	// SetDialer 设置拨号处理器
	SetDialer(Dialer)
	Send([]byte) error
	Read() (Frame, error)
	// Close 关闭
	Close()
}

// Dialer Dialer
type Dialer interface {
	DialAndHandshake(DialerContext) (net.Conn, error)
}

type DialerContext struct {
	Id      string
	Name    string
	Address string
	Timeout time.Duration
}

// OpCode OpCode
type OpCode byte

// Opcode type
const (
	OpContinuation OpCode = 0x0
	OpText         OpCode = 0x1
	OpBinary       OpCode = 0x2
	OpClose        OpCode = 0x8
	OpPing         OpCode = 0x9
	OpPong         OpCode = 0xa
)

// Frame Frame
type Frame interface {
	SetOpCode(OpCode)
	GetOpCode() OpCode
	SetPayload([]byte)
	GetPayload() []byte
}
