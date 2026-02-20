package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/robfig/cron/v3"
)

// --- schedule_task ---

type ScheduleTaskTool struct {
	db       *storage.Database
	timezone string
}

func NewScheduleTaskTool(db *storage.Database, timezone string) *ScheduleTaskTool {
	return &ScheduleTaskTool{db: db, timezone: timezone}
}

func (t *ScheduleTaskTool) Name() string { return "schedule_task" }

func (t *ScheduleTaskTool) Definition() core.ToolDefinition {
	return MakeDef("schedule_task",
		"Schedule a task for future execution. Supports cron (6-field: sec min hour dom month dow) or one-time ISO 8601 timestamp.",
		map[string]any{
			"chat_id":        IntProp("Chat ID where results will be sent"),
			"prompt":         StringProp("Instruction to execute at scheduled time"),
			"schedule_type":  EnumProp("Schedule type", []string{"cron", "once"}),
			"schedule_value": StringProp("6-field cron expression or ISO 8601 timestamp"),
			"timezone":       StringProp("IANA timezone (default: config timezone)"),
		},
		[]string{"chat_id", "prompt", "schedule_type", "schedule_value"},
	)
}

func (t *ScheduleTaskTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ChatID        int64   `json:"chat_id"`
		Prompt        string  `json:"prompt"`
		ScheduleType  string  `json:"schedule_type"`
		ScheduleValue string  `json:"schedule_value"`
		Timezone      *string `json:"timezone"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	auth := ExtractAuthContext(input)
	if auth != nil && !auth.CanAccessChat(params.ChatID) {
		return Error("you don't have access to this chat")
	}

	tz := t.timezone
	if params.Timezone != nil && *params.Timezone != "" {
		tz = *params.Timezone
	}

	var nextRun string
	switch params.ScheduleType {
	case "cron":
		nr, err := computeNextCron(params.ScheduleValue, tz)
		if err != nil {
			return Error(fmt.Sprintf("invalid cron: %v", err))
		}
		nextRun = nr
	case "once":
		parsed, err := time.Parse(time.RFC3339, params.ScheduleValue)
		if err != nil {
			return Error(fmt.Sprintf("invalid timestamp: %v", err))
		}
		nextRun = parsed.UTC().Format(time.RFC3339)
	default:
		return Error("schedule_type must be 'cron' or 'once'")
	}

	id, err := t.db.CreateScheduledTask(params.ChatID, params.Prompt, params.ScheduleType, params.ScheduleValue, nextRun)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}

	return Success(fmt.Sprintf("Task #%d created. Next run: %s", id, nextRun))
}

// --- list_scheduled_tasks ---

type ListScheduledTasksTool struct {
	db *storage.Database
}

func NewListScheduledTasksTool(db *storage.Database) *ListScheduledTasksTool {
	return &ListScheduledTasksTool{db: db}
}

func (t *ListScheduledTasksTool) Name() string { return "list_scheduled_tasks" }

func (t *ListScheduledTasksTool) Definition() core.ToolDefinition {
	return MakeDef("list_scheduled_tasks",
		"List all active and paused scheduled tasks for a chat.",
		map[string]any{
			"chat_id": IntProp("Chat ID to list tasks for"),
		},
		[]string{"chat_id"},
	)
}

func (t *ListScheduledTasksTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	tasks, err := t.db.GetTasksForChat(params.ChatID)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}

	if len(tasks) == 0 {
		return Success("No scheduled tasks found.")
	}

	var sb strings.Builder
	for _, task := range tasks {
		sb.WriteString(fmt.Sprintf("#%d [%s] %s=%s next=%s prompt=%q\n",
			task.ID, task.Status, task.ScheduleType, task.ScheduleValue, task.NextRun, task.Prompt))
	}
	return Success(sb.String())
}

// --- pause_scheduled_task ---

type PauseScheduledTaskTool struct {
	db *storage.Database
}

func NewPauseScheduledTaskTool(db *storage.Database) *PauseScheduledTaskTool {
	return &PauseScheduledTaskTool{db: db}
}

func (t *PauseScheduledTaskTool) Name() string { return "pause_scheduled_task" }

func (t *PauseScheduledTaskTool) Definition() core.ToolDefinition {
	return MakeDef("pause_scheduled_task", "Pause a scheduled task.",
		map[string]any{"task_id": IntProp("Task ID to pause")}, []string{"task_id"})
}

func (t *PauseScheduledTaskTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if err := t.db.UpdateTaskStatus(params.TaskID, "paused"); err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}
	return Success(fmt.Sprintf("Task #%d paused.", params.TaskID))
}

// --- resume_scheduled_task ---

type ResumeScheduledTaskTool struct {
	db       *storage.Database
	timezone string
}

func NewResumeScheduledTaskTool(db *storage.Database, timezone string) *ResumeScheduledTaskTool {
	return &ResumeScheduledTaskTool{db: db, timezone: timezone}
}

func (t *ResumeScheduledTaskTool) Name() string { return "resume_scheduled_task" }

func (t *ResumeScheduledTaskTool) Definition() core.ToolDefinition {
	return MakeDef("resume_scheduled_task", "Resume a paused scheduled task.",
		map[string]any{"task_id": IntProp("Task ID to resume")}, []string{"task_id"})
}

func (t *ResumeScheduledTaskTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	task, err := t.db.GetTaskByID(params.TaskID)
	if err != nil || task == nil {
		return Error("task not found")
	}

	if task.ScheduleType == "cron" {
		nextRun, err := computeNextCron(task.ScheduleValue, t.timezone)
		if err == nil {
			t.db.UpdateTaskAfterRun(params.TaskID, task.NextRun, nextRun)
		}
	}

	if err := t.db.UpdateTaskStatus(params.TaskID, "active"); err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}
	return Success(fmt.Sprintf("Task #%d resumed.", params.TaskID))
}

// --- cancel_scheduled_task ---

type CancelScheduledTaskTool struct {
	db *storage.Database
}

func NewCancelScheduledTaskTool(db *storage.Database) *CancelScheduledTaskTool {
	return &CancelScheduledTaskTool{db: db}
}

func (t *CancelScheduledTaskTool) Name() string { return "cancel_scheduled_task" }

func (t *CancelScheduledTaskTool) Definition() core.ToolDefinition {
	return MakeDef("cancel_scheduled_task", "Cancel (delete) a scheduled task.",
		map[string]any{"task_id": IntProp("Task ID to cancel")}, []string{"task_id"})
}

func (t *CancelScheduledTaskTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if err := t.db.DeleteTask(params.TaskID); err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}
	return Success(fmt.Sprintf("Task #%d cancelled.", params.TaskID))
}

// --- get_scheduled_task_history ---

type GetScheduledTaskHistoryTool struct {
	db *storage.Database
}

func NewGetScheduledTaskHistoryTool(db *storage.Database) *GetScheduledTaskHistoryTool {
	return &GetScheduledTaskHistoryTool{db: db}
}

func (t *GetScheduledTaskHistoryTool) Name() string { return "get_scheduled_task_history" }

func (t *GetScheduledTaskHistoryTool) Definition() core.ToolDefinition {
	return MakeDef("get_scheduled_task_history", "Get execution history for a scheduled task.",
		map[string]any{"task_id": IntProp("Task ID to get history for")}, []string{"task_id"})
}

func (t *GetScheduledTaskHistoryTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	logs, err := t.db.GetTaskRunLogs(params.TaskID, 20)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}

	if len(logs) == 0 {
		return Success("No execution history found.")
	}

	var sb strings.Builder
	for _, l := range logs {
		status := "success"
		if !l.Success {
			status = "failed"
		}
		summary := ""
		if l.ResultSummary != nil {
			summary = *l.ResultSummary
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
		}
		sb.WriteString(fmt.Sprintf("[%s] %s (%dms) %s\n", l.StartedAt, status, l.DurationMs, summary))
	}
	return Success(sb.String())
}

// ComputeNextCronExported is the exported version for use by the scheduler.
func ComputeNextCronExported(expr, tz string) (string, error) {
	return computeNextCron(expr, tz)
}

// computeNextCron parses a 6-field cron expression and returns the next run time.
func computeNextCron(expr, tz string) (string, error) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return "", fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	next := schedule.Next(now)
	return next.UTC().Format(time.RFC3339), nil
}
