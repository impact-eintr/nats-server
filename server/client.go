package server

import (
	"bufio"
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
	cache readCache

	pcd  map[*client]struct{}
	atmr *time.Timer
	ptmr *time.Timer
	wfc  int

	last time.Time
	// 这里client继承了协议解析状态机状态"parseState"。
	// 因此可以将这里的readloop想象成一个对流处理的处理器
	parseState
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
