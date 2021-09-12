package server

import (
	"net/url"
)

type connectInfo struct {
	Verbose  bool   `json:"verbose"`        // 是否关闭服务器的+OK冗余信息，+OK见下面的说明
	Pedantic bool   `json:"pedantic"`       // 是否打开严格校验
	User     string `json:"user,omitempty"` // 用户名
	Pass     string `json:"pass,omitempty"` // 密码
	TLS      bool   `json:"tls_required"`   // 是否需要TLS
	Name     string `json:"name"`           // 客户端名称
}

const (
	_EMPTY_ = ""
)

type route struct {
	remoteID string
}

// StartRouting will start the accept loop om the cluster host:port
// and willl actively try to connect listed routes
func (s *Server) StartRouting(clientListenReady chan struct{}) {
	defer s.grWG.Done()

	// Wait for the client listen port to be opened,
	// and the possible ephemeral(临时的) port to be selected
	<-clientListenReady

	// spin up (加速自旋，这里应该是启动) the accept loop
	ch := make(chan struct{})
	go s.routeAcceptLoop(ch)
	<-ch

	// Solocot(索取) Routes id needed
	s.solicitRoutes(s.getOpts().Routes)
}

func (s *Server) routeAcceptLoop(ch chan struct{}) {

}

func (s *Server) solicitRoutes(routes []*url.URL) {

}
