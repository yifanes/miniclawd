package storage

import "database/sql"

// SessionMeta holds session metadata for listing.
type SessionMeta struct {
	ChatID           int64
	UpdatedAt        string
	ParentSessionKey *string
	ForkPoint        *int
}

// SaveSession upserts session state.
func (d *Database) SaveSession(chatID int64, messagesJSON string) error {
	_, err := d.exec(
		`INSERT OR REPLACE INTO sessions (chat_id, messages_json, updated_at)
		 VALUES (?, ?, ?)`,
		chatID, messagesJSON, nowRFC3339(),
	)
	return err
}

// SaveSessionWithMeta upserts session state with parent/fork metadata.
func (d *Database) SaveSessionWithMeta(chatID int64, messagesJSON string, parentKey *string, forkPoint *int) error {
	_, err := d.exec(
		`INSERT OR REPLACE INTO sessions (chat_id, messages_json, updated_at, parent_session_key, fork_point)
		 VALUES (?, ?, ?, ?, ?)`,
		chatID, messagesJSON, nowRFC3339(), parentKey, forkPoint,
	)
	return err
}

// LoadSession returns the session messages JSON and updated_at timestamp.
func (d *Database) LoadSession(chatID int64) (messagesJSON string, updatedAt string, found bool, err error) {
	err = d.queryRow(
		`SELECT messages_json, updated_at FROM sessions WHERE chat_id = ?`, chatID,
	).Scan(&messagesJSON, &updatedAt)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return messagesJSON, updatedAt, true, nil
}

// LoadSessionMeta returns session with parent/fork metadata.
func (d *Database) LoadSessionMeta(chatID int64) (messagesJSON, updatedAt string, parentKey *string, forkPoint *int, found bool, err error) {
	var pk sql.NullString
	var fp sql.NullInt64
	err = d.queryRow(
		`SELECT messages_json, updated_at, parent_session_key, fork_point FROM sessions WHERE chat_id = ?`, chatID,
	).Scan(&messagesJSON, &updatedAt, &pk, &fp)
	if err == sql.ErrNoRows {
		return "", "", nil, nil, false, nil
	}
	if err != nil {
		return "", "", nil, nil, false, err
	}
	if pk.Valid {
		s := pk.String
		parentKey = &s
	}
	if fp.Valid {
		i := int(fp.Int64)
		forkPoint = &i
	}
	return messagesJSON, updatedAt, parentKey, forkPoint, true, nil
}

// ListSessionMeta returns metadata for all sessions, ordered by updated_at DESC.
func (d *Database) ListSessionMeta(limit int) ([]SessionMeta, error) {
	rows, err := d.query(
		`SELECT chat_id, updated_at, parent_session_key, fork_point
		 FROM sessions ORDER BY updated_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []SessionMeta
	for rows.Next() {
		var m SessionMeta
		var pk sql.NullString
		var fp sql.NullInt64
		if err := rows.Scan(&m.ChatID, &m.UpdatedAt, &pk, &fp); err != nil {
			return nil, err
		}
		if pk.Valid {
			s := pk.String
			m.ParentSessionKey = &s
		}
		if fp.Valid {
			i := int(fp.Int64)
			m.ForkPoint = &i
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

// DeleteSession removes a session.
func (d *Database) DeleteSession(chatID int64) error {
	_, err := d.exec(`DELETE FROM sessions WHERE chat_id = ?`, chatID)
	return err
}

// ClearChatContext deletes session + messages but keeps chat and memories.
func (d *Database) ClearChatContext(chatID int64) error {
	return d.execTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE chat_id = ?`, chatID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM messages WHERE chat_id = ?`, chatID)
		return err
	})
}

// DeleteChatData performs full cascade delete of all chat data.
func (d *Database) DeleteChatData(chatID int64) error {
	return d.execTx(func(tx *sql.Tx) error {
		tables := []string{
			"messages", "sessions", "scheduled_tasks", "memories",
			"memory_reflector_state", "llm_usage_logs",
		}
		for _, t := range tables {
			if _, err := tx.Exec("DELETE FROM "+t+" WHERE chat_id = ?", chatID); err != nil {
				return err
			}
		}
		_, err := tx.Exec("DELETE FROM chats WHERE chat_id = ?", chatID)
		return err
	})
}
