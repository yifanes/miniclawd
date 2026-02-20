package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// runMigrations applies all schema migrations in order.
func (d *Database) runMigrations() error {
	return d.withLock(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// Bootstrap: create db_meta and schema_migrations tables.
		if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS db_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
			return fmt.Errorf("creating db_meta: %w", err)
		}

		if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL,
			note TEXT
		)`); err != nil {
			return fmt.Errorf("creating schema_migrations: %w", err)
		}

		version := d.getSchemaVersion(tx)

		migrations := []struct {
			version int
			note    string
			fn      func(*sql.Tx) error
		}{
			{1, "initial schema", migrateV1},
			{2, "chat/memory identity schema", migrateV2},
			{3, "memory reflector and injection logging", migrateV3},
			{4, "memory supersede edges", migrateV4},
			{5, "authentication", migrateV5},
			{6, "session metadata", migrateV6},
			{7, "metrics history", migrateV7},
			{8, "audit logs and api key expiration", migrateV8},
		}

		for _, m := range migrations {
			if version >= m.version {
				continue
			}
			if err = m.fn(tx); err != nil {
				return fmt.Errorf("migration v%d (%s): %w", m.version, m.note, err)
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if _, err = tx.Exec(
				`INSERT OR REPLACE INTO schema_migrations (version, applied_at, note) VALUES (?, ?, ?)`,
				m.version, now, m.note,
			); err != nil {
				return fmt.Errorf("recording migration v%d: %w", m.version, err)
			}
			if _, err = tx.Exec(
				`INSERT OR REPLACE INTO db_meta (key, value) VALUES ('schema_version', ?)`,
				fmt.Sprintf("%d", m.version),
			); err != nil {
				return fmt.Errorf("updating schema_version: %w", err)
			}
		}

		return tx.Commit()
	})
}

func (d *Database) getSchemaVersion(tx *sql.Tx) int {
	var val string
	err := tx.QueryRow(`SELECT value FROM db_meta WHERE key = 'schema_version'`).Scan(&val)
	if err != nil {
		return 0
	}
	var v int
	fmt.Sscanf(val, "%d", &v)
	return v
}

// migrateV1 creates the initial tables.
func migrateV1(tx *sql.Tx) error {
	stmts := []string{
		// Chats
		`CREATE TABLE IF NOT EXISTS chats (
			chat_id INTEGER PRIMARY KEY,
			chat_title TEXT,
			chat_type TEXT NOT NULL DEFAULT 'private',
			last_message_time TEXT NOT NULL
		)`,

		// Messages
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT NOT NULL,
			chat_id INTEGER NOT NULL,
			sender_name TEXT NOT NULL,
			content TEXT NOT NULL,
			is_from_bot INTEGER NOT NULL DEFAULT 0,
			timestamp TEXT NOT NULL,
			PRIMARY KEY (id, chat_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_timestamp ON messages(chat_id, timestamp)`,

		// Sessions
		`CREATE TABLE IF NOT EXISTS sessions (
			chat_id INTEGER PRIMARY KEY,
			messages_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,

		// Scheduled tasks
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			prompt TEXT NOT NULL,
			schedule_type TEXT NOT NULL DEFAULT 'cron',
			schedule_value TEXT NOT NULL,
			next_run TEXT NOT NULL,
			last_run TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_status_next ON scheduled_tasks(status, next_run)`,

		// Task run logs
		`CREATE TABLE IF NOT EXISTS task_run_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			chat_id INTEGER NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			duration_ms INTEGER NOT NULL,
			success INTEGER NOT NULL DEFAULT 1,
			result_summary TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_run_logs_task_id ON task_run_logs(task_id)`,

		// LLM usage logs
		`CREATE TABLE IF NOT EXISTS llm_usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			caller_channel TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			request_kind TEXT NOT NULL DEFAULT 'agent_loop',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_usage_chat_created ON llm_usage_logs(chat_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_usage_created ON llm_usage_logs(created_at)`,

		// Memories
		`CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER,
			content TEXT NOT NULL,
			category TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			embedding_model TEXT,
			confidence REAL NOT NULL DEFAULT 0.70,
			source TEXT NOT NULL DEFAULT 'legacy',
			last_seen_at TEXT NOT NULL,
			is_archived INTEGER NOT NULL DEFAULT 0,
			archived_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_chat ON memories(chat_id)`,
	}

	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("exec %q: %w", s[:60], err)
		}
	}
	return nil
}

