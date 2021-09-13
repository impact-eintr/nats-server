package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Type of client connection
const (
	// CLIENT is an end user
	CLIENT = iota
	// ROUTER is another router in the cluster
	ROUTER
)

const (
	ClientProtoZero = iota
	ClientProtoInfo
)

// For controlling dynamic buffer sizes
const (
	startBufSize = 512
	minBufSize   = 128
	maxBufSize   = 65536
)

type subscription struct {
	client  *client
	subject []byte // 订阅主题
	queue   []byte // 可选的订阅组名
	sid     []byte // 客户端生成的唯一订阅ID
	nm      int64
	max     int64
}

type clientOpts struct {
	Verbose       bool   `json:"verbose"`      // 是否关闭服务器的+OK冗余信息，+OK见下面的说明
	Pedantic      bool   `json:"pedantic"`     // 是否打开严格校验
	SslRequired   bool   `json:"ssl_required"` // 是否需要SSL
	Authorization string `json:"auth_token"`   // 鉴权内容
	Username      string `json:"user"`         // 用户名
	Password      string `json:"pass"`         // 密码
	Name          string `json:"name"`         // 客户端名称
	Lang          string `json:"lang"`         // 客户端的实现语言
	Version       string `json:"version"`      // 客户端版本
	Protocol      int    `json:"protocol"`     // 协议版本
}

var defaultOpts = clientOpts{Verbose: true, Pedantic: true}

type client struct {
	stats
	mpay int64
	mu   sync.Mutex
	typ  int
	cid  uint64
	lang string

	opts  clientOpts
	start time.Time
	nc    net.Conn
	ncs   string
	bw    *bufio.Writer
	srv   *Server
	subs  map[string]*subscription
	perms *permissions
	cache readCache

	pcd  map[*client]struct{}
	atmr *time.Timer
	ptmr *time.Timer
	pout int
	wfc  int

	last time.Time
	// 这里client继承了协议解析状态机状态"parseState"。
	// 因此可以将这里的readloop想象成一个对流处理的处理器
	parseState

	route *route
	debug bool
	trace bool

	flags clientFlag // 将布尔值压缩到单个字段中。 需要时会增加尺寸
}

// 实际上就是一个保持了订阅主题和发布主题的列表
type permissions struct {
	sub    *Sublist
	pub    *Sublist
	pcache map[string]bool
}

// Represent(代表) client cooleans with bitmask
type clientFlag byte

const (
	connectReceived clientFlag = 1 << iota // The CONNECT proto has been received
	firstPongSent                          // The first PONG has been sent
	infoUpdated                            // The server's Info object has changed before first PONG was sent
)

// set the flag (would be equivalent to set the boolean to true)
func (cf *clientFlag) set(c clientFlag) {
	*cf |= c
}

// isSet returns true if the flag is set, false otherwise
func (cf clientFlag) isSet(c clientFlag) bool {
	return cf&c != 0
}

// unset the flag (would be equivalent to set the boolean to false)
func (cf *clientFlag) clear(c clientFlag) {
	*cf &= ^c
}

func (cf *clientFlag) setIfNotSet(c clientFlag) bool {
	if *cf&c == 0 {
		*cf |= c
		return true
	}
	return false
}

// Used in readloop to cache hot subject(主题) lookups and group statistics(统计值)
type readCache struct {
	genid   uint64
	results map[string]*SublistResult
	prand   *rand.Rand
	inMsgs  int
	inBytes int
	subs    int
}

