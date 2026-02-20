package storage

import "database/sql"

// ChatSummary holds chat metadata with last message preview.
type ChatSummary struct {
	ChatID             int64
	ChatTitle          *string
	ChatType           string
	LastMessageTime    string
	LastMessagePreview *string
}

// UpsertChat creates or updates a chat record.
func (d *Database) UpsertChat(chatID int64, title *string, chatType string) error {
	_, err := d.exec(
		`INSERT INTO chats (chat_id, chat_title, chat_type, last_message_time)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET
		   chat_title = COALESCE(excluded.chat_title, chat_title),
		   last_message_time = excluded.last_message_time`,
		chatID, title, chatType, nowRFC3339(),
	)
	return err
}

// ResolveOrCreateChatID finds an existing chat by channel+external_id or creates one.
func (d *Database) ResolveOrCreateChatID(channel, externalID string, title *string, chatType string) (int64, error) {
	var chatID int64
	err := d.queryRow(
		`SELECT chat_id FROM chats WHERE channel = ? AND external_chat_id = ?`,
		channel, externalID,
	).Scan(&chatID)

	if err == nil {
		// Update title and last_message_time.
		d.exec(
			`UPDATE chats SET chat_title = COALESCE(?, chat_title), last_message_time = ? WHERE chat_id = ?`,
			title, nowRFC3339(), chatID,
		)
		return chatID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Create new chat. Use a hash of channel+externalID for deterministic ID.
	// In practice we let SQLite auto-generate via MAX+1, matching Rust behavior.
	var maxID sql.NullInt64
	d.queryRow(`SELECT MAX(chat_id) FROM chats`).Scan(&maxID)
	chatID = 1
	if maxID.Valid {
		chatID = maxID.Int64 + 1
	}

	_, err = d.exec(
		`INSERT INTO chats (chat_id, chat_title, chat_type, last_message_time, channel, external_chat_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		chatID, title, chatType, nowRFC3339(), channel, externalID,
	)
	return chatID, err
}

// GetRecentChats fetches recent chats with last message preview.
func (d *Database) GetRecentChats(limit int) ([]ChatSummary, error) {
	rows, err := d.query(
		`SELECT c.chat_id, c.chat_title, c.chat_type, c.last_message_time,
		        (SELECT content FROM messages WHERE chat_id = c.chat_id ORDER BY timestamp DESC LIMIT 1)
		 FROM chats c ORDER BY c.last_message_time DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []ChatSummary
	for rows.Next() {
		var cs ChatSummary
		if err := rows.Scan(&cs.ChatID, &cs.ChatTitle, &cs.ChatType, &cs.LastMessageTime, &cs.LastMessagePreview); err != nil {
			return nil, err
		}
		chats = append(chats, cs)
	}
	return chats, rows.Err()
}

// GetChatsByType filters chats by type.
func (d *Database) GetChatsByType(chatType string, limit int) ([]ChatSummary, error) {
	rows, err := d.query(
		`SELECT chat_id, chat_title, chat_type, last_message_time
		 FROM chats WHERE chat_type = ? ORDER BY last_message_time DESC LIMIT ?`,
		chatType, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []ChatSummary
	for rows.Next() {
		var cs ChatSummary
		if err := rows.Scan(&cs.ChatID, &cs.ChatTitle, &cs.ChatType, &cs.LastMessageTime); err != nil {
			return nil, err
		}
		chats = append(chats, cs)
	}
	return chats, rows.Err()
}

// GetChatType returns the chat_type for a given chat ID.
func (d *Database) GetChatType(chatID int64) (string, error) {
	var ct string
	err := d.queryRow(`SELECT chat_type FROM chats WHERE chat_id = ?`, chatID).Scan(&ct)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return ct, err
}

// GetChatExternalID returns the external platform ID for a chat.
func (d *Database) GetChatExternalID(chatID int64) (channel, externalID string, err error) {
	var ch, eid sql.NullString
	err = d.queryRow(
		`SELECT channel, external_chat_id FROM chats WHERE chat_id = ?`, chatID,
	).Scan(&ch, &eid)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if ch.Valid {
		channel = ch.String
	}
	if eid.Valid {
		externalID = eid.String
	}
	return channel, externalID, nil
}
