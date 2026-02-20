package storage

// MetricsPoint represents a metrics snapshot.
type MetricsPoint struct {
	TimestampMs    int64
	LLMCompletions int64
	LLMInputTokens int64
	LLMOutputTokens int64
	HTTPRequests   int64
	ToolExecutions int64
	MCPCalls       int64
	ActiveSessions int64
}

// AuditLogRecord represents an audit log entry.
type AuditLogRecord struct {
	ID        int64
	Kind      string
	Actor     string
	Action    string
	Target    *string
	Status    string
	Detail    *string
	CreatedAt string
}

// UpsertMetricsHistory inserts or updates a metrics snapshot.
func (d *Database) UpsertMetricsHistory(p MetricsPoint) error {
	_, err := d.exec(
		`INSERT INTO metrics_history (timestamp_ms, llm_completions, llm_input_tokens, llm_output_tokens,
		        http_requests, tool_executions, mcp_calls, active_sessions)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(timestamp_ms) DO UPDATE SET
		   llm_completions = excluded.llm_completions,
		   llm_input_tokens = excluded.llm_input_tokens,
		   llm_output_tokens = excluded.llm_output_tokens,
		   http_requests = excluded.http_requests,
		   tool_executions = excluded.tool_executions,
		   mcp_calls = excluded.mcp_calls,
		   active_sessions = excluded.active_sessions`,
		p.TimestampMs, p.LLMCompletions, p.LLMInputTokens, p.LLMOutputTokens,
		p.HTTPRequests, p.ToolExecutions, p.MCPCalls, p.ActiveSessions,
	)
	return err
}

// GetMetricsHistory fetches metrics snapshots since the given timestamp.
func (d *Database) GetMetricsHistory(sinceMs int64, limit int) ([]MetricsPoint, error) {
	rows, err := d.query(
		`SELECT timestamp_ms, llm_completions, llm_input_tokens, llm_output_tokens,
		        http_requests, tool_executions, mcp_calls, active_sessions
		 FROM metrics_history WHERE timestamp_ms >= ? ORDER BY timestamp_ms DESC LIMIT ?`,
		sinceMs, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricsPoint
	for rows.Next() {
		var p MetricsPoint
		if err := rows.Scan(&p.TimestampMs, &p.LLMCompletions, &p.LLMInputTokens, &p.LLMOutputTokens,
			&p.HTTPRequests, &p.ToolExecutions, &p.MCPCalls, &p.ActiveSessions); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// LogAuditEvent records an audit log entry.
func (d *Database) LogAuditEvent(kind, actor, action string, target *string, status string, detail *string) error {
	_, err := d.exec(
		`INSERT INTO audit_logs (kind, actor, action, target, status, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		kind, actor, action, target, status, detail, nowRFC3339(),
	)
	return err
}

// ListAuditLogs fetches audit logs with optional kind filter.
func (d *Database) ListAuditLogs(kind string, limit int) ([]AuditLogRecord, error) {
	var q string
	var args []any
	if kind != "" {
		q = `SELECT id, kind, actor, action, target, status, detail, created_at
		     FROM audit_logs WHERE kind = ? ORDER BY created_at DESC LIMIT ?`
		args = []any{kind, limit}
	} else {
		q = `SELECT id, kind, actor, action, target, status, detail, created_at
		     FROM audit_logs ORDER BY created_at DESC LIMIT ?`
		args = []any{limit}
	}

	rows, err := d.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AuditLogRecord
	for rows.Next() {
		var l AuditLogRecord
		if err := rows.Scan(&l.ID, &l.Kind, &l.Actor, &l.Action, &l.Target, &l.Status, &l.Detail, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