func (c *client) readLoop() {
	// Grab the connection off the client, it will be cleared on a close.
	// We check for that after the loop, but want to avoid a nil dereference
	c.mu.Lock()
	nc := c.nc
	s := c.srv
	defer s.grWG.Done()
	c.mu.Unlock()

	if nc == nil {
		return
	}

	// Start read buffer
	b := make([]byte, startBufSize)

	// Snapshot server options
	opts := s.getOpts()

	// 这里，首先从连接中读取TCP流中的数据，
	// 然后调用client.parse 函数对读取到的内容做解析。这里解析其实也是包含了处理逻辑
	for {
		n, err := nc.Read(b)
		if err != nil {
			c.closeConnection()
			return
		}

		// Grab for updates for last activity
		last := time.Now()

		// Clear inbound(内部的) stats cache
		c.cache.inMsgs = 0
		c.cache.inBytes = 0
		c.cache.subs = 0

		if err := c.parse(b[:n]); err != nil {
			// handled inline
			if err != ErrMaxPayload && err != ErrAuthorization {
				c.Errorf("Error reading from client :%s", err.Error())
				c.sendErr("Parser Error")
				c.closeConnection()
			}
			return
		}
		// Updates stats for client and server that were collected from parsing through the buffer
		atomic.AddInt64(&c.inMsgs, int64(c.cache.inMsgs))
		atomic.AddInt64(&c.inBytes, int64(c.cache.inBytes))
		atomic.AddInt64(&s.inMsgs, int64(c.cache.inMsgs))
		atomic.AddInt64(&s.inBytes, int64(c.cache.inBytes))

		// Check pending clients for flush
		// 检查挂起的客户端是否刷新
		// 在处理发布消息的时候，就会调用 client.deliverMsg将其他的client挂在这个c.pcd里面：
		// 然后在每次loop里面，会将要处理的订阅消息发送给这里挂的其他订阅了的客户端。
		for cp := range c.pcd {
			// Flush those in the set
			cp.mu.Lock()
			if cp.nc != nil {
				// Gather the flush calls that happened before now.
				// This is a signal into us about dynamic buffer allocation tuning.
				wfc := cp.wfc
				cp.wfc = 0

				cp.nc.SetWriteDeadline(time.Now().Add(opts.WriteDeadline))
				err := cp.bw.Flush()
				cp.nc.SetWriteDeadline(time.Time{})
				if err != nil {
					c.Debugf("Error flushing: %v", err)
					cp.mu.Unlock()
					cp.closeConnection()
					cp.mu.Lock()
				} else {
					// Update outbound last activity.
					cp.last = last
					// Check if we should tune(调整) the buffer.
					sz := cp.bw.Available()
					// Check for expansion(膨胀) opportunity(机会).
					if wfc > 2 && sz <= maxBufSize/2 {
						cp.bw = bufio.NewWriterSize(cp.nc, sz*2)
					}
					// Check for shrinking(收缩) opportunity.
					if wfc == 0 && sz >= minBufSize*2 {
						cp.bw = bufio.NewWriterSize(cp.nc, sz/2)
					}
				}
			}
			cp.mu.Unlock()
			delete(c.pcd, cp)
		}
		// Check ti see if we got closed, e.g. slow consumer
		// 检查我们是否已关闭，例如 慢消费者
		c.mu.Lock()
		nc := c.nc
		// Activity based on interest changes or data/msgs
		if c.cache.inMsgs > 0 || c.cache.subs > 0 {
			c.last = last
		}
		c.mu.Unlock()

		if nc == nil {
			return
		}

		// Update buffer size as/if needed
		// Grow
		if n == len(b) && len(b) < maxBufSize {
			b = make([]byte, len(b)*2)
		}
		// Shrink
		if n < len(b)/2 && len(b) > minBufSize {
			b = make([]byte, len(b)/2)
		}
	}
}

func (c *client) setPingTimer() {
	if c.srv == nil {
		return
	}
	d := c.srv.getOpts().PingInterval
	c.ptmr = time.AfterFunc(d, c.processPingTimer)
}

func (c *client) processPingTimer() {

}

func (c *client) maxConnExceeded() {
	c.Errorf(ErrTooManyConnections.Error())
	c.sendErr(ErrTooManyConnections.Error())
	c.closeConnection()
}

func (c *client) closeConnection() {

}

