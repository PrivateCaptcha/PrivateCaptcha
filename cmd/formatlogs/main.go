package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

//go:embed report.html
var htmlTemplate string

// LogEntry represents the normalized data structure for the template
type LogEntry struct {
	Timestamp            string
	Level                string
	LevelClass           string
	Message              string
	Details              string
	ErrorDetails         string
	Extras               string
	TraceID              string
	SessID               string
	Service              string
	IsError              bool
	IsWarn               bool
	TraceColor           string
	TraceBackground      template.CSS
	TraceHoverBackground template.CSS
}

func main() {
	// 1. Parse Input Stream
	// We check if data is being piped to stdin
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Println("Usage: cat logs.json | go run main.go")
		fmt.Println("Waiting for input... (Ctrl+D to finish)")
	}

	var logs []LogEntry
	decoder := json.NewDecoder(os.Stdin)

	for {
		// Decode into a generic map to handle varying field names
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			// Skip malformed lines
			continue
		}

		// Normalize data
		entry := normalizeLog(raw)
		logs = append(logs, entry)
	}

	if len(logs) == 0 {
		fmt.Println("No valid JSON logs found.")
		os.Exit(1)
	}

	applyTraceColors(logs)

	// 2. Create Temp File
	tmpFile, err := os.CreateTemp("", "private_captcha_logs_*.html")
	if err != nil {
		panic(err)
	}
	defer tmpFile.Close()

	// 3. Render Template
	t := template.Must(template.New("report").Parse(htmlTemplate))
	err = t.Execute(tmpFile, logs)
	if err != nil {
		panic(err)
	}

	fmt.Print(tmpFile.Name())
}

func applyTraceColors(entries []LogEntry) {
	palette := []string{
		"#3b82f6", // blue
		"#22c55e", // green
		"#f97316", // orange
		"#a855f7", // purple
		"#14b8a6", // teal
		"#ef4444", // red
		"#eab308", // yellow
		"#0ea5e9", // sky
		"#ec4899", // pink
		"#8b5cf6", // violet
	}

	traceColor := map[string]string{}
	next := 0

	for i := range entries {
		traceID := entries[i].TraceID
		if traceID == "" {
			continue
		}
		color, ok := traceColor[traceID]
		if !ok {
			color = palette[next%len(palette)]
			traceColor[traceID] = color
			next++
		}
		entries[i].TraceColor = color
		entries[i].TraceBackground = withAlpha(color, 0.12)
		entries[i].TraceHoverBackground = withAlpha(color, 0.35)
	}
}

func withAlpha(hex string, alpha float64) template.CSS {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return ""
	}
	r := hex[0:2]
	g := hex[2:4]
	b := hex[4:6]
	return template.CSS(fmt.Sprintf("rgba(%d,%d,%d,%.2f)", mustHex(r), mustHex(g), mustHex(b), alpha))
}

func mustHex(s string) int {
	var v int
	fmt.Sscanf(s, "%02x", &v)
	return v
}

// normalizeLog converts raw JSON map to a structured LogEntry
func normalizeLog(raw map[string]interface{}) LogEntry {
	entry := LogEntry{
		Timestamp: formatTimestamp(raw),
		Level:     getString(raw, "level", "severity", "log_level"),
		Message:   getString(raw, "message", "msg", "text"),
		TraceID:   getString(raw, "traceID", "trace_id", "traceId"),
		SessID:    getString(raw, "sessID", "session_id", "sessionId"),
		Service:   getString(raw, "service", "svc"),
	}

	if entry.Timestamp == "" {
		entry.Timestamp = "N/A"
	}
	if entry.Level == "" {
		entry.Level = "UNKNOWN"
	}

	// Handle stack traces or error objects
	if errObj, ok := raw["error"]; ok {
		entry.ErrorDetails = fmt.Sprintf("%v", errObj)
	}
	if stack, ok := raw["stack_trace"]; ok {
		entry.Details = fmt.Sprintf("%v", stack)
	}

	// Determine styling logic
	lvl := strings.ToUpper(entry.Level)
	entry.LevelClass = strings.ToLower(entry.Level)

	if lvl == "ERROR" || lvl == "CRITICAL" || lvl == "FATAL" {
		entry.IsError = true
	}
	if lvl == "WARN" || lvl == "WARNING" {
		entry.IsWarn = true
	}

	// Collect extra fields not mapped to columns/message/details
	entry.Extras = formatExtras(raw)

	return entry
}

// formatTimestamp attempts to parse a time field and return time.StampMilli
func formatTimestamp(raw map[string]interface{}) string {
	candidates := []string{"timestamp", "time", "date"}
	for _, key := range candidates {
		if val, ok := raw[key]; ok {
			return formatTimeValue(val)
		}
	}
	return ""
}

func formatTimeValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		if t, ok := tryParseTime(v); ok {
			return t.Format(time.StampMilli)
		}
		return v
	case float64:
		// assume unix milliseconds
		t := time.Unix(0, int64(v)*int64(time.Millisecond))
		return t.Format(time.StampMilli)
	case int64:
		t := time.Unix(0, v*int64(time.Millisecond))
		return t.Format(time.StampMilli)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func tryParseTime(s string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.ANSIC,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.000",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// formatExtras renders all non-column fields as pretty JSON
func formatExtras(raw map[string]interface{}) string {
	known := map[string]bool{
		"timestamp":   true,
		"time":        true,
		"date":        true,
		"level":       true,
		"severity":    true,
		"log_level":   true,
		"message":     true,
		"msg":         true,
		"text":        true,
		"traceID":     true,
		"trace_id":    true,
		"traceId":     true,
		"sessID":      true,
		"session_id":  true,
		"sessionId":   true,
		"service":     true,
		"svc":         true,
		"stack_trace": true,
		"error":       true,
	}

	extras := map[string]interface{}{}
	for k, v := range raw {
		if !known[k] {
			extras[k] = v
		}
	}

	if len(extras) == 0 {
		return ""
	}

	// stable output
	keys := make([]string, 0, len(extras))
	for k := range extras {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := map[string]interface{}{}
	for _, k := range keys {
		ordered[k] = extras[k]
	}

	b, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// getString is a helper to find the first existing key from a list of candidates
func getString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := data[key]; ok {
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}
