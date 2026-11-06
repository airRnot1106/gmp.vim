package preview

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const writeTimeout = 5 * time.Second

// session is one listener and the browsers connected to it.
type session struct {
	feed  *feed
	ln    net.Listener
	srv   *http.Server
	url   string
	pumps sync.WaitGroup
}

// start binds first, so a bind error returns before anything else exists.
func start(spec Spec, client fs.FS) (*session, error) {
	text, err := newSnapshot(spec.Lines)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port)))
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s := &session{
		feed: newFeed(view{name: spec.Name, text: text, line: spec.Cursor}),
		ln:   ln,
		url:  "http://" + net.JoinHostPort(spec.Host, strconv.Itoa(port)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, client, "markdown.html")
	})
	mux.HandleFunc("GET /_gmp/ws", s.pump)
	mux.Handle("GET /_gmp/", http.StripPrefix("/_gmp/", http.FileServerFS(client)))
	mux.Handle("GET /", http.FileServer(http.Dir(spec.Root)))

	var handler http.Handler = mux
	if isLoopback(spec.Host) {
		handler = rejectForeignHost(mux)
	}
	s.srv = &http.Server{Handler: handler}
	go s.srv.Serve(ln)
	return s, nil
}

func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// rejectForeignHost stops DNS rebinding, where a site points its own name at
// the loopback address and passes the browser's same-origin rules.
func rejectForeignHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopback(strings.Trim(host, "[]")) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stop publishes the closed view and returns with the port free. Pumps finish
// on their own. A nil session is already stopped.
func (s *session) stop() {
	if s == nil {
		return
	}
	s.feed.update(view.finished)
	s.srv.Close()
	s.ln.Close()
}

// pump sends one browser the frames it is missing until the session closes.
func (s *session) pump(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	s.pumps.Add(1)
	defer s.pumps.Done()
	defer c.CloseNow()

	gone := c.CloseRead(context.Background())
	sent := blank
	for {
		cur, changed := s.feed.read()
		for _, frame := range pending(sent, cur) {
			ctx, cancel := context.WithTimeout(gone, writeTimeout)
			err := c.Write(ctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return
			}
		}
		sent = cur
		if cur.closed {
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		select {
		case <-changed:
		case <-gone.Done():
			return
		}
	}
}