func (c *client) processConnect(arg []byte) error {
	c.traceInOp("CONNECT", arg)

	c.mu.Lock()
	// if we can't stop the timer because the callback is in progress...
	if !c.clearAuthTimer() {
		// TODO
	}
	c.last = time.Now()
	typ := c.typ
	r := c.route
	srv := c.srv

	if err := json.Unmarshal(arg, &c.opts); err != nil {
		c.mu.Unlock()
		return err
	}

	c.flags.set(connectReceived)
	proto := c.opts.Protocol
	verbose := c.opts.Verbose
	lang := c.opts.Lang
	c.mu.Unlock()

	if srv != nil {
		if proto >= ClientProtoInfo {
			srv.mu.Lock()
			srv.cproto++
			srv.mu.Unlock()
		}
		// Check for Auth
		if ok := srv.checkAuthorization(c); !ok {
			c.authViolation()
			return ErrAuthorization
		}
	}

	// Check client protocol request if it exists
	if typ == CLIENT && (proto < ClientProtoZero || proto > ClientProtoInfo) {
		c.sendErr(ErrBadClientProtocol.Error())
		c.closeConnection()
		return ErrBadClientProtocol
	} else if typ == ROUTER && lang != "" {
		c.sendErr(ErrClientConnectedToRoutePort.Error())
		c.closeConnection()
		return ErrClientConnectedToRoutePort
	}

	// Grab connection name of remote route
	if typ == ROUTER && r != nil {
		c.mu.Lock()
		c.route.remoteID = c.opts.Name
		c.mu.Unlock()
	}

	if verbose {
		c.sendOK()
	}
	return nil

}

// Process the infomation message from Clients and other Routes
func (c *client) processInfo(arg []byte) error {
	info := Info{}
	if err := json.Unmarshal(arg, &info); err != nil {
		return err
	}
	if c.typ == ROUTER {
		c.processRouteInfo(&info)
	}
	return nil
}

// argo: old arg
func (c *client) processSub(argo []byte) (err error) {
	c.traceInOp("SUB", argo)

	//Indicate(表明) activity
	c.cache.subs += 1

	// Copy so we do not reference a potentially large buffer
	arg := make([]byte, len(argo))
	copy(arg, argo)
	args := splitArg(arg)

	// 了参数后会创建一个subscription订阅对象
	sub := &subscription{client: c}
	// 同时这里因为订阅组是可选的内容
	switch len(args) {
	case 2:
		sub.subject = args[0]
		sub.queue = nil
		sub.sid = args[1]
	case 3:
		sub.subject = args[0]
		sub.queue = args[1]
		sub.sid = args[2]
	default:
		return fmt.Errorf("processSub Parse Error: %s", arg)
	}

	shouldForward := false

	c.mu.Lock()
	if c.nc == nil {
		c.mu.Unlock()
		return nil
	}

	// Check permissions if applicable.
	if !c.canSubscribe(sub.subject) {
		c.mu.Unlock()
		c.sendErr(fmt.Sprintf("Permissions Violation for Subscription to %q", sub.subject))
		c.Errorf("Subscription Violation - User %q, Subject %q, SID %s",
			c.opts.Username, sub.subject, sub.sid)
		return nil
	}

	// 然后对router转发做一个过滤，比如两个router转发了同一个客户端的请求，此时订阅ID是相同的
	sid := string(sub.sid)
	if c.subs[sid] == nil {
		c.subs[sid] = sub
		if c.srv != nil {
			err = c.srv.sl.Insert(sub)
			if err != nil {
				delete(c.subs, sid)
			} else {
				shouldForward = c.typ != ROUTER
			}
		}
	}
	c.mu.Unlock()
	if err != nil {
		c.sendErr("Invalid Subject")
		return nil
	} else if c.opts.Verbose {
		c.sendOK()
	}
	if shouldForward {
		c.srv.broadcastSubscribe(sub)
	}

	return nil
}

