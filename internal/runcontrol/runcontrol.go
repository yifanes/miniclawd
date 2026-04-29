package runcontrol

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
)

// StoppedText is the message sent when a run is aborted via /stop.
const StoppedText = "Current run aborted."

type chatKey struct {
	Channel string
	ChatID  int64
}

type activeRun struct {
	RunID           uint64
	SourceMessageID string
	Cancel          context.CancelFunc
}

var (
	nextRunID              uint64
	mu                     sync.Mutex
	activeRuns             = make(map[chatKey][]activeRun)
	abortedSourceMessageIDs = make(map[chatKey]map[string]struct{})
)

// RegisterRun registers a new agent run for the given chat and returns a
// cancellable context. The caller should defer UnregisterRun.
func RegisterRun(parentCtx context.Context, channel string, chatID int64, sourceMessageID string) (runID uint64, ctx context.Context, cancel context.CancelFunc) {
	runID = atomic.AddUint64(&nextRunID, 1)
	ctx, cancel = context.WithCancel(parentCtx)

	key := chatKey{Channel: channel, ChatID: chatID}
	mu.Lock()
	activeRuns[key] = append(activeRuns[key], activeRun{
		RunID:           runID,
		SourceMessageID: sourceMessageID,
		Cancel:          cancel,
	})
	mu.Unlock()

	return runID, ctx, cancel
}

// UnregisterRun removes a completed run from the active set.
func UnregisterRun(channel string, chatID int64, runID uint64) {
	key := chatKey{Channel: channel, ChatID: chatID}
	mu.Lock()
	defer mu.Unlock()

	runs := activeRuns[key]
	for i, r := range runs {
		if r.RunID == runID {
			activeRuns[key] = append(runs[:i], runs[i+1:]...)
			break
		}
	}
	if len(activeRuns[key]) == 0 {
		delete(activeRuns, key)
	}
}

// AbortRuns cancels all active runs for the given chat and returns the count.
func AbortRuns(channel string, chatID int64) int {
	key := chatKey{Channel: channel, ChatID: chatID}

	mu.Lock()
	runs := activeRuns[key]
	delete(activeRuns, key)

	// Record aborted source message IDs.
	if len(runs) > 0 {
		if abortedSourceMessageIDs[key] == nil {
			abortedSourceMessageIDs[key] = make(map[string]struct{})
		}
		for _, r := range runs {
			if r.SourceMessageID != "" {
				abortedSourceMessageIDs[key][r.SourceMessageID] = struct{}{}
			}
		}
	}
	mu.Unlock()

	// Cancel all runs outside the lock.
	for _, r := range runs {
		r.Cancel()
	}

	if len(runs) > 0 {
		log.Printf("[run_control] abort_runs channel=%s chat_id=%d count=%d", channel, chatID, len(runs))
	}

	return len(runs)
}

// IsAbortedSourceMessage returns true if the given message ID was part of an aborted run.
func IsAbortedSourceMessage(channel string, chatID int64, messageID string) bool {
	key := chatKey{Channel: channel, ChatID: chatID}
	mu.Lock()
	defer mu.Unlock()

	ids, ok := abortedSourceMessageIDs[key]
	if !ok {
		return false
	}
	_, aborted := ids[messageID]
	return aborted
}
