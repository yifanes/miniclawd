package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Memory represents a stored memory record.
type Memory struct {
	ID             int64
	ChatID         *int64
	Content        string
	Category       string
	CreatedAt      string
	UpdatedAt      string
	EmbeddingModel *string
	Confidence     float64
	Source         string
	LastSeenAt     string
	IsArchived     bool
	ArchivedAt     *string
	ChatChannel    *string
	ExternalChatID *string
}

// MemoryReflectorRun records a reflector execution.
type MemoryReflectorRun struct {
	ID             int64
	ChatID         int64
	StartedAt      string
	FinishedAt     string
	ExtractedCount int64
	InsertedCount  int64
	UpdatedCount   int64
	SkippedCount   int64
	DedupMethod    string
	ParseOK        bool
	ErrorText      *string
}

// MemoryInjectionLog records a memory injection event.
type MemoryInjectionLog struct {
	ID              int64
	ChatID          int64
	CreatedAt       string
	RetrievalMethod string
	CandidateCount  int64
	SelectedCount   int64
	OmittedCount    int64
	TokensEst       int64
}

// InsertMemory inserts a memory with default metadata.
func (d *Database) InsertMemory(chatID *int64, content, category string) (int64, error) {
	return d.InsertMemoryWithMetadata(chatID, content, category, "tool", 0.80)
}

