// Package preview is the gmp plugin. It relays a buffer to browsers.
package preview

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/Shougo/goda.vim/sdk"
)

const (
	// scriptVersion matches the version autoload/gmp.vim passes to goda#register.
	scriptVersion = 1
	closeGrace    = 200 * time.Millisecond
)

// Spec is everything a session needs, so no handler calls back into the editor.
type Spec struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Root   string `json:"root"`
	Name   string `json:"name"`
	Lines  []any  `json:"lines"`
	Cursor int    `json:"cursor"`
}

// Preview is idle when live is nil and serving otherwise.
type Preview struct {
	client fs.FS
	mu     sync.Mutex
	live   *session
}

// New returns a plugin that serves the browser page and bundle from client.
func New(client fs.FS) *Preview {
	return &Preview{client: client}
}

// Name implements sdk.Plugin.
func (p *Preview) Name() string { return "gmp" }

// ScriptVersion implements sdk.ScriptVersioner.
func (p *Preview) ScriptVersion() int { return scriptVersion }

// Handlers implements sdk.Plugin. open is the only request, because its caller
// needs the URL or the bind error. The others are notifications and return
// without waiting for a browser, since a slow one would delay every later push.
func (p *Preview) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"open":   p.open,
		"text":   p.text,
		"cursor": p.cursor,
		"close":  p.closeSession,
		"browse": browse,
	}
}

// Close implements sdk.Closer.
func (p *Preview) Close(ctx context.Context) error {
	s := p.stop()
	if s == nil {
		return nil
	}
	drained := make(chan struct{})
	go func() {
		s.pumps.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(closeGrace):
	case <-ctx.Done():
	}
	return nil
}

// stop ends the live session and returns it, or nil when idle.
func (p *Preview) stop() *session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopLocked()
}

func (p *Preview) stopLocked() *session {
	s := p.live
	s.stop()
	p.live = nil
	return s
}

func (p *Preview) open(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var spec Spec
	if err := sdk.DecodeArgs(params, &spec); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	s, err := start(spec, p.client)
	if err != nil {
		return nil, err
	}
	p.live = s
	return s.url, nil
}

func (p *Preview) session() *session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.live
}

func (p *Preview) text(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var lines []any
	if err := sdk.DecodeArgs(params, &lines); err != nil {
		return nil, err
	}
	text, err := newSnapshot(lines)
	if err != nil {
		return nil, err
	}
	if s := p.session(); s != nil {
		s.feed.update(func(v view) view { return v.withText(text) })
	}
	return nil, nil
}

func (p *Preview) cursor(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var line int
	if err := sdk.DecodeArgs(params, &line); err != nil {
		return nil, err
	}
	if s := p.session(); s != nil {
		s.feed.update(func(v view) view { return v.withCursor(line) })
	}
	return nil, nil
}

func (p *Preview) closeSession(context.Context, sdk.Host, []any) (any, error) {
	p.stop()
	return nil, nil
}

func browse(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var url, app string
	if err := sdk.DecodeArgs(params, &url, sdk.Optional(&app)); err != nil {
		return nil, err
	}
	argv := browserCommand(runtime.GOOS, app, url)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "gmp: %s: %v\n", argv[0], err)
		}
	}()
	return nil, nil
}

func browserCommand(goos, app, url string) []string {
	switch {
	case goos == "darwin" && app != "":
		return []string{"open", "-a", app, url}
	case goos == "darwin":
		return []string{"open", url}
	case app != "":
		return []string{app, url}
	default:
		return []string{"xdg-open", url}
	}
}
