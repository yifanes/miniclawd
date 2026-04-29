package turnqueue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// PendingMessage represents a message that arrived while an agent run was active.
type PendingMessage struct {
	SenderName string
	Content    string
	MessageID  string
	Timestamp  string
}

type chatKey struct {
	Channel string
	ChatID  int64
}

type chatSlot struct {
	mu       sync.Mutex
	busy     bool
	pending  []PendingMessage
}

// TurnGuard is returned by Acquire. Call Release() when done (typically via defer).
type TurnGuard struct {
	queue *ChatTurnQueue
	key   chatKey
	slot  *chatSlot
}

// Release marks the turn as complete.
func (g *TurnGuard) Release() {
	g.slot.mu.Lock()
	g.slot.busy = false
	g.slot.mu.Unlock()

	log.Printf("[turnqueue] chat turn released channel=%s chat_id=%d", g.key.Channel, g.key.ChatID)
}

// ChatTurnQueue ensures at most one agent run per (channel, chatID) at a time.
type ChatTurnQueue struct {
	mu         sync.Mutex
	slots      map[chatKey]*chatSlot
	maxPending int
}

// New creates a ChatTurnQueue with the given max pending messages per chat.
func New(maxPending int) *ChatTurnQueue {
	if maxPending <= 0 {
		maxPending = 20
	}
	return &ChatTurnQueue{
		slots:      make(map[chatKey]*chatSlot),
		maxPending: maxPending,
	}
}

func (q *ChatTurnQueue) getOrCreateSlot(key chatKey) *chatSlot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s, ok := q.slots[key]
	if !ok {
		s = &chatSlot{}
		q.slots[key] = s
	}
	return s
}

// TryAcquire attempts to acquire the turn for the given chat.
// If the chat is busy, the message is enqueued and (nil, false) is returned.
// If acquired, a TurnGuard is returned and the caller should defer guard.Release().
func (q *ChatTurnQueue) TryAcquire(channel string, chatID int64, msg *PendingMessage) (*TurnGuard, bool) {
	key := chatKey{Channel: channel, ChatID: chatID}
	slot := q.getOrCreateSlot(key)

	slot.mu.Lock()
	defer slot.mu.Unlock()

	if slot.busy {
		// Enqueue the message if there's room.
		if msg != nil && len(slot.pending) < q.maxPending {
			slot.pending = append(slot.pending, *msg)
			log.Printf("[turnqueue] enqueued pending message channel=%s chat_id=%d queue_size=%d",
				channel, chatID, len(slot.pending))
		} else if msg != nil {
			log.Printf("[turnqueue] dropping message (queue full) channel=%s chat_id=%d", channel, chatID)
		}
		return nil, false
	}

	slot.busy = true
	return &TurnGuard{queue: q, key: key, slot: slot}, true
}

// Acquire blocks until the turn is acquired or the context expires.
func (q *ChatTurnQueue) Acquire(ctx context.Context, channel string, chatID int64) (*TurnGuard, error) {
	key := chatKey{Channel: channel, ChatID: chatID}
	slot := q.getOrCreateSlot(key)

	for {
		slot.mu.Lock()
		if !slot.busy {
			slot.busy = true
			slot.mu.Unlock()
			return &TurnGuard{queue: q, key: key, slot: slot}, nil
		}
		slot.mu.Unlock()

		// Poll with short interval.
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("turn acquire timeout for channel=%s chat_id=%d: %w", channel, chatID, ctx.Err())
		case <-time.After(100 * time.Millisecond):
			// retry
		}
	}
}

// DrainPending returns and clears all pending messages for the given chat.
func (q *ChatTurnQueue) DrainPending(channel string, chatID int64) []PendingMessage {
	key := chatKey{Channel: channel, ChatID: chatID}
	slot := q.getOrCreateSlot(key)

	slot.mu.Lock()
	defer slot.mu.Unlock()

	if len(slot.pending) == 0 {
		return nil
	}

	msgs := slot.pending
	slot.pending = nil
	return msgs
}

// Enqueue adds a pending message for a chat that is currently busy.
func (q *ChatTurnQueue) Enqueue(channel string, chatID int64, msg PendingMessage) bool {
	key := chatKey{Channel: channel, ChatID: chatID}
	slot := q.getOrCreateSlot(key)

	slot.mu.Lock()
	defer slot.mu.Unlock()

	if len(slot.pending) >= q.maxPending {
		return false
	}
	slot.pending = append(slot.pending, msg)
	return true
}
