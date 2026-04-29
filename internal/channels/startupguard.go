package channels

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	recentDuplicateTTLMs     = 10 * 60 * 1000  // 10 minutes
	maxIDsPerChannel         = 20_000
)

var (
	guardMu          sync.Mutex
	channelStartMs   = make(map[string]int64)
	recentMsgIDs     = make(map[string]map[string]int64)
)

// MarkChannelStarted records the current time as the channel's boot timestamp.
// Messages with timestamps before this are considered stale and will be dropped.
func MarkChannelStarted(channel string) {
	guardMu.Lock()
	defer guardMu.Unlock()
	channelStartMs[channel] = time.Now().UnixMilli()
}

// ShouldDropPreStartMessage returns true if the message timestamp predates
// the channel's startup time, indicating a stale replayed message.
func ShouldDropPreStartMessage(channel, messageID string, messageTimeMs int64) bool {
	guardMu.Lock()
	startMs, ok := channelStartMs[channel]
	guardMu.Unlock()

	if !ok {
		return false
	}
	if messageTimeMs < startMs {
		log.Printf("[startup_guard] dropping pre-start message channel=%s message_id=%s msg_ms=%d startup_ms=%d",
			channel, messageID, messageTimeMs, startMs)
		return true
	}
	return false
}

// ShouldDropRecentDuplicate returns true if the same messageID was seen within
// the recent duplicate TTL window, preventing double-processing.
func ShouldDropRecentDuplicate(channel, messageID string) bool {
	if messageID == "" {
		return false
	}

	nowMs := time.Now().UnixMilli()

	guardMu.Lock()
	defer guardMu.Unlock()

	ids, ok := recentMsgIDs[channel]
	if !ok {
		ids = make(map[string]int64)
		recentMsgIDs[channel] = ids
	}

	if lastSeenMs, exists := ids[messageID]; exists {
		if nowMs-lastSeenMs <= recentDuplicateTTLMs {
			log.Printf("[startup_guard] dropping duplicate message channel=%s message_id=%s last_seen_ms=%d now_ms=%d",
				channel, messageID, lastSeenMs, nowMs)
			return true
		}
	}

	ids[messageID] = nowMs

	// Prune if exceeding max to bound memory.
	if len(ids) > maxIDsPerChannel {
		for k, seenMs := range ids {
			if nowMs-seenMs > recentDuplicateTTLMs {
				delete(ids, k)
			}
		}
	}

	return false
}

// TelegramMessageTimeMs converts a Telegram message date (unix seconds) to milliseconds.
func TelegramMessageTimeMs(dateUnixSec int) int64 {
	return int64(dateUnixSec) * 1000
}

// FormatTelegramMsgID formats a Telegram message ID for the startup guard.
func FormatTelegramMsgID(msgID int) string {
	return fmt.Sprintf("tg_%d", msgID)
}

// FormatDiscordMsgID formats a Discord message ID for the startup guard.
func FormatDiscordMsgID(msgID string) string {
	return "dc_" + msgID
}
