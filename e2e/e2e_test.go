// Package e2e drives real Vim and Neovim through autoload/goda_mp.vim and goda's
// autoload, with the test as the browser.
//
// A headless editor has no main loop to fire TextChanged and CursorMoved, so
// the scenarios change the buffer and then run doautocmd.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const patience = 30 * time.Second

var (
	repoRoot string
	binary   string
	goda     string
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "goda-mp-e2e")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if repoRoot, err = filepath.Abs(".."); err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "goda-mp")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("go build: %v\n%s", err, out))
	}
	goda = os.Getenv("GODA_MP_E2E_GODA")
	if goda == "" {
		list := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/Shougo/goda.vim")
		list.Dir = repoRoot
		out, err := list.Output()
		if err != nil {
			panic(fmt.Sprintf("go list: %v", err))
		}
		goda = strings.TrimSpace(string(out))
	}
	return m.Run()
}

type editorKind struct {
	name    string
	banner  string
	command []string
}

// `vim` may be Neovim under another name, so the banner decides which it is.
var editorKinds = []editorKind{
	{"vim", "VIM - Vi IMproved", []string{"vim", "-u", "NONE", "-N", "-n", "-i", "NONE", "-es"}},
	{"nvim", "NVIM", []string{"nvim", "-u", "NONE", "-n", "-i", "NONE", "--headless"}},
}

func (k editorKind) installed() bool {
	out, err := exec.Command(k.command[0], "--version").Output()
	return err == nil && strings.HasPrefix(string(out), k.banner)
}

