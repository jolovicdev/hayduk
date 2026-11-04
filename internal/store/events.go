package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

const (
	eventQueueCap   = 4096
	eventFlushEvery = 500 * time.Millisecond
	eventFlushBatch = 64
)

// maxEventFileSize is a variable so tests can force rotation cheaply.
var maxEventFileSize int64 = 32 << 20

// eventLog is an append-only JSONL audit log with a bounded producer queue.
// One goroutine owns the file handle; producers only enqueue, so an operator
// action never waits on a disk flush.
type eventLog struct {
	path     string
	ch       chan *protocol.EventEntry
	flushReq chan chan error
	wg       sync.WaitGroup

	mu      sync.RWMutex // closed
	closed  bool
	writeMu sync.Mutex // file handle, buffered writer, size, err
	f       *os.File
	w       *bufio.Writer
	size    int64
	err     error

	dropped atomic.Int64
	queued  atomic.Int64
}

func openEventLog(path string) (*eventLog, error) {
	l := &eventLog{
		path:     path,
		ch:       make(chan *protocol.EventEntry, eventQueueCap),
		flushReq: make(chan chan error, 1),
	}
	if err := l.openFile(); err != nil {
		return nil, err
	}
	l.wg.Add(1)
	go l.run()
	return l, nil
}

// openFile opens or creates the log and sizes the rotation counter.
func (l *eventLog) openFile() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f = f
	l.w = bufio.NewWriterSize(f, 32<<10)
	l.size = st.Size()
	return nil
}

func (l *eventLog) run() {
	defer l.wg.Done()
	ticker := time.NewTicker(eventFlushEvery)
	defer ticker.Stop()
	pending := 0
	for {
		select {
		case entry, ok := <-l.ch:
			if !ok {
				l.writeMu.Lock()
				l.flushLocked()
				l.closeFileLocked()
				l.writeMu.Unlock()
				return
			}
			l.writeEntry(entry)
			l.queued.Add(-1)
			pending++
			if pending >= eventFlushBatch {
				l.writeMu.Lock()
				l.flushLocked()
				l.writeMu.Unlock()
				pending = 0
			}
		case req := <-l.flushReq:
			l.writeMu.Lock()
			l.flushLocked()
			err := l.err
			l.writeMu.Unlock()
			req <- err
			pending = 0
		case <-ticker.C:
			l.writeMu.Lock()
			l.flushLocked()
			l.writeMu.Unlock()
			pending = 0
		}
	}
}

func (l *eventLog) append(entry *protocol.EventEntry) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return
	}
	l.queued.Add(1)
	select {
	case l.ch <- entry:
	default:
		// A disk this far behind is already a problem; dropping the audit
		// line is preferable to stalling every broadcast behind it.
		l.queued.Add(-1)
		l.dropped.Add(1)
	}
}

func (l *eventLog) writeEntry(entry *protocol.EventEntry) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.w == nil {
		return
	}
	if l.size >= maxEventFileSize {
		l.rotateLocked()
	}
	if n, err := l.w.Write(line); err != nil {
		l.err = err
	} else {
		l.size += int64(n)
	}
}

// rotateLocked moves the current log aside and starts a fresh one. Holding
// two files bounds the on-disk history at roughly twice the cap.
func (l *eventLog) rotateLocked() {
	l.flushLocked()
	l.closeFileLocked()
	_ = os.Rename(l.path, l.path+".1")
	if err := l.openFile(); err != nil {
		l.err = err
	}
}

func (l *eventLog) flushLocked() {
	if l.w == nil {
		return
	}
	if err := l.w.Flush(); err != nil {
		l.err = err
	}
}

func (l *eventLog) closeFileLocked() {
	if l.f != nil {
		if err := l.f.Close(); err != nil && l.err == nil {
			l.err = err
		}
	}
	l.f = nil
	l.w = nil
}

// flushNow blocks until every queued write reaches the buffer and the buffer
// reaches the file. Tests use it; Close drains through the closed channel.
func (l *eventLog) flushNow() error {
	// Wait for the writer to consume everything queued before the request.
	// The channel alone cannot order a flush request behind entries already
	// in it: select may pick either.
	for l.queued.Load() > 0 {
		time.Sleep(time.Millisecond)
	}
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return nil
	}
	req := make(chan error, 1)
	l.flushReq <- req // the writer is alive while we hold RLock
	l.mu.RUnlock()
	return <-req
}

func (l *eventLog) close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.ch)
	l.mu.Unlock()
	l.wg.Wait()
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return l.err
}

// readEvents parses one log file, skipping torn or malformed lines.
func readEvents(path string) ([]*protocol.EventEntry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []*protocol.EventEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry protocol.EventEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		out = append(out, &entry)
	}
	return out, scanner.Err()
}
