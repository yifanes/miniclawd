package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/channels"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/yifanes/miniclawd/internal/tools"
)

// SpawnScheduler starts the 60-second poll loop for due tasks.
func SpawnScheduler(ctx context.Context, db *storage.Database, deps *agent.AgentDeps, registry *channels.ChannelRegistry) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		// Run immediately on start.
		runDueTasks(ctx, db, deps, registry)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runDueTasks(ctx, db, deps, registry)
			}
		}
	}()
}

func runDueTasks(ctx context.Context, db *storage.Database, deps *agent.AgentDeps, registry *channels.ChannelRegistry) {
	now := time.Now().UTC().Format(time.RFC3339)
	tasks, err := db.GetDueTasks(now)
	if err != nil {
		log.Printf("[scheduler] error fetching due tasks: %v", err)
		return
	}

	for _, task := range tasks {
		go executeTask(ctx, db, deps, registry, task)
	}
}

func executeTask(ctx context.Context, db *storage.Database, deps *agent.AgentDeps, registry *channels.ChannelRegistry, task storage.ScheduledTask) {
	startedAt := time.Now().UTC()

	// Get chat routing.
	chatType, err := db.GetChatType(task.ChatID)
	if err != nil || chatType == "" {
		log.Printf("[scheduler] cannot resolve chat type for task #%d (chat %d): %v", task.ID, task.ChatID, err)
		return
	}

	reqCtx := agent.AgentRequestContext{
		CallerChannel: channels.SessionSourceForChat(chatType),
		ChatID:        task.ChatID,
		ChatType:      chatType,
	}

	prompt := task.Prompt
	response, err := agent.ProcessWithAgent(ctx, deps, reqCtx, &prompt, nil)

	finishedAt := time.Now().UTC()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	success := err == nil

	var summary *string
	if response != "" {
		s := response
		if len(s) > 500 {
			s = s[:500] + "..."
		}
		summary = &s
	}

	// Log the run.
	db.LogTaskRun(task.ID, task.ChatID, startedAt.Format(time.RFC3339), finishedAt.Format(time.RFC3339), durationMs, success, summary)

	// Deliver response if successful.
	if success && response != "" {
		deliverResponse(ctx, db, registry, task.ChatID, response, deps.Config.BotUsername)
	}

	// Compute next run.
	if task.ScheduleType == "cron" {
		nextRun, err := tools.ComputeNextCronExported(task.ScheduleValue, deps.Config.Timezone)
		if err == nil {
			db.UpdateTaskAfterRun(task.ID, finishedAt.Format(time.RFC3339), nextRun)
		}
	} else {
		// One-shot: mark as completed.
		db.UpdateTaskStatus(task.ID, "completed")
	}
}

func deliverResponse(ctx context.Context, db *storage.Database, registry *channels.ChannelRegistry, chatID int64, text, botUsername string) {
	chatType, _ := db.GetChatType(chatID)
	adapter, _, ok := registry.Resolve(chatType)
	if !ok || adapter == nil || adapter.IsLocalOnly() {
		return
	}

	_, externalID, _ := db.GetChatExternalID(chatID)
	if externalID == "" {
		return
	}

	chunks := core.SplitText(text, 4096)
	for _, chunk := range chunks {
		if err := adapter.SendText(ctx, externalID, chunk); err != nil {
			log.Printf("[scheduler] delivery error for chat %d: %v", chatID, err)
		}
	}

	// Store bot message.
	db.StoreMessage(storage.StoredMessage{
		ID:         "sched_" + time.Now().UTC().Format("20060102150405"),
		ChatID:     chatID,
		SenderName: botUsername,
		Content:    text,
		IsFromBot:  true,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}