var scenarios = map[string]func(*testing.T, *editor){
	"open syncs the buffer, the page and files next to it": func(t *testing.T, e *editor) {
		e.write(t, "image.png", "pixels")
		url, b := e.preview(t, "a.md", "# one", "two")

		b.until(t, func(f frame) bool { return f.BufName != nil && *f.BufName == "a.md" })
		b.untilBuf(t, "# one", "two")
		b.untilCursor(t, 1, 2)

		for path, want := range map[string]string{
			"/":                         "/_goda-mp/markdown.bundle.js",
			"/_goda-mp/markdown.bundle.js":  "",
			"/_goda-mp/markdown.bundle.css": "",
			"/image.png":                "pixels",
		} {
			status, body := get(t, url+path)
			if status != http.StatusOK || !strings.Contains(body, want) {
				t.Errorf("GET %s = %d, body has no %q", path, status, want)
			}
		}
	},

	"open echoes the URL": func(t *testing.T, e *editor) {
		e.run(t, "edit a.md", "let g:goda_mp#open_browser_fn = 'CaptureURL'")
		out := e.run(t, "PreviewMarkdown")
		if url := e.eval(t, "g:url"); !strings.Contains(out, url) {
			t.Fatalf("messages %q have no %s", out, url)
		}
	},

	"without an opener function the browser starts with the URL": func(t *testing.T, e *editor) {
		e.run(t, "edit a.md", "let g:goda_mp#browser = 'firefox'")
		out := e.run(t, "PreviewMarkdown")
		var opened []byte
		eventually(t, "the opener ran", func() bool {
			opened, _ = os.ReadFile(e.openerLog)
			return len(opened) > 0
		})
		url := strings.TrimSpace(out[strings.Index(out, "http://"):])
		if !strings.Contains(string(opened), url) || !strings.Contains(string(opened), "firefox") {
			t.Fatalf("opener got %q, want firefox and %s", opened, url)
		}
	},

	"edits reach the browser on every text event": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one")
		b.untilBuf(t, "one")
		for _, event := range []string{"TextChanged", "TextChangedI", "TextChangedP"} {
			e.run(t, fmt.Sprintf("call setline(1, %q)", event), "doautocmd "+event)
			b.untilBuf(t, event)
		}
	},

	"cursor moves reach the browser on every cursor event": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one", "two", "three")
		b.untilCursor(t, 1, 3)
		e.run(t, "call cursor(2, 1)", "doautocmd CursorMoved")
		b.untilCursor(t, 2, 3)
		e.run(t, "call cursor(3, 1)", "doautocmd CursorMovedI")
		b.untilCursor(t, 3, 3)
	},

	"close tells the browser and frees the port": func(t *testing.T, e *editor) {
		url, b := e.preview(t, "a.md", "one")
		e.run(t, "PreviewMarkdownClose")
		b.untilClosed(t)
		eventually(t, "the port is free", func() bool {
			_, err := http.Get(url)
			return err != nil
		})
		if out := e.run(t, "PreviewMarkdownClose", "doautocmd TextChanged"); out != "" {
			t.Fatalf("second close and a later edit printed %q", out)
		}
	},

	"toggle opens, then closes": func(t *testing.T, e *editor) {
		e.run(t, "edit a.md", "let g:goda_mp#open_browser_fn = 'CaptureURL'", "PreviewMarkdownToggle")
		b := e.browser(t, e.eval(t, "g:url"))
		b.untilBuf(t, "")
		e.run(t, "PreviewMarkdownToggle")
		b.untilClosed(t)
	},

	"deleting the buffer closes the preview": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one")
		e.run(t, "bdelete!")
		b.untilClosed(t)
	},

	"wiping out the buffer closes the preview": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one")
		e.run(t, "bwipeout!")
		b.untilClosed(t)
	},

	"opening a second buffer replaces the preview": func(t *testing.T, e *editor) {
		_, first := e.preview(t, "a.md", "first")
		_, second := e.preview(t, "b.md", "second")
		first.untilClosed(t)
		second.untilBuf(t, "second")

		e.run(t, "buffer a.md", "call setline(1, 'ignored')", "doautocmd TextChanged")
		e.run(t, "buffer b.md", "call setline(1, 'kept')", "doautocmd TextChanged")
		got := second.until(t, func(f frame) bool { return f.Buf != nil })
		if !slices.Equal(got.Buf, []string{"kept"}) {
			t.Fatalf("text after edits in both buffers = %q, want the second buffer's edit", got.Buf)
		}
	},

	"reopening on a fixed port gives the same URL": func(t *testing.T, e *editor) {
		port := freePort(t)
		e.run(t, fmt.Sprintf("let g:goda_mp#server_port = %d", port))
		url, old := e.preview(t, "a.md", "old")
		if !strings.HasSuffix(url, ":"+strconv.Itoa(port)) {
			t.Fatalf("url = %s, want port %d", url, port)
		}
		again, fresh := e.preview(t, "a.md", "new")
		old.untilClosed(t)
		fresh.untilBuf(t, "new")
		if again != url {
			t.Fatalf("second url = %s, want %s", again, url)
		}
	},

	"a buffer option overrides the global one": func(t *testing.T, e *editor) {
		port := freePort(t)
		e.run(t,
			"edit a.md",
			"let g:goda_mp#server_port = 1",
			fmt.Sprintf("let b:goda_mp_server_port = %d", port),
			"let g:goda_mp#server_host = 'localhost'",
		)
		url, b := e.preview(t, "a.md", "one")
		if url != fmt.Sprintf("http://localhost:%d", port) {
			t.Fatalf("url = %s", url)
		}
		b.untilBuf(t, "one")
	},

	"a taken port warns and leaves no preview": func(t *testing.T, e *editor) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port
		e.run(t, "edit a.md", fmt.Sprintf("let g:goda_mp#server_port = %d", port))
		out := e.run(t, "PreviewMarkdown")
		if !strings.HasPrefix(out, "goda-mp: ") || !strings.Contains(out, "address already in use") || strings.Contains(out, "Vim(") {
			t.Fatalf("messages = %q", out)
		}
		if out := e.run(t, "doautocmd TextChanged"); out != "" {
			t.Fatalf("an edit after the failed open printed %q", out)
		}
		ln.Close()
		_, b := e.preview(t, "a.md", "one")
		b.untilBuf(t, "one")
	},

	"a killed binary warns once and is not restarted": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one")
		b.untilBuf(t, "one")
		started := e.starts(t)
		if err := syscall.Kill(started[len(started)-1], syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the editor sees the exit", func() bool { return e.eval(t, "goda#is_running('mp')") == "0" })

		out := e.run(t, "call setline(1, 'late')", "doautocmd TextChanged")
		if !strings.Contains(out, `goda-mp: preview lost, see goda#log("mp")`) {
			t.Fatalf("messages = %q", out)
		}
		if out := e.run(t, "doautocmd TextChanged", "doautocmd CursorMoved"); out != "" {
			t.Fatalf("later edits printed %q", out)
		}
		if got := e.starts(t); len(got) != len(started) || e.eval(t, "goda#is_running('mp')") != "0" {
			t.Fatalf("binary started again: %v, was %v", got, started)
		}

		_, fresh := e.preview(t, "a.md", "after")
		fresh.untilBuf(t, "after")
	},

	"quitting the editor closes the preview and ends the binary": func(t *testing.T, e *editor) {
		_, b := e.preview(t, "a.md", "one")
		b.untilBuf(t, "one")
		started := e.starts(t)
		e.quit(t)
		b.untilClosed(t)
		eventually(t, "the binary exits", func() bool {
			return errors.Is(syscall.Kill(started[len(started)-1], 0), syscall.ESRCH)
		})
	},

	"a non-markdown buffer is refused": func(t *testing.T, e *editor) {
		out := e.run(t, "edit a.txt", "call goda_mp#open()")
		if !strings.Contains(out, "goda-mp: not a markdown file") {
			t.Fatalf("messages = %q", out)
		}
		if e.eval(t, "goda#is_running('mp')") != "0" {
			t.Fatal("the binary started for a refused buffer")
		}
	},

	"a line of invalid UTF-8 reaches the browser with a replacement": func(t *testing.T, e *editor) {
		e.run(t, "edit a.md", "let g:goda_mp#open_browser_fn = 'CaptureURL'", `call setline(1, "open\xffed")`, "PreviewMarkdown")
		b := e.browser(t, e.eval(t, "g:url"))
		b.untilBuf(t, "open�ed")
		e.run(t, `call setline(1, "edit\xffed")`, "doautocmd TextChanged")
		b.untilBuf(t, "edit�ed")
	},

	"a large buffer opens and updates": func(t *testing.T, e *editor) {
		const lines = 20000
		e.run(t, "edit a.md", "let g:goda_mp#open_browser_fn = 'CaptureURL'",
			fmt.Sprintf("call setline(1, map(range(%d), 'string(v:val)'))", lines),
			"PreviewMarkdown")
		b := e.browser(t, e.eval(t, "g:url"))
		f := b.until(t, func(f frame) bool { return f.Buf != nil })
		if len(f.Buf) != lines || f.Buf[lines-1] != strconv.Itoa(lines-1) {
			t.Fatalf("got %d lines", len(f.Buf))
		}
		e.run(t, "call setline(1, 'edited')", "doautocmd TextChanged")
		f = b.until(t, func(f frame) bool { return f.Buf != nil })
		if len(f.Buf) != lines || f.Buf[0] != "edited" {
			t.Fatalf("got %d lines starting %q", len(f.Buf), f.Buf[0])
		}
		e.run(t, fmt.Sprintf("call cursor(%d, 1)", lines), "doautocmd CursorMoved")
		b.untilCursor(t, lines, lines)
	},

	"the binary is found in bin, then in result/bin": func(t *testing.T, e *editor) {
		e.run(t, "unlet g:goda_mp#binary", "edit a.md", "let g:goda_mp#open_browser_fn = 'CaptureURL'")
		out := e.run(t, "PreviewMarkdown")
		if !strings.Contains(out, "no binary found") || !strings.Contains(out, "nix build") {
			t.Fatalf("messages = %q", out)
		}

		for _, dir := range []string{"result/bin", "bin"} {
			link := filepath.Join(e.plugin, dir, "goda-mp")
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(binary, link); err != nil {
				t.Fatal(err)
			}
			e.run(t, "PreviewMarkdown")
			if got := e.eval(t, "goda#plugins().mp"); !strings.HasSuffix(got, "/plugin/"+dir+"/goda-mp") {
				t.Fatalf("running %s, want the one in %s", got, dir)
			}
			e.run(t, "call goda#stop()")
		}
	},
}

