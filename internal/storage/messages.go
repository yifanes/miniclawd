package storage

import (
	"database/sql"
	"time"
)

// StoredMessage represents a persisted chat message.
type StoredMessage struct {
	ID         string
	ChatID     int64
	SenderName string
	Content    string
	IsFromBot  bool
	Timestamp  string // RFC 3339
}

// StoreMessage inserts or replaces a message.
func (d *Database) StoreMessage(msg StoredMessage) error {
	_, err := d.exec(
		`INSERT OR REPLACE INTO messages (id, chat_id, sender_name, content, is_from_bot, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ChatID, msg.SenderName, msg.Content, boolToInt(msg.IsFromBot), msg.Timestamp,
	)
	return err
}

// GetRecentMessages fetches the most recent N messages for a chat.
func (d *Database) GetRecentMessages(chatID int64, limit int) ([]StoredMessage, error) {
	rows, err := d.query(
		`SELECT id, chat_id, sender_name, content, is_from_bot, timestamp
		 FROM messages WHERE chat_id = ? ORDER BY timestamp DESC LIMIT ?`,
		chatID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var bot int
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderName, &m.Content, &bot, &m.Timestamp); err != nil {
			return nil, err
		}
		m.IsFromBot = bot != 0
		msgs = append(msgs, m)
	}
	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

// GetAllMessages fetches all messages for a chat in chronological order.
func (d *Database) GetAllMessages(chatID int64) ([]StoredMessage, error) {
	rows, err := d.query(
		`SELECT id, chat_id, sender_name, content, is_from_bot, timestamp
		 FROM messages WHERE chat_id = ? ORDER BY timestamp ASC`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// GetMessagesSinceLastBotResponse gets all messages since the bot's last reply
// (group catch-up), up to max. Falls back to fallback if no bot message found.
func (d *Database) GetMessagesSinceLastBotResponse(chatID int64, max, fallback int) ([]StoredMessage, error) {
	// Find the timestamp of the last bot message.
	var lastBotTS sql.NullString
	d.queryRow(
		`SELECT timestamp FROM messages WHERE chat_id = ? AND is_from_bot = 1
		 ORDER BY timestamp DESC LIMIT 1`,
		chatID,
	).Scan(&lastBotTS)

	if !lastBotTS.Valid {
		return d.GetRecentMessages(chatID, fallback)
	}

	rows, err := d.query(
		`SELECT id, chat_id, sender_name, content, is_from_bot, timestamp
		 FROM messages WHERE chat_id = ? AND timestamp > ? ORDER BY timestamp ASC LIMIT ?`,
		chatID, lastBotTS.String, max,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// GetMessagesSince fetches messages after a timestamp.
func (d *Database) GetMessagesSince(chatID int64, since string, limit int) ([]StoredMessage, error) {
	rows, err := d.query(
		`SELECT id, chat_id, sender_name, content, is_from_bot, timestamp
		 FROM messages WHERE chat_id = ? AND timestamp > ? ORDER BY timestamp ASC LIMIT ?`,
		chatID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// GetNewUserMessagesSince fetches non-bot messages after a timestamp.
func (d *Database) GetNewUserMessagesSince(chatID int64, since string, limit int) ([]StoredMessage, error) {
	rows, err := d.query(
		`SELECT id, chat_id, sender_name, content, is_from_bot, timestamp
		 FROM messages WHERE chat_id = ? AND timestamp > ? AND is_from_bot = 0
		 ORDER BY timestamp ASC LIMIT ?`,
		chatID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// GetActiveChatIDsSince returns chat IDs with activity after the given time.
func (d *Database) GetActiveChatIDsSince(since string) ([]int64, error) {
	rows, err := d.query(
		`SELECT DISTINCT chat_id FROM messages WHERE timestamp > ?`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanMessages(rows *sql.Rows) ([]StoredMessage, error) {
	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var bot int
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderName, &m.Content, &bot, &m.Timestamp); err != nil {
			return nil, err
		}
		m.IsFromBot = bot != 0
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
