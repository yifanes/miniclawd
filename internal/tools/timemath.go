package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

// GetCurrentTimeTool returns the current time in the configured timezone.
type GetCurrentTimeTool struct {
	timezone string
}

func NewGetCurrentTimeTool(timezone string) *GetCurrentTimeTool {
	return &GetCurrentTimeTool{timezone: timezone}
}

func (t *GetCurrentTimeTool) Name() string { return "get_current_time" }

func (t *GetCurrentTimeTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "get_current_time",
		Description: "Get the current date and time in the configured timezone.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"timezone": {
					"type": "string",
					"description": "Optional IANA timezone name (e.g. 'America/New_York'). Defaults to the configured timezone."
				}
			}
		}`),
	}
}

func (t *GetCurrentTimeTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Timezone string `json:"timezone"`
	}
	json.Unmarshal(input, &params)

	tzName := t.timezone
	if params.Timezone != "" {
		tzName = params.Timezone
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return Error("Invalid timezone: " + tzName)
	}

	now := time.Now().In(loc)
	return Success(fmt.Sprintf("%s (%s)\nDay of week: %s\nUnix: %d",
		now.Format("2006-01-02 15:04:05 MST"),
		tzName,
		now.Weekday().String(),
		now.Unix()))
}

// CompareTimeTool computes the duration between two timestamps.
type CompareTimeTool struct {
	timezone string
}

func NewCompareTimeTool(timezone string) *CompareTimeTool {
	return &CompareTimeTool{timezone: timezone}
}

func (t *CompareTimeTool) Name() string { return "compare_time" }

func (t *CompareTimeTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "compare_time",
		Description: "Compute the duration between two timestamps. Supports RFC3339, 'YYYY-MM-DD HH:MM:SS', and 'YYYY-MM-DD' formats.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"time_a": {
					"type": "string",
					"description": "First timestamp"
				},
				"time_b": {
					"type": "string",
					"description": "Second timestamp"
				},
				"timezone": {
					"type": "string",
					"description": "Timezone for parsing naive timestamps"
				}
			},
			"required": ["time_a", "time_b"]
		}`),
	}
}

func (t *CompareTimeTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		TimeA    string `json:"time_a"`
		TimeB    string `json:"time_b"`
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	tzName := t.timezone
	if params.Timezone != "" {
		tzName = params.Timezone
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return Error("Invalid timezone: " + tzName)
	}

	a, err := parseTimestamp(params.TimeA, loc)
	if err != nil {
		return Error("Invalid time_a: " + err.Error())
	}
	b, err := parseTimestamp(params.TimeB, loc)
	if err != nil {
		return Error("Invalid time_b: " + err.Error())
	}

	diff := b.Sub(a)
	absDiff := diff
	if absDiff < 0 {
		absDiff = -absDiff
	}

	hours := int(absDiff.Hours())
	days := hours / 24
	remainHours := hours % 24
	mins := int(absDiff.Minutes()) % 60

	direction := "later"
	if diff < 0 {
		direction = "earlier"
	}

	return Success(fmt.Sprintf(
		"A: %s\nB: %s\nDifference: %d days, %d hours, %d minutes (%s is %s)\nTotal seconds: %.0f",
		a.Format(time.RFC3339),
		b.Format(time.RFC3339),
		days, remainHours, mins,
		"B", direction,
		absDiff.Seconds()))
}

func parseTimestamp(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)

	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	// Try common naive formats.
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, value, loc); err == nil {
			return t, nil
		}
	}

	// Try date only.
	if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized format. Use RFC3339 or 'YYYY-MM-DD HH:MM[:SS]'")
}

// CalculateTool performs basic arithmetic.
type CalculateTool struct{}

func NewCalculateTool() *CalculateTool { return &CalculateTool{} }

func (t *CalculateTool) Name() string { return "calculate" }

func (t *CalculateTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "calculate",
		Description: "Evaluate a simple arithmetic expression. Supports +, -, *, /, parentheses, and basic math functions (sqrt, abs, round, ceil, floor).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "The arithmetic expression to evaluate (e.g. '(3 + 4) * 2')"
				}
			},
			"required": ["expression"]
		}`),
	}
}

func (t *CalculateTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	result, err := evalExpr(params.Expression)
	if err != nil {
		return Error("evaluation error: " + err.Error())
	}

	// Format nicely: if integer, no decimal; otherwise up to 10 sig digits.
	if result == math.Trunc(result) && !math.IsInf(result, 0) {
		return Success(fmt.Sprintf("%.0f", result))
	}
	return Success(strconv.FormatFloat(result, 'g', 10, 64))
}

// Simple recursive-descent expression evaluator.
type exprParser struct {
	input string
	pos   int
}

func evalExpr(expr string) (float64, error) {
	p := &exprParser{input: strings.TrimSpace(expr)}
	result := p.parseExpr()
	p.skipSpaces()
	if p.pos < len(p.input) {
		return 0, fmt.Errorf("unexpected character at position %d: '%c'", p.pos, p.input[p.pos])
	}
	return result, nil
}

func (p *exprParser) skipSpaces() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

func (p *exprParser) parseExpr() float64 {
	result := p.parseTerm()
	for {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right := p.parseTerm()
		if op == '+' {
			result += right
		} else {
			result -= right
		}
	}
	return result
}

func (p *exprParser) parseTerm() float64 {
	result := p.parseFactor()
	for {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '*' && op != '/' {
			break
		}
		p.pos++
		right := p.parseFactor()
		if op == '*' {
			result *= right
		} else {
			if right != 0 {
				result /= right
			} else {
				result = math.Inf(1)
			}
		}
	}
	return result
}

func (p *exprParser) parseFactor() float64 {
	p.skipSpaces()

	// Unary minus.
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
		return -p.parseFactor()
	}

	// Parentheses.
	if p.pos < len(p.input) && p.input[p.pos] == '(' {
		p.pos++
		result := p.parseExpr()
		p.skipSpaces()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
		}
		return result
	}

	// Functions.
	for _, fn := range []string{"sqrt", "abs", "round", "ceil", "floor"} {
		if p.pos+len(fn) <= len(p.input) && strings.ToLower(p.input[p.pos:p.pos+len(fn)]) == fn {
			p.pos += len(fn)
			p.skipSpaces()
			if p.pos < len(p.input) && p.input[p.pos] == '(' {
				p.pos++
				arg := p.parseExpr()
				p.skipSpaces()
				if p.pos < len(p.input) && p.input[p.pos] == ')' {
					p.pos++
				}
				switch fn {
				case "sqrt":
					return math.Sqrt(arg)
				case "abs":
					return math.Abs(arg)
				case "round":
					return math.Round(arg)
				case "ceil":
					return math.Ceil(arg)
				case "floor":
					return math.Floor(arg)
				}
			}
		}
	}

	// Number.
	start := p.pos
	for p.pos < len(p.input) && (p.input[p.pos] >= '0' && p.input[p.pos] <= '9' || p.input[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0
	}
	val, _ := strconv.ParseFloat(p.input[start:p.pos], 64)
	return val
}