func TestEditors(t *testing.T) {
	for _, kind := range editorKinds {
		t.Run(kind.name, func(t *testing.T) {
			if !kind.installed() {
				if os.Getenv("GODA_MP_E2E_STRICT") != "" {
					t.Fatalf("%s is not installed", kind.name)
				}
				t.Skipf("%s is not installed", kind.name)
			}
			for name, scenario := range scenarios {
				t.Run(name, func(t *testing.T) { scenario(t, start(t, kind)) })
			}
		})
	}
}

type editor struct {
	cmd       *exec.Cmd
	exited    chan error
	dir       string
	plugin    string
	startLog  string
	openerLog string
	step      int
}

func start(t *testing.T, kind editorKind) *editor {
	t.Helper()
	dir := t.TempDir()
	e := &editor{
		exited:    make(chan error, 1),
		dir:       dir,
		plugin:    filepath.Join(dir, "plugin"),
		startLog:  filepath.Join(dir, "starts"),
		openerLog: filepath.Join(dir, "opener"),
	}
	for _, sub := range []string{"autoload", "ftplugin"} {
		if err := os.CopyFS(filepath.Join(e.plugin, sub), os.DirFS(filepath.Join(repoRoot, sub))); err != nil {
			t.Fatal(err)
		}
	}

	// The wrapper records each process, because goda does not expose its pid.
	wrapper := filepath.Join(dir, "goda-mp-wrapper")
	script := fmt.Sprintf("#!/bin/sh\necho $$ >> %s\nexec %s \"$@\"\n", e.startLog, binary)
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Stand-ins for the desktop openers keep a test from reaching a real browser.
	stubs := filepath.Join(dir, "stubs")
	if err := os.Mkdir(stubs, 0o755); err != nil {
		t.Fatal(err)
	}
	opener := fmt.Sprintf("#!/bin/sh\necho \"$0 $*\" >> %s\n", e.openerLog)
	for _, name := range []string{"open", "xdg-open", "firefox"} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(opener), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	driver := filepath.Join(repoRoot, "e2e", "testdata", "driver.vim")
	e.cmd = exec.Command(kind.command[0], append(kind.command[1:], "-S", driver)...)
	e.cmd.Dir = dir
	e.cmd.Env = append(os.Environ(),
		"GODA_MP_E2E_DIR="+dir,
		"GODA_MP_E2E_PLUGIN="+e.plugin,
		"GODA_MP_E2E_GODA="+goda,
		"HOME="+dir,
		"XDG_CONFIG_HOME="+filepath.Join(dir, "xdg-config"),
		"XDG_DATA_HOME="+filepath.Join(dir, "xdg-data"),
		"XDG_STATE_HOME="+filepath.Join(dir, "xdg-state"),
		"XDG_CACHE_HOME="+filepath.Join(dir, "xdg-cache"),
		"PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TERM=dumb",
	)
	if err := e.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { e.exited <- e.cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-e.exited:
			return
		default:
		}
		e.send("qa!")
		select {
		case <-e.exited:
		case <-time.After(5 * time.Second):
			e.cmd.Process.Kill()
			<-e.exited
		}
	})
	e.run(t, "let g:goda_mp#binary = "+strconv.Quote(wrapper))
	return e
}

