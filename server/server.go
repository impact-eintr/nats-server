package server

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
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

	info Info

	// Server的配置信息
	configFile string
	optsMu     sync.RWMutex
	opts       *Options

	// Server的状态
	running  bool
	shutdown bool

	// Server中的goroutine的的互斥锁
	grMu      sync.Mutex
	grRunning bool // Server中的goroutine的状态
	logging   struct {
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
