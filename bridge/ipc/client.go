package ipc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// Client 是 Go 外殼用的同步 IPC client（T15）。一問一答；同一個 Client 可被多個 goroutine 呼叫，
// 內部序列化請求。連線關閉時 server 會一併關閉這個連線開的 session。
type Client struct {
	mu      sync.Mutex
	conn    io.ReadWriteCloser
	session uint64
	info    map[string]string
	closed  bool
}

// NewClient 包裝既有的連線（net.Conn、net.Pipe 或測試替身）。
func NewClient(conn io.ReadWriteCloser) *Client {
	return &Client{conn: conn}
}

// Dial 連上引擎端點。network、address 與 net.Dial 相同，例如 "unix"、socket 路徑。
func Dial(network, address string) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// DialTimeout 與 Dial 相同，但限制連線建立的時間。
func DialTimeout(network, address string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// Open 開一個 session，並記住 open 回覆的 info（能力旗標）。已有 session 時先關閉舊的。
func (c *Client) Open() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("連線已關閉")
	}
	if c.session != 0 {
		_, _ = c.callLocked(Envelope{Op: OpClose, Session: c.session})
		c.session, c.info = 0, nil
	}
	reply, err := c.callLocked(Envelope{Op: OpOpen, Version: proto.Version})
	if err != nil {
		return err
	}
	if reply.Session == 0 {
		return errors.New("open 回覆缺少 session")
	}
	c.session = reply.Session
	c.info = copyInfo(reply.Info)
	return nil
}

// Handle 在目前 session 上送出請求，回傳完整快照。必須先 Open。
func (c *Client) Handle(req proto.Request) (proto.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return proto.Snapshot{}, errors.New("連線已關閉")
	}
	if c.session == 0 {
		return proto.Snapshot{}, errors.New("尚未 open")
	}
	reply, err := c.callLocked(Envelope{Op: OpHandle, Session: c.session, Req: &req})
	if err != nil {
		return proto.Snapshot{}, err
	}
	if reply.Snapshot == nil {
		return proto.Snapshot{}, errors.New("handle 回覆缺少快照")
	}
	return *reply.Snapshot, nil
}

// Poll 取得已完成且仍有效的背景候選評分。第二個回傳值為 false 表示目前
// 沒有可用結果（尚未完成、逾時、失敗或已過期），呼叫端保留畫面上的順序。
func (c *Client) Poll() (proto.Snapshot, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return proto.Snapshot{}, false, errors.New("連線已關閉")
	}
	if c.session == 0 {
		return proto.Snapshot{}, false, errors.New("尚未 open")
	}
	reply, err := c.callLocked(Envelope{Op: OpPoll, Session: c.session})
	if err != nil {
		return proto.Snapshot{}, false, err
	}
	if !reply.OK {
		return proto.Snapshot{}, false, errors.New(reply.Error)
	}
	if reply.Snapshot == nil {
		return proto.Snapshot{}, false, nil
	}
	return *reply.Snapshot, true, nil
}

// Health 查詢引擎狀態，並更新能力旗標快取。不需要先 Open。
func (c *Client) Health() (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("連線已關閉")
	}
	reply, err := c.callLocked(Envelope{Op: OpHealth})
	if err != nil {
		return nil, err
	}
	c.info = copyInfo(reply.Info)
	return copyInfo(reply.Info), nil
}

// Ping 確認連線兩端仍在運作。
func (c *Client) Ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("連線已關閉")
	}
	_, err := c.callLocked(Envelope{Op: OpPing})
	return err
}

// Session 回傳目前的 session；尚未 Open 時為 0。
func (c *Client) Session() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// Info 回傳最近一次 open 或 health 回覆的 info 副本。
func (c *Client) Info() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyInfo(c.info)
}

// Capability 回報最近一次 open 或 health 是否回報 name=true。舊引擎缺少該項時為 false。
func (c *Client) Capability(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return proto.Capability(c.info, name)
}

// Close 關閉連線；server 會一併關閉這個連線的 session。可重複呼叫。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.session, c.info = 0, nil
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) callLocked(env Envelope) (Reply, error) {
	var reply Reply
	if c.conn == nil {
		return reply, errors.New("沒有連線")
	}
	if err := WriteFrame(c.conn, env); err != nil {
		return reply, err
	}
	if err := ReadFrame(c.conn, &reply); err != nil {
		return reply, err
	}
	if !reply.OK {
		if reply.Error != "" {
			return reply, fmt.Errorf("%s：%s", env.Op, reply.Error)
		}
		return reply, fmt.Errorf("%s 失敗", env.Op)
	}
	return reply, nil
}

func copyInfo(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
