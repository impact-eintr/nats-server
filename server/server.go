package server

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

type Info struct {
	ID                string   `json:"server_id"`     // NATS服务器的ID
	Version           string   `json:"version"`       // NATS的版本
	GoVersion         string   `json:"go"`            // NATS用的go版本
	Host              string   `json:"host"`          // 服务器主机IP
	Port              int      `json:"port"`          // 服务器主机Port
	AuthRequired      bool     `json:"auth_required"` // 是否需要鉴权
	SSLRequired       bool     `json:"ssl_required"`  // 是否需要SSL
	TLSRequired       bool     `json:"tls_required"`  // 是否需要TLS
	TLSVerify         bool     `json:"tls_verify"`    // TLS需要的证书
	MaxPayload        int      `int:"max_payload"`    // 最大接受长度
	IP                string   `json:"ip,omitempty"`
	ClientConnectURLs []string `json:"connect_urls,omitempty"` // 一个URL列表，表示客户端可以连接的服务器地址
}

type Server struct {
	mu sync.Mutex // Server的全局互斥锁

	info     Info
	infoJSON []byte

	// Server的配置信息
	configFile string
	optsMu     sync.RWMutex
	opts       *Options

	// Server的状态
	running  bool
	shutdown bool
	listener net.Listener

	clients      map[uint64]*client
	totalClients uint64
	done         chan bool
	start        time.Time

	// Server中的goroutine的的互斥锁
	grMu      sync.Mutex
	grRunning bool // Server中的goroutine的状态
	//grTnpClients map[uint64]*client
	grWG sync.WaitGroup // to wait on(服侍) various(各种各样的) goroutines

	// 日志
	logging struct {
		sync.RWMutex
		logger Logger
		trace  int32
		debug  int32
	}
}

func New(opt *Options) *Server {

}

func (s *Server) Start() {
	s.Noticef("Starting nats0server version %s", VERSION)
	s.Debugf("Go build version %s", s.info.GoVersion)

	// Avoid RACE between Start() and Shutdown()
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.grMu.Lock()
	s.grRunning = true
	s.grMu.Unlock()

	// Snapshot server options
	opts := s.getOpts()

	// Log the pid to a file
	if opts.PidFile != _EMPTY_ {
		if err := s.logPid(); err != nil {
			PrintAndDie(fmt.Sprintf("Could not write pidfile: %v\n", err))
		}
	}

	// Start moitoring(监视) if needed
	// if err := s.StartMonitoring(); err != nil {
	// 	s.Fatalf("Can't start monitoring:%v", err)
	// 	return
	// }

	// The Routing goroutine needs to wait for the client listen port to be opened
	// and potentail(可能存在的) ephemeral(短暂的) port selected
	clientListenReady := make(chan struct{})

	// Start up routing as well if needed
	if opts.Cluster.Port != 0 {
		s.startGoRoutine(func() {
			s.StartRouting(clientListenReady)
		})
	}

	// Pprof http endpoint for the profiler(分析器)
	if opts.ProfPort != 0 {
		s.StartProfiler()
	}

	// Wait for clients
	s.AcceptLoop(clientListenReady)
}

func (s *Server) getOpts() *Options {
	s.optsMu.RLock()
	opts := s.opts
	s.optsMu.RUnlock()
	return opts
}

func (s *Server) logPid() error {
	pidStr := strconv.Itoa(os.Getpid())
	return ioutil.WriteFile(s.getOpts().PidFile, []byte(pidStr), 0660)
}

func PrintAndDie(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	os.Exit(1)
}

// 回调
func (s *Server) startGoRoutine(f func()) {
	s.grMu.Lock()
	if s.grRunning {
		s.grWG.Add(1)
		go f()
	}
	s.grMu.Unlock()
}

// StartProfiler is called to enable dynamic profiling(描述)
func (s *Server) StartProfiler() {

}

