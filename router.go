// 文件：router.go
// 职责：消息路由器——按 Command 将消息分发到对应的 HandlerFunc 链，支持中间件和对象池复用 Context。
//
// 定义的类型：
//   - Router 结构体：消息路由器，持有中间件链、FuncTree 命令树和 Context 对象池
//   - FuncTree 结构体：命令到 HandlerFunc 链的映射树，支持注册和查找
//
// 方法：
//   - NewRouter()                             → 创建一个新的路由器实例
//   - (Router).Use(handlers...)               → 注册全局中间件（对所有命令生效）
//   - (Router).Handle(command, handlers...)   → 注册指定命令的处理函数链（会自动追加中间件）
//   - (Router).Serve(packet, dispatcher, cache, session) → 处理一个消息包：从池中取 Context → 查找 handlers → 执行
//   - (Router).serveContext(ctx)              → 内部方法：按 command 查找 handlers 链并触发 Next
//   - (FuncTree).Add(path, handlers...)       → 向命令树中添加/追加处理函数链
//   - (FuncTree).Get(path)                    → 从命令树中查找处理函数链
//   - handleNoFound(ctx)                      → 默认处理函数：返回 NotImplemented 错误

package kim

import (
	"errors"
	"fmt"
	"sync"

	"github.com/klintcheng/kim/wire/pkt"
)

// ErrSessionLost Session 丢失错误
var ErrSessionLost = errors.New("err:session lost")

// Router 消息路由器，按 Command 分发到对应的 HandlerFunc 链
type Router struct {
	middlewares []HandlerFunc
	handlers    *FuncTree
	pool        sync.Pool
}

// NewRouter NewRouter
func NewRouter() *Router {
	r := &Router{
		handlers:    NewTree(),
		middlewares: make([]HandlerFunc, 0),
	}
	r.pool.New = func() interface{} {
		return BuildContext()
	}
	return r
}

func (r *Router) Use(handlers ...HandlerFunc) {
	r.middlewares = append(r.middlewares, handlers...)
}

// Handle register a command handler
func (r *Router) Handle(command string, handlers ...HandlerFunc) {
	r.handlers.Add(command, r.middlewares...)
	r.handlers.Add(command, handlers...)
}

// Serve a packet from client
func (r *Router) Serve(packet *pkt.LogicPkt, dispatcher Dispatcher, cache SessionStorage, session Session) error {
	if dispatcher == nil {
		return fmt.Errorf("dispatcher is nil")
	}
	if cache == nil {
		return fmt.Errorf("cache is nil")
	}
	ctx := r.pool.Get().(*ContextImpl)
	ctx.reset()
	ctx.request = packet
	ctx.Dispatcher = dispatcher
	ctx.SessionStorage = cache
	ctx.session = session

	r.serveContext(ctx)
	// Put Context to Pool
	r.pool.Put(ctx)
	return nil
}

func (r *Router) serveContext(ctx *ContextImpl) {
	chain, ok := r.handlers.Get(ctx.Header().Command)
	if !ok {
		ctx.handlers = []HandlerFunc{handleNoFound}
		ctx.Next()
		return
	}
	ctx.handlers = chain
	ctx.Next()
}

func handleNoFound(ctx Context) {
	_ = ctx.Resp(pkt.Status_NotImplemented, &pkt.ErrorResp{Message: "NotImplemented"})
}

// FuncTree is a tree structure
type FuncTree struct {
	nodes map[string]HandlersChain
}

// NewTree NewTree
func NewTree() *FuncTree {
	return &FuncTree{nodes: make(map[string]HandlersChain, 10)}
}

// Add a handler to tree
func (t *FuncTree) Add(path string, handlers ...HandlerFunc) {
	if t.nodes[path] == nil {
		t.nodes[path] = HandlersChain{}
	}

	t.nodes[path] = append(t.nodes[path], handlers...)
}

// Get a handler from tree
func (t *FuncTree) Get(path string) (HandlersChain, bool) {
	f, ok := t.nodes[path]
	return f, ok
}
