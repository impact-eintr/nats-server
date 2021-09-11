package server

import (
	"crypto/tls"
	"net/url"
	"time"
)

type Options struct {
	// 基本配置
	ConfigFile string `json:"-"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Trace      bool   `json:"-"`
	Debug      bool   `json:"-"`
	MaxConn    int    `json:"max_connections"`

	PingInterval time.Duration `json:"ping_interval"`
	MaxPingsOut  int           `json:"ping_max"`

	MaxPayload int         `json:"max_payload"`
	Cluster    ClusterOpts `json:"cluster"`
	ProfPort   int         `json:"-"`
	PidFile    string      `josn:"-"`
	LogFile    string      `json:"-"`
	Routes     []*url.URL  `json:"-"`

	TLS       bool        `json:-`
	TLSConfig *tls.Config `json:"-"`
}

type ClusterOpts struct {
	Host           string      `json:"addr"`
	Port           int         `json:"cluster_port"`
	Username       string      `json:"-"`
	Password       string      `json:"-"`
	AuthTimeout    float64     `json:"-"`
	TLSTimeout     float64     `json:"-"`
	TLSConfig      *tls.Config `json:"-"`
	ListenStr      string      `json:"-"`
	NoAdvertise    bool        `json:"-"` // 通知
	ConnectRetries int         `json:-`   // 重连
}