func (s *Server) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// AcceptLoop is exported(可导出的) for easier testing
func (s *Server) AcceptLoop(clr chan struct{}) {
	// If we were to exit before the the listener is setup properly(正确地),
	// make sure we close the channel(防止gotoutine泄露).
	// 这里传入的clr，最终当循环退出时，会传递一个消息到channel中，
	// 通知启动Server.Start()的调用者，服务结束了。
	defer func() {
		if clr != nil {
			close(clr)
		}
	}()

	// Snapshot server options.
	opts := s.getOpts()

	// 开启对服务端口的监听
	hp := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	l, err := net.Listen("tcp", hp)
	if e != nil {
		s.Fatalf("Error listening on port:%s, %q", hp, err)
		return
	}
	s.Noticef("Listening for client connections on %s",
		net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)))

	// Alert of(对……的警戒) TLS enabled
	if opts.TLSConfig != nil {
		s.Noticef("TLS required for client connections")
	}

	s.Debugf("Server id is %s", s.info.ID)
	s.Noticef("Server is ready")

	// Setup state that can enable shutdown
	s.mu.Lock()
	s.listener = l

	// Id server was started with RANDOM_PORT(-1), opts.Port would be equal to 0
	// at the beginning this function. So we need to get the actual port
	if opts.Port == 0 {
		// Write resolved port back to options. 这里获取正确的端口号
		_, port, err := net.SplitHostPort(l.Addr().String())
		if err != nil {
			s.Fatalf("Error parsing server address (%s):%s", l.Addr().String(), err)
			s.mu.Unlock()
			return
		}
		portNum, err := strconv.Atoi(port)
		if err != nil {
			s.Fatalf("Error parsing server address (%s): %s", l.Addr().String(), err)
			s.mu.Unlock()
			return
		}
		opts.Port = portNum
	}
	s.mu.Unlock()

	// Let the caller know that we are ready
	close(clr)
	clr = nil

	// Dealy 延迟
	tmpDelay := ACCEPT_MIN_SLEEP

	// 这里的：for s.isRunning() { 形成了真正的AcceptLoop等待客户端过来创建TCP链接。
	for s.isRunning() {
		conn, err := l.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				s.Debugf("Temporary Client Accept Error(%s), sleeping %dms", ne, tmpDelay/time.Millisecond)
				time.Sleep(tmpDelay)
				tmpDelay *= 2
				if tmpDelay > ACCEPT_MAX_SLEEP {
					tmpDelay = ACCEPT_MAX_SLEEP
				}
			} else if s.isRunning() {
				s.Noticef("Accept error: %v", err)
			}
			continue
		}
		tmpDelay = ACCEPT_MIN_SLEEP
		// 每当Accept一条新链接后，开启一个goroutine用这个链接创建一个Client对象。
		s.startGoRoutine(func() {
			s.createClinet(conn)
			s.grWG.Done()
		})
	}
	s.Noticef("Server Exiting")
	s.done <- true
}

func (s *Server) createClinet(conn net.Conn) *client {
	// Snapshot server options
	opts := s.getOpts()

	c := &client{
		srv:   s,
		nc:    conn,
		opts:  defaultOpts,
		mpay:  int64(opts.MaxPayload),
		start: time.Now(),
	}

	s.mu.Lock()
	info := s.infoJSON
	authRequired := s.info.AuthRequired
	tlsRequired := s.info.TLSRequired
	s.totalClients++
	s.mu.Unlock()

	// Grab(获取) lock
	c.mu.Lock()
	// Initialize
	c.initClient()
	c.Debugf("Client connection created")

	// Send out infomation 发送INFO类型消息
	c.sendInfo(info)

	// Unlock to register
	c.mu.Unlock()

	// Register with the server
	s.mu.Lock()
	// If server is not running, Shutdown() may have already gathered(聚集) the list of connections
	// to close. It won't contain(包含) this on, so we need to bail out (退出) now otherwise
	// the readloop started down there would not be interrupted
	if !s.running {
		s.mu.Unlock()
		return c
	}

	// If there is a max connections specified(规定的), check that adding this new client would not
	// push us over(把...推到) the max
	if opts.MaxConn > 0 && len(s.clients) >= opts.MaxConn {
		s.mu.Unlock()
		c.maxConnExceeded()
		return nil
	}
	s.clients[c.cid] = c
	s.mu.Unlock()

	// Re-Grab lock
	c.mu.Lock()

	// Check for TLS
	if tlsRequired {

	}

	// The connection may have been closed
	if c.nc == nil {
		c.mu.Unlock()
		return c
	}

	// Check for Auth. We schedule this timer after the TLS handshake to avoid
	// the race where the timer fires during the handshake and causes the
	// server to write bad data to the socket.
	if authRequired {

	}

	if tlsRequired {

	}

	// Do final client initialization

	//Set the Ping timer
	c.setPingTimer()

	s.startGoRoutine(func() {
		c.readLoop()
	})

	if tlsRequired {

	}

	c.mu.Unlock()

	return c

}

func (s *Server) Shutdown() {
	s.grWG.Wait()
}
