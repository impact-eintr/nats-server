package server

import "fmt"

type pubArg struct {
	subject []byte
	reply   []byte
	sid     []byte
	azb     []byte
	size    int
}

type parseState struct {
	state   int
	as      int
	drop    int
	pa      pubArg
	argBuf  []byte
	msgBuf  []byte
	scratch [MAX_CONTROL_LINE_SIZE]byte
}

// 整个协议，用这个作为状态机记录。
// 其中state表示各个状态，其实这里有点类似游标的感觉，比如对于Info协议：

// INFO {["option_name":option_value],...}\r\n

// 当读入到字母"I"的时候，就会切入到状态"OP_I"状态。
// 之后读到”INFO”后面的空格时，进入到"INFO_ARG"状态，至此，后面的内容都会被当成参数，
// 记录到argBuf参数中，直到最后"\r\n"的时候结束对这个完整消息的读取。

// 先订阅(s)后发布(p)

const (
	OP_START = iota
	OP_PLUS
	OP_PLUS_O
	OP_PLUS_OK
	OP_MINUS
	OP_MINUS_E
	OP_MINUS_ER
	OP_MINUS_ERR
	OP_MINUS_ERR_SPC
	MINUS_ERR_ARG
	OP_C
	OP_CO
	OP_CON
	OP_CONN
	OP_CONNE
	OP_CONNEC
	OP_CONNECT
	CONNECT_ARG
	OP_P
	OP_PU
	OP_PUB
	OP_PUB_SPC
	PUB_ARG
	OP_PI
	OP_PIN
	OP_PING
	OP_PO
	OP_PON
	OP_PONG
	MSG_PAYLOAD
	MSG_END
	OP_S
	OP_SU
	OP_SUB
	OP_SUB_SPC
	SUB_ARG
	OP_U
	OP_UN
	OP_UNS
	OP_UNSU
	OP_UNSUB
	OP_UNSUB_SPC
	UNSUB_ARG
	OP_M
	OP_MS
	OP_MSG
	OP_MSG_SPC
	MSG_ARG
	OP_I
	OP_IN
	OP_INF
	OP_INFO
	INFO_ARG
)

