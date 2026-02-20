package storage

import "database/sql"

// LLMUsageSummary aggregates token usage stats.
type LLMUsageSummary struct {
	Requests      int64
	InputTokens   int64
	OutputTokens  int64
	TotalTokens   int64
	LastRequestAt *string
}

// LLMUsageByModel groups usage by model.
type LLMUsageByModel struct {
	Model        string
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// LogLLMUsage records an LLM API call.
func (d *Database) LogLLMUsage(chatID int64, channel, provider, model string, inputTokens, outputTokens int, kind string) error {
	total := inputTokens + outputTokens
	_, err := d.exec(
		`INSERT INTO llm_usage_logs (chat_id, caller_channel, provider, model, input_tokens, output_tokens, total_tokens, request_kind, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chatID, channel, provider, model, inputTokens, outputTokens, total, kind, nowRFC3339(),
	)
	return err
}

// GetLLMUsageSummary returns all-time or per-chat usage summary.
// Pass chatID=0 for global summary.
func (d *Database) GetLLMUsageSummary(chatID int64) (*LLMUsageSummary, error) {
	var q string
	var args []any
	if chatID > 0 {
		q = `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		            COALESCE(SUM(total_tokens),0), MAX(created_at)
		     FROM llm_usage_logs WHERE chat_id = ?`
		args = []any{chatID}
	} else {
		q = `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		            COALESCE(SUM(total_tokens),0), MAX(created_at)
		     FROM llm_usage_logs`
	}

	var s LLMUsageSummary
	err := d.queryRow(q, args...).Scan(&s.Requests, &s.InputTokens, &s.OutputTokens, &s.TotalTokens, &s.LastRequestAt)
	if err == sql.ErrNoRows {
		return &LLMUsageSummary{}, nil
	}
	return &s, err
}

// GetLLMUsageSummarySince returns usage summary filtered by timestamp.
func (d *Database) GetLLMUsageSummarySince(chatID int64, since string) (*LLMUsageSummary, error) {
	var q string
	var args []any
	if chatID > 0 {
		q = `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		            COALESCE(SUM(total_tokens),0), MAX(created_at)
		     FROM llm_usage_logs WHERE chat_id = ? AND created_at >= ?`
		args = []any{chatID, since}
	} else {
		q = `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		            COALESCE(SUM(total_tokens),0), MAX(created_at)
		     FROM llm_usage_logs WHERE created_at >= ?`
		args = []any{since}
	}

	var s LLMUsageSummary
	err := d.queryRow(q, args...).Scan(&s.Requests, &s.InputTokens, &s.OutputTokens, &s.TotalTokens, &s.LastRequestAt)
	return &s, err
}

// GetLLMUsageByModel returns usage grouped by model.
func (d *Database) GetLLMUsageByModel(chatID int64, since string, limit int) ([]LLMUsageByModel, error) {
	var q string
	var args []any
	if chatID > 0 {
		q = `SELECT model, COUNT(*), SUM(input_tokens), SUM(output_tokens), SUM(total_tokens)
		     FROM llm_usage_logs WHERE chat_id = ? AND created_at >= ?
		     GROUP BY model ORDER BY SUM(total_tokens) DESC LIMIT ?`
		args = []any{chatID, since, limit}
	} else {
		q = `SELECT model, COUNT(*), SUM(input_tokens), SUM(output_tokens), SUM(total_tokens)
		     FROM llm_usage_logs WHERE created_at >= ?
		     GROUP BY model ORDER BY SUM(total_tokens) DESC LIMIT ?`
		args = []any{since, limit}
	}

	rows, err := d.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []LLMUsageByModel
	for rows.Next() {
		var u LLMUsageByModel
		if err := rows.Scan(&u.Model, &u.Requests, &u.InputTokens, &u.OutputTokens, &u.TotalTokens); err != nil {
			return nil, err
		}
		results = append(results, u)
	}
	return results, rows.Err()
}
