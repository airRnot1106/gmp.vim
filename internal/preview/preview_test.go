package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Shougo/goda.vim/sdk"
	"github.com/coder/websocket"
)

var clientFiles = fstest.MapFS{
	"markdown.html":      {Data: []byte("<title>page</title>")},
	"markdown.bundle.js": {Data: []byte("bundle()")},
}

func call(t *testing.T, p *Preview, method string, params ...any) (any, error) {
	t.Helper()
	return p.Handlers()[method](t.Context(), nil, params)
}

func spec(root string, port int, lines ...any) map[string]any {
	return map[string]any{
		"host": "127.0.0.1", "port": int64(port), "root": root, "name": "a.md",
		"lines": lines, "cursor": int64(1),
	}
}

func open(t *testing.T, p *Preview, s map[string]any) string {
	t.Helper()
	url, err := call(t, p, "open", s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(context.Background()) })
	return url.(string)
}

func dial(t *testing.T, url string, header http.Header) (*websocket.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(url, "http") + "/_goda-mp/ws"
	c, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(-1)
	t.Cleanup(func() { c.CloseNow() })
	return c, nil
}

func browser(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func next(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if err := json.Unmarshal(b, &frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func expect(t *testing.T, c *websocket.Conn, want ...string) {
	t.Helper()
	for _, w := range want {
		var frame map[string]any
		if err := json.Unmarshal([]byte(w), &frame); err != nil {
			t.Fatal(err)
		}
		if got := next(t, c); !reflect.DeepEqual(got, frame) {
			t.Fatalf("frame = %v, want %v", got, frame)
		}
	}
}

func get(t *testing.T, url, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestBrowserGetsTheBufferOnConnect(t *testing.T) {
	p := New(clientFiles)
	url := open(t, p, spec(t.TempDir(), 0, "# one", "two"))
	expect(t, browser(t, url),
		`{"bufname":"a.md"}`,
		`{"buf":["# one","two"]}`,
		`{"cursorLine":{"linePos":1,"bufLength":2}}`,
	)
}

func TestUnnamedBufferStillSendsItsName(t *testing.T) {
	s := spec(t.TempDir(), 0, "one")
	s["name"] = ""
	expect(t, browser(t, open(t, New(clientFiles), s)), `{"bufname":""}`, `{"buf":["one"]}`)
}

func TestEditsReachAConnectedBrowser(t *testing.T) {
	p := New(clientFiles)
	c := browser(t, open(t, p, spec(t.TempDir(), 0, "one")))
	expect(t, c, `{"bufname":"a.md"}`, `{"buf":["one"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)

	if _, err := call(t, p, "text", []any{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	expect(t, c, `{"buf":["one","two"]}`, `{"cursorLine":{"linePos":1,"bufLength":2}}`)

	if _, err := call(t, p, "cursor", int64(2)); err != nil {
		t.Fatal(err)
	}
	expect(t, c, `{"cursorLine":{"linePos":2,"bufLength":2}}`)
}

func TestInvalidUTF8IsReplacedInTheFrame(t *testing.T) {
	p := New(clientFiles)
	c := browser(t, open(t, p, spec(t.TempDir(), 0, "one")))
	expect(t, c, `{"bufname":"a.md"}`, `{"buf":["one"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)
	if _, err := call(t, p, "text", []any{"a\xffb"}); err != nil {
		t.Fatal(err)
	}
	expect(t, c, `{"buf":["a�b"]}`)
}

func TestStalledBrowserDoesNotDelayHandlersOrOtherBrowsers(t *testing.T) {
	p := New(clientFiles)
	url := open(t, p, spec(t.TempDir(), 0, "start"))
	browser(t, url) // connected and never read
	fast := browser(t, url)

	big := strings.Repeat("x", 1<<20)
	begin := time.Now()
	const pushes = 100
	for i := range pushes {
		if _, err := call(t, p, "text", []any{big, strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(begin); took >= writeTimeout {
		t.Fatalf("%d pushes took %v, as long as a blocked write", pushes, took)
	}

	last := strconv.Itoa(pushes - 1)
	for {
		frame := next(t, fast)
		if buf, ok := frame["buf"].([]any); ok && buf[len(buf)-1] == last {
			return
		}
	}
}

func TestCloseTellsTheBrowserAndFreesThePort(t *testing.T) {
	p := New(clientFiles)
	url := open(t, p, spec(t.TempDir(), 0, "one"))
	c := browser(t, url)
	expect(t, c, `{"bufname":"a.md"}`, `{"buf":["one"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)

	if _, err := call(t, p, "close"); err != nil {
		t.Fatal(err)
	}
	expect(t, c, `{"connect":"close"}`)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("read after close = %v, want a normal closure", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Fatal("the server still answers after close")
	}
	if _, err := call(t, p, "close"); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestPluginCloseReachesTheBrowser(t *testing.T) {
	p := New(clientFiles)
	c := browser(t, open(t, p, spec(t.TempDir(), 0, "one")))
	expect(t, c, `{"bufname":"a.md"}`, `{"buf":["one"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	expect(t, c, `{"connect":"close"}`)
}

func TestReopenOnAFixedPortReplacesTheSession(t *testing.T) {
	p := New(clientFiles)
	port := freePort(t)
	old := browser(t, open(t, p, spec(t.TempDir(), port, "old")))
	expect(t, old, `{"bufname":"a.md"}`, `{"buf":["old"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)

	url := open(t, p, spec(t.TempDir(), port, "new"))
	expect(t, old, `{"connect":"close"}`)
	expect(t, browser(t, url), `{"bufname":"a.md"}`, `{"buf":["new"]}`, `{"cursorLine":{"linePos":1,"bufLength":1}}`)
}

func TestPortInUseLeavesThePluginIdle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	p := New(clientFiles)
	old := open(t, p, spec(t.TempDir(), 0, "old"))
	if _, err := call(t, p, "open", spec(t.TempDir(), ln.Addr().(*net.TCPAddr).Port, "new")); err == nil {
		t.Fatal("open on a taken port succeeded")
	}
	if _, err := http.Get(old); err == nil {
		t.Fatal("the previous session survived a failed open")
	}
	if _, err := call(t, p, "text", []any{"late"}); err != nil {
		t.Fatalf("push while idle: %v", err)
	}
	expect(t, browser(t, open(t, p, spec(t.TempDir(), 0, "next"))),
		`{"bufname":"a.md"}`, `{"buf":["next"]}`)
}

func TestRoutes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "image.png"), []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "markdown.bundle.js"), []byte("user file"), 0o644); err != nil {
		t.Fatal(err)
	}
	url := open(t, New(clientFiles), spec(root, 0))
	host := strings.TrimPrefix(url, "http://")
	for path, want := range map[string]string{
		"/":                        "<title>page</title>",
		"/_goda-mp/markdown.bundle.js": "bundle()",
		"/image.png":               "pixels",
		"/markdown.bundle.js":      "user file",
	} {
		status, body := get(t, url+path, host)
		if status != http.StatusOK || body != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, status, body, want)
		}
	}
	if status, _ := get(t, url+"/missing.png", host); status != http.StatusNotFound {
		t.Errorf("GET /missing.png = %d, want 404", status)
	}
}

func TestLoopbackServerRejectsForeignHostHeaders(t *testing.T) {
	url := open(t, New(clientFiles), spec(t.TempDir(), 0))
	port := url[strings.LastIndex(url, ":")+1:]
	for host, want := range map[string]int{
		"127.0.0.1:" + port:        http.StatusOK,
		"localhost:" + port:        http.StatusOK,
		"[::1]:" + port:            http.StatusOK,
		"localhost":                http.StatusOK,
		"rebound.example:" + port:  http.StatusForbidden,
		"rebound.example":          http.StatusForbidden,
		"127.0.0.1.nip.io:" + port: http.StatusForbidden,
	} {
		if status, _ := get(t, url+"/", host); status != want {
			t.Errorf("Host %s = %d, want %d", host, status, want)
		}
	}
}

func TestNonLoopbackServerAcceptsAnyHostHeader(t *testing.T) {
	s := spec(t.TempDir(), 0)
	s["host"] = "0.0.0.0"
	url := open(t, New(clientFiles), s)
	port := url[strings.LastIndex(url, ":")+1:]
	if status, _ := get(t, "http://127.0.0.1:"+port+"/", "my-laptop.lan:"+port); status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestWebSocketRejectsAnotherOrigin(t *testing.T) {
	url := open(t, New(clientFiles), spec(t.TempDir(), 0, "one"))
	if _, err := dial(t, url, http.Header{"Origin": {"http://evil.example"}}); err == nil {
		t.Fatal("a page from another origin connected")
	}
	if _, err := dial(t, url, http.Header{"Origin": {url}}); err != nil {
		t.Fatalf("same origin: %v", err)
	}
}

func TestBrowseRunsTheOpener(t *testing.T) {
	bin := t.TempDir()
	out := filepath.Join(bin, "args")
	opener := fmt.Sprintf("#!/bin/sh\necho \"$0 $*\" > %s.tmp && mv %s.tmp %s\n", out, out, out)
	for _, name := range []string{"open", "xdg-open", "firefox"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(opener), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := New(clientFiles).Handlers()["browse"](t.Context(), sdk.Host(nil), []any{"http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(out)
		if err == nil {
			if !strings.Contains(string(b), "http://127.0.0.1:1") {
				t.Fatalf("opener got %q", b)
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBrowserCommand(t *testing.T) {
	const url = "http://127.0.0.1:1"
	for _, c := range []struct {
		goos, app string
		want      []string
	}{
		{"darwin", "", []string{"open", url}},
		{"darwin", "Safari", []string{"open", "-a", "Safari", url}},
		{"linux", "", []string{"xdg-open", url}},
		{"linux", "firefox", []string{"firefox", url}},
	} {
		if got := browserCommand(c.goos, c.app, url); !reflect.DeepEqual(got, c.want) {
			t.Errorf("browserCommand(%q, %q) = %v, want %v", c.goos, c.app, got, c.want)
		}
	}
}
