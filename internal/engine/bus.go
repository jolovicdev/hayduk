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

// bus fans messages out to subscribers. Sends never block the engine: the
// producer appends to a bounded queue; one broadcaster goroutine delivers
// to each subscriber with a bounded channel, closing subscribers that fall
// behind (they reconnect and resnapshot).
type bus struct {
	mu       sync.Mutex
	cond     *sync.Cond
	queue    []protocol.ServerMessage
	subs     map[int]chan protocol.ServerMessage
	nextID   int
	stopped  bool
	overflow bool
	stopOnce sync.Once
}

func newBus() *bus {
	b := &bus{
		subs: make(map[int]chan protocol.ServerMessage),
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
	b.queue = append(b.queue, m)
	if len(b.queue) > busQueueCap {
		// bound memory now; run() closes every subscriber and drops the
		// backlog - silently shedding a few messages while subscribers
		// stay open would leave them stale forever
		b.overflow = true
		b.queue = b.queue[len(b.queue)-busQueueCap:]
	}
	b.cond.Signal()
}

func (b *bus) subscribe() (int, chan protocol.ServerMessage) {
	ch := make(chan protocol.ServerMessage, 256)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		close(ch)
		return 0, ch
	}
	b.nextID++
	b.subs[b.nextID] = ch
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
		subs := make([]chan protocol.ServerMessage, 0, len(b.subs))
		for _, ch := range b.subs {
			subs = append(subs, ch)
		}
		b.mu.Unlock()

		for _, ch := range subs {
			select {
			case ch <- m:
			default:
				b.drop(ch)
			}
		}
	}
}

func (b *bus) drop(ch chan protocol.ServerMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, c := range b.subs {
		if c == ch {
			close(ch)
			delete(b.subs, id)
			return
		}
	}
}

func (b *bus) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}
