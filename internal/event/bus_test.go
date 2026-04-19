package event

import (
	"sync"
	"testing"
	"time"
)

func TestBus_DeliversToAllSubscribers(t *testing.T) {
	b := NewBus()
	defer b.Close()
	a := b.Subscribe(4, false)
	c := b.Subscribe(4, false)

	b.Publish(StageStarted{StageID: "s1"})
	b.Publish(StageCompleted{StageID: "s1"})

	got := func(ch <-chan Event) []string {
		var kinds []string
		for i := 0; i < 2; i++ {
			select {
			case ev := <-ch:
				kinds = append(kinds, ev.Kind())
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("timed out waiting for event")
			}
		}
		return kinds
	}
	if len(got(a)) != 2 || len(got(c)) != 2 {
		t.Fatalf("expected 2 events on each subscriber")
	}
}

func TestBus_LossyDropsStreamDeltas(t *testing.T) {
	b := NewBus()
	defer b.Close()
	// buf=1, lossy. Publish two stream deltas without reading; second must drop.
	ch := b.Subscribe(1, true)

	b.Publish(MessageStreamed{StageID: "s1", Delta: "a"})
	b.Publish(MessageStreamed{StageID: "s1", Delta: "b"})

	select {
	case ev := <-ch:
		if ev.(MessageStreamed).Delta != "a" {
			t.Fatalf("unexpected delta %q", ev.(MessageStreamed).Delta)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out")
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected second event: %#v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBus_LossyStillDeliversLifecycleEvents(t *testing.T) {
	b := NewBus()
	defer b.Close()
	ch := b.Subscribe(1, true)

	// Drain via goroutine so the strict lifecycle event doesn't block.
	var wg sync.WaitGroup
	wg.Add(1)
	var kinds []string
	go func() {
		defer wg.Done()
		for i := 0; i < 2; i++ {
			ev, ok := <-ch
			if !ok {
				return
			}
			kinds = append(kinds, ev.Kind())
		}
	}()

	b.Publish(MessageStreamed{StageID: "s1", Delta: "a"})
	b.Publish(StageCompleted{StageID: "s1"})

	wg.Wait()
	if len(kinds) != 2 || kinds[1] != "stage_completed" {
		t.Fatalf("unexpected kinds: %v", kinds)
	}
}

func TestBus_CloseIsIdempotent(t *testing.T) {
	b := NewBus()
	ch := b.Subscribe(1, false)
	b.Close()
	b.Close() // no panic
	if _, ok := <-ch; ok {
		t.Fatalf("expected closed channel")
	}
	b.Publish(StageStarted{}) // no-op after close
}