func splitArg(arg []byte) [][]byte {
	a := [MAX_MSG_ARGS][]byte{}
	args := a[:0]
	start := -1
	for i, b := range arg {
		switch b {
		case ' ', '\t', '\r', '\n':
			if start >= 0 {
				args = append(args, arg[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		args = append(args, arg[start:])
	}
	return args
}

func (c *client) processPing() {
	c.mu.Lock()
	c.traceInOp("PING", nil)
	if c.nc == nil {
		c.mu.Unlock()
		return
	}
	c.traceOutOp("PONG", nil)
	err := c.sendProto([]byte("PONG\r\n"), true)
	if err != nil {
		c.clearConnection()
		c.Debugf("Error on Flush, error %s", err.Error())
	}
	srv := c.srv
	sendUpdateINFO := false
	// Check if this is the first Pong, if so...
	if c.flags.setIfNotSet(firstPongSent) {
		// Check if server should send an async INFO protocol to the client
		if c.opts.Protocol >= ClientProtoInfo &&
			srv != nil && c.flags.isSet(infoUpdated) {
			sendUpdateINFO = true
		}
		// We can now clear the flag
		c.flags.clear(infoUpdated)
	}
	c.mu.Unlock()

	// Some clients send an initial PING as part of the synchronous connect process.
	// They can't be receiving anything until the first PONG is received.
	// So we delay the possible update INFO after this point
	if sendUpdateINFO {
		srv.mu.Lock()
		// Use the cached protocol
		proto := srv.infoJSON
		srv.mu.Unlock()

		c.mu.Lock()
		c.sendInfo(proto)
		c.mu.Unlock()
	}
}

func (c *client) processPong() {
	c.traceInOp("PONG", nil)
	c.mu.Lock()
	c.pout = 0
	c.mu.Unlock()
}

func (c *client) traceInOp(op string, arg []byte) {
	c.traceOp("->> %s", op, arg)
}

func (c *client) traceOutOp(op string, arg []byte) {
	c.traceOp("<<- %s", op, arg)
}

func (c *client) traceOp(format, op string, arg []byte) {
	if !c.trace {
		return
	}

	opa := []interface{}{}
	if op != "" {
		opa = append(opa, op)
	}
	if arg != nil {
		opa = append(opa, string(arg))
	}
	c.Tracef(format, opa)
}

// Used to treat maps as efficient set
var needFlush = struct{}{}
var routeSeen = struct{}{}

// Assume the lock is held upon entry.
func (c *client) sendInfo(info []byte) {
	c.sendProto(info, true)
}

func (c *client) sendErr(err string) {
	c.mu.Lock()
	c.traceOutOp("-ERR", []byte(err))
	c.sendProto([]byte(fmt.Sprintf("-ERR '%s'\r\n", err)), true)
	c.mu.Unlock()
}

func (c *client) sendOK() {
	c.mu.Lock()
	c.traceOutOp("OK", nil)
	// Can not autoflush this one, needs to be async.
	c.sendProto([]byte("+OK\r\n"), false)
	c.pcd[c] = needFlush
	c.mu.Unlock()
}

func (c *client) authTimeout() {
	c.sendErr(ErrAuthTimeout.Error())
	c.Debugf("Authorization Timeout")
	c.closeConnection()
}

func (c *client) authViolation() {
	if c.srv != nil && c.srv.getOpts().Users != nil {
		c.Errorf("%s - User %q",
			ErrAuthorization.Error(),
			c.opts.Username)
	} else {
		c.Errorf(ErrAuthorization.Error())
	}
	c.sendErr("Authorization Violation")
	c.closeConnection()
}

func (c *client) RegisterUser(user *User) {
	if user.Permissions == nil {
		// Reset perms to nil in case client previously had them
		c.mu.Lock()
		c.perms = nil
		c.mu.Unlock()
		return
	}
	// Process Permissions and map into client connection structures.
	c.mu.Lock()
	defer c.mu.Unlock()

	// Pre-allocate all to simplify(简化) checks later.
	c.perms = &permissions{}
	c.perms.sub = NewSubList()
	c.perms.pub = NewSubList()
	c.perms.pcache = make(map[string]bool)

	// Loop over publish permissions
	for _, pubSubject := range user.Permissions.Publish {
		sub := &subscription{subject: []byte(pubSubject)}
		c.perms.pub.Insert(sub)
	}

	// Loop over subscribe permissions
	for _, subSubject := range user.Permissions.Subscribe {
		sub := &subscription{subject: []byte(subSubject)}
		c.perms.sub.Insert(sub)
	}
}

// Logging functionality scoped to a client or route.

func (c *client) Errorf(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Errorf(format, v...)
}

func (c *client) Debugf(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Debugf(format, v...)
}

func (c *client) Noticef(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Noticef(format, v...)
}

func (c *client) Tracef(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Tracef(format, v...)
}