// buf是原始消息内容
func (c *client) parse(buf []byte) error {
	var i int
	var b byte

	// Snapshot this, and reset when we receive a proper CONNECT id needed
	authSet := c.isAuthTimerSet()

	for i = 0; i < len(buf); i++ {
		b = buf[i]
		switch c.state {
		// 是在"OP_START"状态下，先判断第一个字母是不是"C"/"c"，
		// 也就是大小写都兼容，并且客户端连接过来的第一条消息必须是"CONNECT"。
		case OP_START:
			if b != 'C' && b != 'c' && authSet {
				goto authErr
			}
			switch b {
			case 'C', 'c':
				c.state = OP_C
			case 'I', 'i':
				c.state = OP_I
			case 'S', 's':
				c.state = OP_S
			case 'P', 'p':
				c.state = OP_P
			case 'U', 'u':
				c.state = OP_U
			case 'M', 'm':
				if c.typ == CLIENT {
					goto parseErr
				} else {
					c.state = OP_M
				}
			case '+':
				c.state = OP_PLUS
			case '-':
				c.state = OP_MINUS
			default:
				goto parseErr
			}

		// 处理CONNECT的的命令 C -> S
		case OP_C:
			switch b {
			case 'O', 'o':
				c.state = OP_CO
			default:
				goto parseErr
			}
		case OP_CO:
			switch b {
			case 'N', 'n':
				c.state = OP_CON
			default:
				goto parseErr
			}
		case OP_CON:
			switch b {
			case 'N', 'n':
				c.state = OP_CONN
			default:
				goto parseErr
			}
		case OP_CONN:
			switch b {
			case 'E', 'e':
				c.state = OP_CONNE
			default:
				goto parseErr
			}
		case OP_CONNE:
			switch b {
			case 'C', 'c':
				c.state = OP_CONNEC
			default:
				goto parseErr
			}
		case OP_CONNEC:
			switch b {
			case 'T', 't':
				c.state = OP_CONNECT
			default:
				goto parseErr
			}
		case OP_CONNECT:
			switch b {
			case ' ', '\t':
				// 保持OP_CONNECT的状态 并且跳过了分隔符
				continue
			default:
				c.state = CONNECT_ARG
				c.as = i
			}
		case CONNECT_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop] // 划分出参数消息
				}
				// 处理CONNECT的逻辑
				if err := c.processConnect(arg); err != nil {
					return err
				}
				c.drop, c.state = 0, OP_START
				// Reset notions(概念) on authSet
				authSet = c.isAuthTimerSet()
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}

		// 处理INFO命令 S -> C
		case OP_I:
			switch b {
			case 'N', 'n':
				c.state = OP_IN
			default:
				goto parseErr
			}
		case OP_IN:
			switch b {
			case 'F', 'f':
				c.state = OP_INF
			default:
				goto parseErr
			}
		case OP_INF:
			switch b {
			case 'O', 'o':
				c.state = OP_INFO
			default:
				goto parseErr
			}
		case OP_INFO:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = INFO_ARG
				c.as = i
			}
		case INFO_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.processInfo(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_P:
			switch b {
			case 'U', 'u':
				c.state = OP_PU
			case 'I', 'i':
				c.state = OP_PI
			case 'O', 'o':
				c.state = OP_PO
			default:
				goto parseErr
			}

		// 处理 SUB 命令 客户端向服务器订阅一条消息  C -> S
		case OP_S:
			switch b {
			case 'U', 'u':
				c.state = OP_SU
			default:
				goto parseErr
			}
		case OP_SU:
			switch b {
			case 'B', 'b':
				c.state = OP_SUB
			default:
				goto parseErr
			}
		case OP_SUB:
			switch b {
			case ' ', '\t':
				c.state = OP_SUB_SPC
			default:
				goto parseErr
			}
		case OP_SUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = SUB_ARG
				c.as = i
			}
		case SUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.processSub(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}

		// 处理 UNSUB 命令 客户端向服务器取消之前的订阅 C -> S
		case OP_U:
			switch b {
			case 'N', 'n':
				c.state = OP_UN
			default:
				goto parseErr
			}
		case OP_UN:
			switch b {
			case 'S', 's':
				c.state = OP_UNS
			default:
				goto parseErr
			}
		case OP_UNS:
			switch b {
			case 'U', 'u':
				c.state = OP_UNSU
			default:
				goto parseErr
			}
		case OP_UNSU:
			switch b {
			case 'B', 'b':
				c.state = OP_UNSUB
			default:
				goto parseErr
			}
		case OP_UNSUB:
			switch b {
			case ' ', '\t':
				c.state = OP_UNSUB_SPC
			default:
				goto parseErr
			}
		case OP_UNSUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = UNSUB_ARG
				c.as = i
			}
		case UNSUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.processUnsub(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}

		// 处理 PUB 命令 客户端发送一个发布消息给服务器 C -> S
		case OP_PU:
			switch b {
			case 'B', 'b':
				c.state = OP_PUB
			default:
				goto parseErr
			}
		case OP_PUB:
			switch b {
			case ' ', '\t':
				c.state = OP_PUB_SPC
			default:
				goto parseErr
			}
		case OP_PUB_SPC:
			switch b {
			case ' ', '\t':
				// 保留当前状态不停地删除无效信息
				continue
			default:
				c.state = PUB_ARG
				c.as = i
			}
		case PUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.processPub(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD
				// 如果我们没有保存的缓冲区，则继续使用索引。
				// 如果这超出了剩下的内容，我们就会退出并处理拆分缓冲区。
				if c.msgBuf == nil {
					// TODO
				}
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}

		// 处理 MSG 命令 服务器发送订阅的内容给客户端 S -> C
		case OP_M:
			switch b {
			case 'S', 's':
				c.state = OP_MS
			default:
				goto parseErr
			}
		case OP_MS:
			switch b {
			case 'G', 'g':
				c.state = OP_MSG
			default:
				goto parseErr
			}
		case OP_MSG:
			switch b {
			case ' ', '\t':
				c.state = OP_MSG_SPC
			default:
				goto parseErr
			}
		case OP_MSG_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = MSG_ARG
				c.as = i
			}
		case MSG_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.processMsgArgs(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD

				// jump ahead with the index. If this overruns(泛滥成灾 超过)
				// what is left we fall out and process split buffer.
				// TODO
				i = c.as + c.pa.size - 1
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}

		// 处理 PING 命令 PING keep-alive 消息 S <- -> C
		case OP_PI:
			switch b {
			case 'N', 'n':
				c.state = OP_PIN
			default:
				goto parseErr
			}
		case OP_PIN:
			switch b {
			case 'G', 'g':
				c.state = OP_PING
			default:
				goto parseErr
			}
		case OP_PING:
			switch b {
			case '\n':
				c.processPing()
				c.drop, c.state = 0, OP_START
			}

		// 处理 PONG 命令 PONG keep-alive 响应 S <- -> C
		case OP_PO:
			switch b {
			case 'N', 'n':
				c.state = OP_PON
			default:
				goto parseErr
			}
		case OP_PON:
			switch b {
			case 'G', 'g':
				c.state = OP_PONG
			default:
				goto parseErr
			}
		case OP_PONG:
			switch b {
			case '\n':
				c.processPong()
				c.drop, c.state = 0, OP_START
			}

		case OP_PLUS:
			switch b {
			case 'O', 'o':
				c.state = OP_PLUS_O
			default:
				goto parseErr
			}
		case OP_PLUS_O:
			switch b {
			case 'K', 'k':
				c.state = OP_PLUS_OK
			default:
				goto parseErr
			}
		case OP_PLUS_OK:
			switch b {
			case '\n':
				c.drop, c.state = 0, OP_START
			}

		case OP_MINUS:
			switch b {
			case 'E', 'e':
				c.state = OP_MINUS_E
			default:
				goto parseErr
			}
		case OP_MINUS_E:
			switch b {
			case 'R', 'r':
				c.state = OP_MINUS_ER
			default:
				goto parseErr
			}
		case OP_MINUS_ER:
			switch b {
			case 'R', 'r':
				c.state = OP_MINUS_ERR
			default:
				goto parseErr
			}
		case OP_MINUS_ERR:
			switch b {
			case ' ', '\t':
				c.state = OP_MINUS_ERR_SPC
			default:
				goto parseErr
			}
		case OP_MINUS_ERR_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = MINUS_ERR_ARG
				c.as = i
			}
		case MINUS_ERR_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				c.processErr(string(arg))
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		default:
			goto parseErr
		}
	}

	// 循环结束
	return nil
authErr:
	c.authViolation()
	return ErrAuthorization

parseErr:
	s.sendErr("Unknown Protocol Operation")
	snip := protoSnippet(i, buf)
	err := fmt.Errorf("%s Parser ERROR, state=%d, i=%d: proto='%s...'",
		c.typeString(), c.state, i, snip)
	return err

}
