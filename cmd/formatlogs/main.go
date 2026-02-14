package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// HTMLTemplate is the constant defining the page structure
const HTMLTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Error Log Viewer</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #f4f4f9; margin: 0; }
        h1 { color: #333; margin: 0 0 12px 0; }

        .page-header {
            position: sticky;
            top: 0;
            z-index: 3;
            background: #f4f4f9;
            padding: 20px 20px 12px 20px;
            border-bottom: 1px solid #ccc;
        }

        table { width: 100%; border-collapse: collapse; background: white; box-shadow: 0 1px 3px rgba(0,0,0,0.1); border-radius: 8px; overflow: hidden; }
        thead th { position: sticky; top: 110px; z-index: 2; }
        th, td { padding: 12px 15px; text-align: left; border-bottom: 1px solid #ddd; vertical-align: middle; }
        td.details { max-width: 400px; }
        thead { background-color: #f8f9fa; }
        th { background-color: #f8f9fa; font-weight: 600; color: #555; }
        tr:hover { background-color: #f1f1f1; }

        /* Highlighting Errors */
        .error-row { box-shadow: inset 5px 0 0 #dc3545; border-left: none; }
        .warn-row  { box-shadow: inset 5px 0 0 #ffc107; border-left: none; }

        /* Badges */
        .badge { padding: 4px 8px; border-radius: 4px; font-size: 0.85em; font-weight: bold; color: white; background: #6c757d; }
        .badge.error, .badge.critical, .badge.fatal { background: #dc3545; }
        .badge.warning, .badge.warn { background: #ffc107; color: #333; }
        .badge.info { background: #17a2b8; }

        pre { background: #2d2d2d; color: #f8f8f2; padding: 10px; border-radius: 4px; overflow-x: auto; font-size: 0.75rem; margin-top: 5px; }
        .message { font-weight: 500; font-size: 1rem; max-width: 600px; }
        .error-detail { border: 2px solid #dc3545; font-size: 1rem; display: inline-block; padding: 6px 10px; border-radius: 0.5rem; }

        .filter-row { display: flex; align-items: center; justify-content: flex-start; gap: 3rem; flex-wrap: wrap; }
        .filter { display: flex; align-items: center; gap: 8px; }
        .filter label { font-weight: 600; margin-right: 8px; }
        .total { font-weight: 600; color: #333; }
        .page-body { padding: 20px; }

        .trace-cell {
            border-radius: 0.375rem;
            padding: 0.25rem 0.5rem;
        }
    </style>
</head>
<body>
    <div class="page-header">
        <h1>Log Analysis Report</h1>
        <div class="filter-row">
            <div class="filter">
                <label for="levelFilter">Minimum Level:</label>
                <select id="levelFilter">
                    <option value="ERROR">ERROR</option>
                    <option value="WARN">WARN</option>
                    <option value="INFO">INFO</option>
                    <option value="DEBUG">DEBUG</option>
                    <option value="DEBUG-4">DEBUG-4 (Trace)</option>
                </select>
            </div>
            <div class="filter">
                <label for="traceFilter">Trace ID:</label>
                <select id="traceFilter">
                    <option value="">All</option>
                </select>
            </div>
            <div class="total">Total Entries: {{len .}}</div>
        </div>
    </div>

    <div class="page-body">
        <table>
            <thead>
                <tr>
                    <th style="width: 150px;">Timestamp</th>
                    <th style="width: 80px;">Level</th>
                    <th style="width: 100px;">Trace ID</th>
                    <th style="width: 100px;">Session ID</th>
                    <th style="width: 80px;">Service</th>
                    <th>Message</th>
                    <th>Error</th>
                    <th style="width: 120px;">Details</th>
                </tr>
            </thead>
            <tbody>
                {{range .}}
                <tr class="{{if .IsError}}error-row{{else if .IsWarn}}warn-row{{end}}" data-level="{{.Level}}" data-trace-id="{{.TraceID}}">
                    <td>{{.Timestamp}}</td>
                    <td><span class="badge {{.LevelClass}}">{{.Level}}</span></td>
                    <td>
                        {{if .TraceID}}
                        <span class="trace-cell" data-trace-color="{{.TraceColor}}" style="background-color: {{.TraceBackground}};">
                            {{.TraceID}}
                        </span>
                        {{else}}
                            <span class="trace-cell" style="opacity: 0.3">(empty)</span>
                        {{end}}
                    </td>
                    <td>{{.SessID}}</td>
                    <td>{{.Service}}</td>
                    <td>
                        <div class="message">{{.Message}}</div>
                    </td>
                    <td>
                        {{if .ErrorDetails}}
                        <p class="error-detail">{{.ErrorDetails}}</p>
                        {{end}}
                    </td>
                    <td class="details">
                        {{if .Extras}}
                        <pre>{{.Extras}}</pre>
                        {{end}}
                        {{if .Details}}
                        <pre>{{.Details}}</pre>
                        {{end}}
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>

    <script>
        const levelOrder = {
            "DEBUG-4": 1,
            "TRACE": 1,
            "DEBUG": 2,
            "INFO": 3,
            "WARN": 4,
            "WARNING": 4,
            "ERROR": 5,
            "CRITICAL": 6,
            "FATAL": 7
        };

        const filterSelect = document.getElementById("levelFilter");
        const traceFilter = document.getElementById("traceFilter");
        filterSelect.value = "ERROR";

        function applyFilter() {
            const minLevel = filterSelect.value;
            const minRank = levelOrder[minLevel] || 0;
            const selectedTrace = traceFilter.value;
            const emptyToken = "__EMPTY__";

            document.querySelectorAll("tbody tr").forEach(row => {
                const rowLevel = (row.dataset.level || "").toUpperCase();
                const rowRank = levelOrder[rowLevel] || 0;
                const rowTrace = row.dataset.traceId || "";
                const levelMatch = rowRank >= minRank;
                const traceMatch =
                    selectedTrace === "" ||
                    (selectedTrace === emptyToken && rowTrace === "") ||
                    rowTrace === selectedTrace;
                row.style.display = levelMatch && traceMatch ? "" : "none";
            });
        }

        function buildTraceFilter() {
            const traces = new Set();
            document.querySelectorAll("tbody tr").forEach(row => {
                const trace = row.dataset.traceId || "";
                traces.add(trace);
            });

            const ordered = Array.from(traces).sort((a, b) => {
                if (a === "") return -1;
                if (b === "") return 1;
                return a.localeCompare(b);
            });

            const emptyToken = "__EMPTY__";
            ordered.forEach(trace => {
                const option = document.createElement("option");
                option.value = trace === "" ? emptyToken : trace;
                option.textContent = trace === "" ? "(empty)" : trace;
                traceFilter.appendChild(option);
            });
        }

        filterSelect.addEventListener("change", applyFilter);
        traceFilter.addEventListener("change", applyFilter);
        buildTraceFilter();
        applyFilter();
    </script>
</body>
</html>
`

// LogEntry represents the normalized data structure for the template
type LogEntry struct {
	Timestamp       string
	Level           string
	LevelClass      string
	Message         string
	Details         string
	ErrorDetails    string
	Extras          string
	TraceID         string
	SessID          string
	Service         string
	IsError         bool
	IsWarn          bool
	TraceColor      string
	TraceBackground template.CSS
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
	t := template.Must(template.New("report").Parse(HTMLTemplate))
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