// migrateV2 adds channel/external_chat_id to chats and memories.
func migrateV2(tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE chats ADD COLUMN channel TEXT`,
		`ALTER TABLE chats ADD COLUMN external_chat_id TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_chats_channel_external ON chats(channel, external_chat_id)`,
		`ALTER TABLE memories ADD COLUMN chat_channel TEXT`,
		`ALTER TABLE memories ADD COLUMN external_chat_id TEXT`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV3 adds memory reflector and injection logging.
func migrateV3(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memory_reflector_state (
			chat_id INTEGER PRIMARY KEY,
			last_reflected_ts TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS memory_reflector_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			extracted_count INTEGER NOT NULL DEFAULT 0,
			inserted_count INTEGER NOT NULL DEFAULT 0,
			updated_count INTEGER NOT NULL DEFAULT 0,
			skipped_count INTEGER NOT NULL DEFAULT 0,
			dedup_method TEXT NOT NULL,
			parse_ok INTEGER NOT NULL DEFAULT 1,
			error_text TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_reflector_runs_chat_started ON memory_reflector_runs(chat_id, started_at)`,
		`CREATE TABLE IF NOT EXISTS memory_injection_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			retrieval_method TEXT NOT NULL,
			candidate_count INTEGER NOT NULL DEFAULT 0,
			selected_count INTEGER NOT NULL DEFAULT 0,
			omitted_count INTEGER NOT NULL DEFAULT 0,
			tokens_est INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_injection_logs_chat_created ON memory_injection_logs(chat_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_active_updated ON memories(is_archived, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_confidence ON memories(confidence)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV4 adds memory supersede edges.
func migrateV4(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memory_supersede_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_memory_id INTEGER NOT NULL,
			to_memory_id INTEGER NOT NULL,
			reason TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_supersede_from ON memory_supersede_edges(from_memory_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_supersede_to ON memory_supersede_edges(to_memory_id, created_at)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV5 adds authentication tables.
func migrateV5(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS auth_passwords (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			session_id TEXT PRIMARY KEY,
			label TEXT,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			prefix TEXT NOT NULL,
			created_at TEXT NOT NULL,
			revoked_at TEXT,
			last_used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS api_key_scopes (
			api_key_id INTEGER NOT NULL,
			scope TEXT NOT NULL,
			PRIMARY KEY (api_key_id, scope)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_scopes_scope ON api_key_scopes(scope)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV6 adds session metadata columns.
func migrateV6(tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE sessions ADD COLUMN parent_session_key TEXT`,
		`ALTER TABLE sessions ADD COLUMN fork_point INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent_session_key ON sessions(parent_session_key)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV7 adds metrics history table.
func migrateV7(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS metrics_history (
			timestamp_ms INTEGER PRIMARY KEY,
			llm_completions INTEGER NOT NULL DEFAULT 0,
			llm_input_tokens INTEGER NOT NULL DEFAULT 0,
			llm_output_tokens INTEGER NOT NULL DEFAULT 0,
			http_requests INTEGER NOT NULL DEFAULT 0,
			tool_executions INTEGER NOT NULL DEFAULT 0,
			mcp_calls INTEGER NOT NULL DEFAULT 0,
			active_sessions INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_history_ts ON metrics_history(timestamp_ms)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// migrateV8 adds audit logs and api key expiration.
func migrateV8(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			target TEXT,
			status TEXT NOT NULL,
			detail TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_kind_created ON audit_logs(kind, created_at DESC)`,
		`ALTER TABLE api_keys ADD COLUMN expires_at TEXT`,
		`ALTER TABLE api_keys ADD COLUMN rotated_from_key_id INTEGER`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