func (e *editor) send(commands ...string) error {
	tmp := filepath.Join(e.dir, "cmd.tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(commands, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(e.dir, "cmd."+strconv.Itoa(e.step)))
}

// run executes Ex commands in the editor and returns their messages.
func (e *editor) run(t *testing.T, commands ...string) string {
	t.Helper()
	if err := e.send(commands...); err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(e.dir, "res."+strconv.Itoa(e.step))
	e.step++
	var b []byte
	eventually(t, "the editor answers "+strings.Join(commands, " | "), func() bool {
		var err error
		b, err = os.ReadFile(answer)
		return err == nil
	})
	status, out, _ := strings.Cut(string(b), "\n")
	out = strings.TrimSpace(out)
	if status != "ok" {
		t.Fatalf("%q: %s", commands, out)
	}
	return out
}

func (e *editor) eval(t *testing.T, expr string) string {
	t.Helper()
	return e.run(t, "echo "+expr)
}

func (e *editor) quit(t *testing.T) {
	t.Helper()
	if err := e.send("qa!"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.exited:
		e.exited <- nil
	case <-time.After(patience):
		t.Fatal("the editor did not quit")
	}
}

func (e *editor) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// preview opens name with lines in the editor and connects a browser to it.
func (e *editor) preview(t *testing.T, name string, lines ...string) (string, *browser) {
	t.Helper()
	quoted, err := json.Marshal(lines)
	if err != nil {
		t.Fatal(err)
	}
	e.run(t,
		"edit! "+name,
		"let g:goda_mp#open_browser_fn = 'CaptureURL'",
		"let g:url = ''",
		"silent %delete _",
		"call setline(1, "+string(quoted)+")",
		"call cursor(1, 1)",
		"PreviewMarkdown",
	)
	url := e.eval(t, "g:url")
	if url == "" {
		t.Fatalf("open failed: %q", e.run(t, "messages"))
	}
	return url, e.browser(t, url)
}

func (e *editor) browser(t *testing.T, url string) *browser {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), patience)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/_goda-mp/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(-1)
	t.Cleanup(func() { c.CloseNow() })
	return &browser{c}
}

// starts lists the pids of every binary process the editor started.
func (e *editor) starts(t *testing.T) []int {
	t.Helper()
	b, err := os.ReadFile(e.startLog)
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, field := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	return pids
}

type frame struct {
	Buf        []string `json:"buf"`
	BufName    *string  `json:"bufname"`
	CursorLine *struct {
		LinePos   int `json:"linePos"`
		BufLength int `json:"bufLength"`
	} `json:"cursorLine"`
	Connect string `json:"connect"`
}

type browser struct {
	conn *websocket.Conn
}

func (b *browser) next(t *testing.T) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), patience)
	defer cancel()
	_, data, err := b.conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func (b *browser) until(t *testing.T, match func(frame) bool) frame {
	t.Helper()
	for {
		if f := b.next(t); match(f) {
			return f
		}
	}
}

func (b *browser) untilBuf(t *testing.T, lines ...string) {
	t.Helper()
	b.until(t, func(f frame) bool { return f.Buf != nil && slices.Equal(f.Buf, lines) })
}

func (b *browser) untilCursor(t *testing.T, line, length int) {
	t.Helper()
	b.until(t, func(f frame) bool {
		return f.CursorLine != nil && f.CursorLine.LinePos == line && f.CursorLine.BufLength == length
	})
}

func (b *browser) untilClosed(t *testing.T) {
	t.Helper()
	b.until(t, func(f frame) bool { return f.Connect == "close" })
}

func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
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
