package server

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