// InsertMemoryWithMetadata inserts a memory with full control.
func (d *Database) InsertMemoryWithMetadata(chatID *int64, content, category, source string, confidence float64) (int64, error) {
	now := nowRFC3339()
	var id int64
	err := d.withLock(func() error {
		result, e := d.db.Exec(
			`INSERT INTO memories (chat_id, content, category, created_at, updated_at, confidence, source, last_seen_at, is_archived)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			chatID, content, category, now, now, confidence, source, now,
		)
		if e != nil {
			return e
		}
		id, e = result.LastInsertId()
		return e
	})
	return id, err
}

// GetMemoriesForContext fetches active memories for system prompt injection.
// Returns memories for the given chat + global (chat_id IS NULL), confidence >= 0.45.
func (d *Database) GetMemoriesForContext(chatID int64, limit int) ([]Memory, error) {
	rows, err := d.query(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories
		 WHERE is_archived = 0 AND confidence >= 0.45
		   AND (chat_id = ? OR chat_id IS NULL)
		 ORDER BY updated_at DESC LIMIT ?`,
		chatID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// GetAllMemoriesForChat fetches all memories (including archived) for a chat or global.
func (d *Database) GetAllMemoriesForChat(chatID int64) ([]Memory, error) {
	rows, err := d.query(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories
		 WHERE chat_id = ? OR chat_id IS NULL
		 ORDER BY updated_at DESC`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// SearchMemories performs keyword search on memory content.
func (d *Database) SearchMemories(chatID int64, query string, limit int) ([]Memory, error) {
	return d.SearchMemoriesWithOptions(chatID, query, limit, false, false)
}

// SearchMemoriesWithOptions provides advanced memory search.
func (d *Database) SearchMemoriesWithOptions(chatID int64, query string, limit int, includeArchived, broadRecall bool) ([]Memory, error) {
	archivedClause := "AND is_archived = 0"
	if includeArchived {
		archivedClause = ""
	}
	chatClause := "(chat_id = ? OR chat_id IS NULL)"
	if broadRecall {
		chatClause = "1=1"
	}

	q := fmt.Sprintf(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories
		 WHERE %s AND content LIKE ? %s
		 ORDER BY updated_at DESC LIMIT ?`,
		chatClause, archivedClause,
	)

	var args []any
	if !broadRecall {
		args = append(args, chatID)
	}
	args = append(args, "%"+query+"%", limit)

	rows, err := d.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// GetMemoryByID fetches a single memory.
func (d *Database) GetMemoryByID(id int64) (*Memory, error) {
	row := d.queryRow(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories WHERE id = ?`, id,
	)
	m, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// UpdateMemoryContent updates a memory's content and category, resetting metadata.
func (d *Database) UpdateMemoryContent(id int64, content, category string) error {
	now := nowRFC3339()
	_, err := d.exec(
		`UPDATE memories SET content = ?, category = ?, updated_at = ?, last_seen_at = ?,
		        is_archived = 0, archived_at = NULL, embedding_model = NULL
		 WHERE id = ?`,
		content, category, now, now, id,
	)
	return err
}

// UpdateMemoryWithMetadata updates a memory with full metadata control.
func (d *Database) UpdateMemoryWithMetadata(id int64, content, category string, confidence float64, source string) error {
	now := nowRFC3339()
	_, err := d.exec(
		`UPDATE memories SET content = ?, category = ?, confidence = ?, source = ?,
		        updated_at = ?, last_seen_at = ?, is_archived = 0, archived_at = NULL, embedding_model = NULL
		 WHERE id = ?`,
		content, category, confidence, source, now, now, id,
	)
	return err
}

// UpdateMemoryEmbeddingModel sets the embedding model used for a memory.
func (d *Database) UpdateMemoryEmbeddingModel(id int64, model string) error {
	_, err := d.exec(`UPDATE memories SET embedding_model = ? WHERE id = ?`, model, id)
	return err
}

// DeleteMemory removes a memory.
func (d *Database) DeleteMemory(id int64) error {
	_, err := d.exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// ArchiveMemory soft-deletes a memory by setting is_archived = 1.
func (d *Database) ArchiveMemory(id int64) error {
	now := nowRFC3339()
	_, err := d.exec(
		`UPDATE memories SET is_archived = 1, archived_at = ? WHERE id = ?`,
		now, id,
	)
	return err
}

// GetAllActiveMemories returns all non-archived memories (for embedding batch).
func (d *Database) GetAllActiveMemories() ([]Memory, error) {
	rows, err := d.query(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories WHERE is_archived = 0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// GetMemoriesWithoutEmbedding returns memories lacking an embedding model.
func (d *Database) GetMemoriesWithoutEmbedding() ([]Memory, error) {
	rows, err := d.query(
		`SELECT id, chat_id, content, category, created_at, updated_at, embedding_model,
		        confidence, source, last_seen_at, is_archived, archived_at, chat_channel, external_chat_id
		 FROM memories WHERE is_archived = 0 AND embedding_model IS NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// SupersedeMemory archives the old memory and creates a supersede edge.
func (d *Database) SupersedeMemory(fromID, toID int64, reason string) error {
	return d.execTx(func(tx *sql.Tx) error {
		now := nowRFC3339()
		if _, err := tx.Exec(
			`UPDATE memories SET is_archived = 1, archived_at = ? WHERE id = ?`, now, fromID,
		); err != nil {
			return err
		}
		_, err := tx.Exec(
			`INSERT INTO memory_supersede_edges (from_memory_id, to_memory_id, reason, created_at)
			 VALUES (?, ?, ?, ?)`,
			fromID, toID, reason, now,
		)
		return err
	})
}

// TouchMemoryLastSeen updates last_seen_at timestamp.
func (d *Database) TouchMemoryLastSeen(id int64) error {
	_, err := d.exec(`UPDATE memories SET last_seen_at = ? WHERE id = ?`, nowRFC3339(), id)
	return err
}

// --- Reflector operations ---

// GetReflectorCursor returns the last_reflected_ts for a chat.
func (d *Database) GetReflectorCursor(chatID int64) (string, bool, error) {
	var ts string
	err := d.queryRow(
		`SELECT last_reflected_ts FROM memory_reflector_state WHERE chat_id = ?`, chatID,
	).Scan(&ts)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return ts, err == nil, err
}

// SetReflectorCursor upserts the reflector cursor for a chat.
func (d *Database) SetReflectorCursor(chatID int64, ts string) error {
	_, err := d.exec(
		`INSERT OR REPLACE INTO memory_reflector_state (chat_id, last_reflected_ts, updated_at)
		 VALUES (?, ?, ?)`,
		chatID, ts, nowRFC3339(),
	)
	return err
}

// LogReflectorRun records a reflector execution.
func (d *Database) LogReflectorRun(chatID int64, startedAt, finishedAt string, extracted, inserted, updated, skipped int, method string, parseOK bool, errorText *string) error {
	_, err := d.exec(
		`INSERT INTO memory_reflector_runs (chat_id, started_at, finished_at, extracted_count, inserted_count, updated_count, skipped_count, dedup_method, parse_ok, error_text)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chatID, startedAt, finishedAt, extracted, inserted, updated, skipped, method, boolToInt(parseOK), errorText,
	)
	return err
}

// GetMemoryReflectorRuns fetches reflector runs for a chat.
func (d *Database) GetMemoryReflectorRuns(chatID int64, since string, limit, offset int) ([]MemoryReflectorRun, error) {
	rows, err := d.query(
		`SELECT id, chat_id, started_at, finished_at, extracted_count, inserted_count, updated_count, skipped_count, dedup_method, parse_ok, error_text
		 FROM memory_reflector_runs WHERE chat_id = ? AND started_at >= ?
		 ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		chatID, since, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []MemoryReflectorRun
	for rows.Next() {
		var r MemoryReflectorRun
		var ok int
		if err := rows.Scan(&r.ID, &r.ChatID, &r.StartedAt, &r.FinishedAt, &r.ExtractedCount,
			&r.InsertedCount, &r.UpdatedCount, &r.SkippedCount, &r.DedupMethod, &ok, &r.ErrorText); err != nil {
			return nil, err
		}
		r.ParseOK = ok != 0
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// --- Injection logging ---

// LogMemoryInjection records a memory injection event.
func (d *Database) LogMemoryInjection(chatID int64, method string, candidates, selected, omitted, tokensEst int) error {
	_, err := d.exec(
		`INSERT INTO memory_injection_logs (chat_id, created_at, retrieval_method, candidate_count, selected_count, omitted_count, tokens_est)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chatID, nowRFC3339(), method, candidates, selected, omitted, tokensEst,
	)
	return err
}

// GetMemoryInjectionLogs fetches injection events for a chat.
func (d *Database) GetMemoryInjectionLogs(chatID int64, since string, limit, offset int) ([]MemoryInjectionLog, error) {
	rows, err := d.query(
		`SELECT id, chat_id, created_at, retrieval_method, candidate_count, selected_count, omitted_count, tokens_est
		 FROM memory_injection_logs WHERE chat_id = ? AND created_at >= ?
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		chatID, since, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []MemoryInjectionLog
	for rows.Next() {
		var l MemoryInjectionLog
		if err := rows.Scan(&l.ID, &l.ChatID, &l.CreatedAt, &l.RetrievalMethod,
			&l.CandidateCount, &l.SelectedCount, &l.OmittedCount, &l.TokensEst); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// ArchiveStaleMemories archives memories older than the given duration.
func (d *Database) ArchiveStaleMemories(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	result, err := d.exec(
		`UPDATE memories SET is_archived = 1, archived_at = ?
		 WHERE is_archived = 0 AND last_seen_at < ?`,
		nowRFC3339(), cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	var mems []Memory
	for rows.Next() {
		m, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, *m)
	}
	return mems, rows.Err()
}

func scanMemoryFromRows(rows *sql.Rows) (*Memory, error) {
	var m Memory
	var archived int
	if err := rows.Scan(&m.ID, &m.ChatID, &m.Content, &m.Category, &m.CreatedAt, &m.UpdatedAt,
		&m.EmbeddingModel, &m.Confidence, &m.Source, &m.LastSeenAt, &archived, &m.ArchivedAt,
		&m.ChatChannel, &m.ExternalChatID); err != nil {
		return nil, err
	}
	m.IsArchived = archived != 0
	return &m, nil
}

func scanMemory(row *sql.Row) (*Memory, error) {
	var m Memory
	var archived int
	err := row.Scan(&m.ID, &m.ChatID, &m.Content, &m.Category, &m.CreatedAt, &m.UpdatedAt,
		&m.EmbeddingModel, &m.Confidence, &m.Source, &m.LastSeenAt, &archived, &m.ArchivedAt,
		&m.ChatChannel, &m.ExternalChatID)
	if err != nil {
		return nil, err
	}
	m.IsArchived = archived != 0
	return &m, nil
}
