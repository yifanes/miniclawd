package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// KnowledgeGraphQueryTool queries the temporal knowledge graph.
type KnowledgeGraphQueryTool struct {
	db *storage.Database
}

func NewKnowledgeGraphQueryTool(db *storage.Database) *KnowledgeGraphQueryTool {
	return &KnowledgeGraphQueryTool{db: db}
}

func (t *KnowledgeGraphQueryTool) Name() string { return "knowledge_graph_query" }

func (t *KnowledgeGraphQueryTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "knowledge_graph_query",
		Description: "Query the temporal knowledge graph for entity relationships. Returns triples (subject-predicate-object) with temporal validity. Use 'as_of' for point-in-time queries. Use 'timeline' mode to see how facts changed over time.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"entity": {
					"type": "string",
					"description": "The entity (subject or object) to query"
				},
				"mode": {
					"type": "string",
					"enum": ["subject", "object", "timeline", "stats"],
					"description": "Query mode: 'subject' (find facts about entity), 'object' (reverse lookup), 'timeline' (all historical facts), 'stats' (graph statistics)"
				},
				"as_of": {
					"type": "string",
					"description": "Optional ISO 8601 timestamp for point-in-time query (only for subject mode)"
				},
				"chat_id": {
					"type": "integer",
					"description": "Optional chat_id to scope the query"
				}
			},
			"required": ["entity", "mode"]
		}`),
	}
}

func (t *KnowledgeGraphQueryTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Entity string  `json:"entity"`
		Mode   string  `json:"mode"`
		AsOf   *string `json:"as_of"`
		ChatID *int64  `json:"chat_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	entity := strings.TrimSpace(params.Entity)
	if entity == "" {
		return Error("entity is required")
	}

	switch params.Mode {
	case "subject":
		triples, err := t.db.KGQuerySubject(entity, params.ChatID, params.AsOf)
		if err != nil {
			return Error("query error: " + err.Error())
		}
		return Success(storage.FormatTriples(triples))

	case "object":
		triples, err := t.db.KGQueryObject(entity, params.ChatID)
		if err != nil {
			return Error("query error: " + err.Error())
		}
		return Success(storage.FormatTriples(triples))

	case "timeline":
		triples, err := t.db.KGQueryTimeline(entity, params.ChatID)
		if err != nil {
			return Error("query error: " + err.Error())
		}
		return Success(storage.FormatTriples(triples))

	case "stats":
		total, active, err := t.db.KGStats(params.ChatID)
		if err != nil {
			return Error("stats error: " + err.Error())
		}
		return Success(fmt.Sprintf("Total triples: %d\nActive triples: %d\nSuperseded triples: %d",
			total, active, total-active))

	default:
		return Error("unknown mode: " + params.Mode + " (use subject, object, timeline, or stats)")
	}
}

// KnowledgeGraphAddTool adds triples to the knowledge graph.
type KnowledgeGraphAddTool struct {
	db *storage.Database
}

func NewKnowledgeGraphAddTool(db *storage.Database) *KnowledgeGraphAddTool {
	return &KnowledgeGraphAddTool{db: db}
}

func (t *KnowledgeGraphAddTool) Name() string { return "knowledge_graph_add" }

func (t *KnowledgeGraphAddTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "knowledge_graph_add",
		Description: "Add a fact (subject-predicate-object triple) to the knowledge graph. If a fact with the same subject+predicate already exists, it is automatically superseded (temporal versioning).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"subject": {
					"type": "string",
					"description": "The subject entity"
				},
				"predicate": {
					"type": "string",
					"description": "The relationship/predicate"
				},
				"object": {
					"type": "string",
					"description": "The object entity or value"
				},
				"chat_id": {
					"type": "integer",
					"description": "Optional chat_id to scope the triple"
				},
				"source": {
					"type": "string",
					"description": "Optional source description for provenance"
				}
			},
			"required": ["subject", "predicate", "object"]
		}`),
	}
}

func (t *KnowledgeGraphAddTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Subject   string  `json:"subject"`
		Predicate string  `json:"predicate"`
		Object    string  `json:"object"`
		ChatID    *int64  `json:"chat_id"`
		Source    string  `json:"source"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	subject := strings.TrimSpace(params.Subject)
	predicate := strings.TrimSpace(params.Predicate)
	object := strings.TrimSpace(params.Object)

	if subject == "" || predicate == "" || object == "" {
		return Error("subject, predicate, and object are all required")
	}

	source := params.Source
	if source == "" {
		source = "agent"
	}

	if err := t.db.KGAddTriple(params.ChatID, subject, predicate, object, source); err != nil {
		return Error("failed to add triple: " + err.Error())
	}

	return Success(fmt.Sprintf("Added: %s → %s → %s", subject, predicate, object))
}
