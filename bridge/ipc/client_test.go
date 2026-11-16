package ipc

import (
	"net"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

func newPipeClient(t *testing.T, srv *Server) *Client {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go srv.handle(serverConn)
	c := NewClient(clientConn)
	t.Cleanup(func() { c.Close() })
	return c
}

func TestClientRequiresOpen(t *testing.T) {
	c := newPipeClient(t, &Server{Engine: toyEngine(t)})
	if _, err := c.Handle(proto.Request{Type: proto.Key, Key: "a"}); err == nil || !strings.Contains(err.Error(), "尚未 open") {
		t.Fatalf("Handle before Open: %v", err)
	}
	if c.Session() != 0 {
		t.Fatalf("session = %d", c.Session())
	}
}

func TestClientOpenHandleHealth(t *testing.T) {
	srv := &Server{Engine: toyEngine(t), Info: map[string]string{"version": "test"}}
	c := newPipeClient(t, srv)
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	if c.Session() == 0 {
		t.Fatal("Open did not return a session")
	}
	if err := c.Ping(); err != nil {
		t.Fatal(err)
	}
	snap, err := c.Handle(proto.Request{Type: proto.Key, Key: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Handled || snap.Text() == "" {
		t.Fatalf("snapshot: %+v", snap)
	}
	info, err := c.Health()
	if err != nil {
		t.Fatal(err)
	}
	if info["version"] != "test" || info["sessions"] == "" || info["composing"] != "1" {
		t.Fatalf("health: %v", info)
	}
	if c.Info()["sessions"] != info["sessions"] {
		t.Fatalf("Health did not refresh cached info: %v", c.Info())
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := c.Open(); err == nil {
		t.Fatal("Open after Close must fail")
	}
}

func TestClientCapabilities(t *testing.T) {
	srv := &Server{Engine: toyEngine(t), Capabilities: []string{proto.CapJapanese, proto.CapConvertKeys}}
	c := newPipeClient(t, srv)
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	if !c.Capability(proto.CapJapanese) || !c.Capability(proto.CapConvertKeys) {
		t.Fatalf("open must advertise configured capabilities: %v", c.Info())
	}
	if c.Capability(proto.CapClauseDisplay) || c.Capability(proto.CapLiveConversion) {
		t.Fatalf("unlisted capabilities must stay absent: %v", c.Info())
	}
	info, err := c.Health()
	if err != nil {
		t.Fatal(err)
	}
	if info[proto.CapJapanese] != "true" {
		t.Fatalf("health must advertise capabilities: %v", info)
	}
	if _, ok := info[proto.CapClauseDisplay]; ok {
		t.Fatalf("health must not invent capabilities: %v", info)
	}
	// Info 回傳副本，外殼改它不影響能力判斷。
	info2 := c.Info()
	info2[proto.CapJapanese] = "false"
	if !c.Capability(proto.CapJapanese) {
		t.Fatal("Info must return a copy")
	}
}

// 舊引擎沒有能力旗標：新外殼看到 false，照舊走注音，不送日文模式與按鍵。
func TestLegacyEngineDoesNotAdvertiseJapanese(t *testing.T) {
	srv := &Server{Engine: toyEngine(t)}
	if _, ok := srv.health()[proto.CapJapanese]; ok {
		t.Fatal("health must omit unsupported capabilities")
	}
	if _, ok := srv.info()[proto.CapJapanese]; ok {
		t.Fatal("open must omit unsupported capabilities")
	}
	c := newPipeClient(t, srv)
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	for _, cap := range []string{proto.CapJapanese, proto.CapConvertKeys, proto.CapClauseDisplay, proto.CapLiveConversion} {
		if c.Capability(cap) {
			t.Errorf("legacy engine must not report %s", cap)
		}
	}
	if _, err := c.Health(); err != nil {
		t.Fatal(err)
	}
	if c.Capability(proto.CapJapanese) {
		t.Fatal("health must not add japanese")
	}
}
