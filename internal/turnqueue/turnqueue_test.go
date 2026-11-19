package turnqueue

import "testing"

func TestTryAcquireEnqueuesWhileBusy(t *testing.T) {
	q := New(5)

	guard, ok := q.TryAcquire("discord", 1, &PendingMessage{SenderName: "a", Content: "first"})
	if !ok {
		t.Fatal("expected to acquire free chat")
	}

	if _, ok := q.TryAcquire("discord", 1, &PendingMessage{SenderName: "a", Content: "second"}); ok {
		t.Fatal("expected busy chat to enqueue instead of acquire")
	}

	// DrainPending is what makes queued messages get processed.
	pending := q.DrainPending("discord", 1)
	if len(pending) != 1 || pending[0].Content != "second" {
		t.Fatalf("pending = %+v, want one message with content %q", pending, "second")
	}
	if again := q.DrainPending("discord", 1); len(again) != 0 {
		t.Fatalf("second drain returned %d messages, want 0", len(again))
	}

	guard.Release()

	if _, ok := q.TryAcquire("discord", 1, nil); !ok {
		t.Fatal("expected chat to be acquirable after release")
	}
}

func TestTryAcquireRespectsMaxPending(t *testing.T) {
	q := New(2)
	if _, ok := q.TryAcquire("telegram", 7, nil); !ok {
		t.Fatal("expected to acquire free chat")
	}
	for i := 0; i < 5; i++ {
		q.TryAcquire("telegram", 7, &PendingMessage{Content: "x"})
	}
	if got := len(q.DrainPending("telegram", 7)); got != 2 {
		t.Fatalf("pending length = %d, want 2 (maxPending)", got)
	}
}

func TestChatsAreIndependent(t *testing.T) {
	q := New(2)
	if _, ok := q.TryAcquire("discord", 1, nil); !ok {
		t.Fatal("chat 1 should be free")
	}
	if _, ok := q.TryAcquire("discord", 2, nil); !ok {
		t.Fatal("chat 2 should be free independently of chat 1")
	}
}
