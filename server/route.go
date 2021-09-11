package server

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
