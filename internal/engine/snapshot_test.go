package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// The bus must skip everything queued before a subscription even when the
// broadcaster has not drained its backlog yet: without the sequence cutoff a
// freshly connected client is replayed pre-snapshot messages after the
// snapshot, which reverts newer absolute resource state (older hosts, older
// console chunks) until the next broadcast overwrites it - or forever.
//
// Deterministic setup: no broadcaster runs while the stale messages are
// queued, so they are guaranteed to still be sitting in the queue when the
// subscription joins, which is the exact interleaving the live race produces.
func TestBusSkipsBacklogQueuedBeforeSubscription(t *testing.T) {
	b := newBus()
	b.send(protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Data: "stale console chunk"})
	b.send(protocol.SessionOutputMsg{Type: protocol.KindSessionOutput, SID: "1", Data: "stale session chunk"})
	b.send(protocol.HostsUpdate(nil))

	id, ch := b.subscribe()
	defer b.unsubscribe(id)
	defer b.shutdown() // do not strand the broadcaster in cond.Wait
	b.send(protocol.OperatorsUpdate([]string{"dana"}))
	go b.run()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case m := <-ch:
			if v, ok := m.(protocol.ResourceUpdate); !ok || v.Resource != protocol.ResOperators {
				t.Fatalf("pre-subscription message replayed: %#v", m)
			}
			// the one post-subscription message arrived; the backlog must
			// never follow it either
			select {
			case m := <-ch:
				t.Fatalf("unexpected extra message: %#v", m)
			case <-time.After(50 * time.Millisecond):
				return
			}
		case <-deadline:
			t.Fatal("post-subscription message never arrived")
		}
	}
}

// SubscribeSnapshot is the WS handshake: its snapshot and its stream must
// join without a seam. While events fire concurrently, every subscription's
// snapshot holds events 1..S and its channel must deliver exactly S+1..N in
// order - no replay (nothing ≤ S), no loss (nothing missing), no duplicate.
func TestSubscribeSnapshotSeamlessUnderConcurrentBroadcasts(t *testing.T) {
	const batches, perBatch = 4, 250
	const total = batches * perBatch

	e := New(Config{SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	type handshake struct {
		snap    protocol.CampaignState
		sub     *Subscription
		mu      sync.Mutex
		seqs    []int64
		dropped bool
		done    chan struct{}
	}
	start := func() *handshake {
		sub, snap := e.SubscribeSnapshot()
		h := &handshake{snap: snap, sub: sub, done: make(chan struct{})}
		go func() {
			defer close(h.done)
			defer sub.Stop()
			for {
				m, ok := <-sub.C()
				if !ok {
					h.mu.Lock()
					h.dropped = true
					h.mu.Unlock()
					return
				}
				ev, ok := m.(protocol.EventMsg)
				if !ok {
					t.Errorf("unexpected non-event message %#v", m)
					return
				}
				h.mu.Lock()
				h.seqs = append(h.seqs, ev.Seq)
				h.mu.Unlock()
				if ev.Text == "sentinel" {
					return // everything before it has arrived
				}
			}
		}()
		return h
	}

	handshakes := []*handshake{start(), start(), start()}

	var wg sync.WaitGroup
	for g := 0; g < batches; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perBatch; j++ {
				e.eventf(protocol.LevelInfo, "event")
				// a realistic broadcast rate: the bus drops subscribers that
				// fall a full queue behind, and that drop is a different (and
				// intended) behavior from the seam this test pins down
				time.Sleep(100 * time.Microsecond)
			}
		}()
		if g == batches/2 {
			// interleave a fresh handshake with the firing
			handshakes = append(handshakes, start())
		}
	}
	wg.Wait()
	e.eventf(protocol.LevelInfo, "sentinel") // seq total+1: drains every stream

	for i, h := range handshakes {
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("handshake %d: stream never drained", i)
		}
		h.mu.Lock()
		dropped, seqs := h.dropped, h.seqs
		h.mu.Unlock()
		if dropped {
			t.Fatalf("handshake %d: subscriber was dropped by the bus", i)
		}
		s := int64(len(h.snap.Events))
		if want := total + 1 - s; int64(len(seqs)) != want {
			t.Fatalf("handshake %d: snapshot held %d events, stream delivered %d, want %d", i, s, len(seqs), want)
		}
		for k, seq := range seqs {
			// events are numbered 1..N by the engine's seq; the snapshot
			// carries 1..s, so the stream must carry s+1, s+2, ... exactly
			// once, in order, through the sentinel
			if want := s + int64(k) + 1; seq != want {
				t.Fatalf("handshake %d: event %d out of order or replayed/lost (want %d)", i, seq, want)
			}
		}
	}
}
