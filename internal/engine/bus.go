package engine

import (
	"sync"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// busQueueCap bounds the producer queue. Producers can outpace the single
// broadcaster during event storms; on overflow every subscriber is closed
// and the queue cleared - clients reconnect and take a fresh snapshot
// rather than sitting on a silently lossy feed.
const busQueueCap = 8192

// busMsg is one queued broadcast stamped with the sequence assigned at
// enqueue time. The sequence is what makes snapshot handshakes safe: a
// subscriber starts at the sequence observed when it joined, so messages
// queued before it existed are skipped even if the broadcaster has not
// drained them yet.
type busMsg struct {
	seq int64
	m   protocol.ServerMessage
}

// busSub is one registered subscriber. after is the last sequence that
// predates the subscription: everything at or below it is already reflected
// in the snapshot the subscriber took (or will take) alongside joining.
type busSub struct {
	ch    chan protocol.ServerMessage
	after int64
}

// bus fans messages out to subscribers. Sends never block the engine: the
// producer appends to a bounded queue; one broadcaster goroutine delivers
// to each subscriber with a bounded channel, closing subscribers that fall
// behind (they reconnect and resnapshot).
type bus struct {
	mu       sync.Mutex
	cond     *sync.Cond
	queue    []busMsg
	subs     map[int]*busSub
	nextID   int
	seq      int64
	stopped  bool
	overflow bool
	stopOnce sync.Once
}

func newBus() *bus {
	b := &bus{
		subs: make(map[int]*busSub),
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// shutdown wakes the broadcaster and stops delivery. Safe to call once.
func (b *bus) shutdown() {
	b.stopOnce.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.stopped = true
		b.cond.Broadcast()
	})
}

func (b *bus) send(m protocol.ServerMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.seq++
	b.queue = append(b.queue, busMsg{seq: b.seq, m: m})
	if len(b.queue) > busQueueCap {
		// bound memory now; run() closes every subscriber and drops the
		// backlog - silently shedding a few messages while subscribers
		// stay open would leave them stale forever
		b.overflow = true
		b.queue = b.queue[len(b.queue)-busQueueCap:]
	}
	b.cond.Signal()
}

// subscribe registers a subscriber whose delivery starts strictly after the
// messages already queued: the returned channel never carries a broadcast
// that predates the subscription. Callers that pair the subscription with a
// state snapshot must hold the state lock across both for that guarantee to
// line up with the snapshot's contents.
func (b *bus) subscribe() (int, chan protocol.ServerMessage) {
	ch := make(chan protocol.ServerMessage, 256)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		close(ch)
		return 0, ch
	}
	b.nextID++
	b.subs[b.nextID] = &busSub{ch: ch, after: b.seq}
	return b.nextID, ch
}

func (b *bus) unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, id)
}

// run delivers queued messages until shutdown. It is the only goroutine that
// writes or closes subscriber channels.
func (b *bus) run() {
	for {
		b.mu.Lock()
		for len(b.queue) == 0 && !b.stopped && !b.overflow {
			b.cond.Wait()
		}
		if b.overflow {
			// producers outran delivery: everyone resnapshots
			b.overflow = false
			b.queue = nil
			b.mu.Unlock()
			b.closeAll()
			continue
		}
		if len(b.queue) == 0 {
			b.mu.Unlock()
			b.closeAll()
			return
		}
		m := b.queue[0]
		b.queue = b.queue[1:]
		if len(b.queue) == 0 {
			b.queue = nil // drop a burst's grown backing array once drained
		}
		subs := make([]*busSub, 0, len(b.subs))
		for _, s := range b.subs {
			subs = append(subs, s)
		}
		b.mu.Unlock()

		for _, s := range subs {
			if m.seq <= s.after {
				continue // predates the subscription; its snapshot covers it
			}
			select {
			case s.ch <- m.m:
			default:
				b.drop(s.ch)
			}
		}
	}
}

func (b *bus) drop(ch chan protocol.ServerMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.subs {
		if s.ch == ch {
			close(ch)
			delete(b.subs, id)
			return
		}
	}
}

func (b *bus) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.subs {
		close(s.ch)
		delete(b.subs, id)
	}
}
