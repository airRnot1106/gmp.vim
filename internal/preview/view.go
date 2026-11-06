package preview

import (
	"encoding/json"
	"sync"
)

// snapshot is one encoded {buf} frame, shared by all browsers. Its pointer is
// its version.
type snapshot struct {
	frame []byte
	lines int
}

func newSnapshot(lines []any) (*snapshot, error) {
	frame, err := json.Marshal(map[string]any{"buf": lines})
	if err != nil {
		return nil, err
	}
	return &snapshot{frame: frame, lines: len(lines)}, nil
}

// view is everything a browser shows. It is immutable once published.
type view struct {
	name   string
	text   *snapshot
	line   int
	closed bool
}

// blank is what a browser shows before its first frame.
var blank = view{text: &snapshot{}}

func (v view) withText(text *snapshot) view {
	v.text = text
	return v
}

func (v view) withCursor(line int) view {
	v.line = line
	return v
}

func (v view) finished() view {
	v.closed = true
	return v
}

// pending lists the frames that move a browser from sent to cur. Every frame
// carries a whole value, so a browser that skips views still ends up current.
func pending(sent, cur view) [][]byte {
	var frames [][]byte
	if sent == blank {
		frames = append(frames, encode(map[string]any{"bufname": cur.name}))
	}
	if sent.text != cur.text {
		frames = append(frames, cur.text.frame)
	}
	if sent.line != cur.line || sent.text.lines != cur.text.lines {
		frames = append(frames, encode(map[string]any{
			"cursorLine": map[string]any{"linePos": cur.line, "bufLength": cur.text.lines},
		}))
	}
	if cur.closed && !sent.closed {
		frames = append(frames, encode(map[string]any{"connect": "close"}))
	}
	return frames
}

func encode(frame map[string]any) []byte {
	b, err := json.Marshal(frame)
	if err != nil {
		panic(err)
	}
	return b
}

// feed holds the latest view. Handlers write it and each browser's pump reads
// it at its own speed, so handlers never see a browser.
type feed struct {
	mu      sync.Mutex
	cur     view
	changed chan struct{}
}

func newFeed(v view) *feed {
	return &feed{cur: v, changed: make(chan struct{})}
}

func (f *feed) read() (view, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur, f.changed
}

func (f *feed) update(next func(view) view) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cur = next(f.cur)
	close(f.changed)
	f.changed = make(chan struct{})
}
