package server

import "crypto/tls"

type Options struct {
	ConfigFile string      `json:"-"`
	Cluster    ClusterOpts `json:"cluster"`
	ProfPort   int         `json:"-"`
	PidFile    string      `josn:"-"`
	LogFile    string      `json:"-"`
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
