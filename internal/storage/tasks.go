package storage

import "database/sql"

// ScheduledTask represents a scheduled task.
type ScheduledTask struct {
	ID            int64
	ChatID        int64
	Prompt        string
	ScheduleType  string // "cron" or "once"
	ScheduleValue string
	NextRun       string
	LastRun       *string
	Status        string // "active", "paused", "completed", "cancelled"
	CreatedAt     string
}

// TaskRunLog represents a task execution record.
type TaskRunLog struct {
	ID            int64
	TaskID        int64
	ChatID        int64
	StartedAt     string
	FinishedAt    string
	DurationMs    int64
	Success       bool
	ResultSummary *string
}

// CreateScheduledTask inserts a new scheduled task.
func (d *Database) CreateScheduledTask(chatID int64, prompt, scheduleType, scheduleValue, nextRun string) (int64, error) {
	var id int64
	err := d.withLock(func() error {
		result, e := d.db.Exec(
			`INSERT INTO scheduled_tasks (chat_id, prompt, schedule_type, schedule_value, next_run, status, created_at)
			 VALUES (?, ?, ?, ?, ?, 'active', ?)`,
			chatID, prompt, scheduleType, scheduleValue, nextRun, nowRFC3339(),
		)
		if e != nil {
			return e
		}
		id, e = result.LastInsertId()
		return e
	})
	return id, err
}

// GetDueTasks returns active tasks whose next_run is <= now.
func (d *Database) GetDueTasks(now string) ([]ScheduledTask, error) {
	rows, err := d.query(
		`SELECT id, chat_id, prompt, schedule_type, schedule_value, next_run, last_run, status, created_at
		 FROM scheduled_tasks WHERE status = 'active' AND next_run <= ?`,
		now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// GetTasksForChat returns all active/paused tasks for a chat.
func (d *Database) GetTasksForChat(chatID int64) ([]ScheduledTask, error) {
	rows, err := d.query(
		`SELECT id, chat_id, prompt, schedule_type, schedule_value, next_run, last_run, status, created_at
		 FROM scheduled_tasks WHERE chat_id = ? AND status IN ('active', 'paused')
		 ORDER BY created_at DESC`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// GetTaskByID fetches a single task.
func (d *Database) GetTaskByID(id int64) (*ScheduledTask, error) {
	row := d.queryRow(
		`SELECT id, chat_id, prompt, schedule_type, schedule_value, next_run, last_run, status, created_at
		 FROM scheduled_tasks WHERE id = ?`, id,
	)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// UpdateTaskStatus sets the status of a task.
func (d *Database) UpdateTaskStatus(id int64, status string) error {
	_, err := d.exec(`UPDATE scheduled_tasks SET status = ? WHERE id = ?`, status, id)
	return err
}

// UpdateTaskAfterRun updates last_run and next_run after execution.
func (d *Database) UpdateTaskAfterRun(id int64, lastRun, nextRun string) error {
	_, err := d.exec(
		`UPDATE scheduled_tasks SET last_run = ?, next_run = ? WHERE id = ?`,
		lastRun, nextRun, id,
	)
	return err
}

// DeleteTask removes a task.
func (d *Database) DeleteTask(id int64) error {
	_, err := d.exec(`DELETE FROM scheduled_tasks WHERE id = ?`, id)
	return err
}

// LogTaskRun records a task execution.
func (d *Database) LogTaskRun(taskID, chatID int64, startedAt, finishedAt string, durationMs int64, success bool, summary *string) error {
	_, err := d.exec(
		`INSERT INTO task_run_logs (task_id, chat_id, started_at, finished_at, duration_ms, success, result_summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, chatID, startedAt, finishedAt, durationMs, boolToInt(success), summary,
	)
	return err
}

// GetTaskRunLogs fetches execution history for a task.
func (d *Database) GetTaskRunLogs(taskID int64, limit int) ([]TaskRunLog, error) {
	rows, err := d.query(
		`SELECT id, task_id, chat_id, started_at, finished_at, duration_ms, success, result_summary
		 FROM task_run_logs WHERE task_id = ? ORDER BY started_at DESC LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []TaskRunLog
	for rows.Next() {
		var l TaskRunLog
		var success int
		if err := rows.Scan(&l.ID, &l.TaskID, &l.ChatID, &l.StartedAt, &l.FinishedAt, &l.DurationMs, &success, &l.ResultSummary); err != nil {
			return nil, err
		}
		l.Success = success != 0
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func scanTasks(rows *sql.Rows) ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		if err := rows.Scan(&t.ID, &t.ChatID, &t.Prompt, &t.ScheduleType, &t.ScheduleValue,
			&t.NextRun, &t.LastRun, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func scanTask(row *sql.Row) (*ScheduledTask, error) {
	var t ScheduledTask
	err := row.Scan(&t.ID, &t.ChatID, &t.Prompt, &t.ScheduleType, &t.ScheduleValue,
		&t.NextRun, &t.LastRun, &t.Status, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
