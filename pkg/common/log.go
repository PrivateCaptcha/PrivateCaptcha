package common

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const (
	LevelTrace = slog.Level(-8)
)

type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if tid, ok := ctx.Value(TraceIDContextKey).(string); ok && (len(tid) > 0) {
			r.AddAttrs(TraceIDAttr(tid))
		}

		if sid, ok := ctx.Value(SessionIDContextKey).(string); ok && (len(sid) > 0) {
			r.AddAttrs(SessionIDAttr(sid))
		}
	}

	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{h.Handler.WithGroup(name)}
}

func TraceContextFunc(ctx context.Context, traceID func() string) context.Context {
	if tid, ok := ctx.Value(TraceIDContextKey).(string); !ok || (len(tid) == 0) {
		ctx = context.WithValue(ctx, TraceIDContextKey, traceID())
	}

	return ctx
}

func TraceContext(ctx context.Context, traceID string) context.Context {
	if tid, ok := ctx.Value(TraceIDContextKey).(string); !ok || (len(tid) == 0) {
		ctx = context.WithValue(ctx, TraceIDContextKey, traceID)
	}

	return ctx
}

func CopyTraceID(from context.Context, to context.Context) context.Context {
	if tid, ok := from.Value(TraceIDContextKey).(string); ok && (len(tid) > 0) {
		return context.WithValue(to, TraceIDContextKey, tid)
	}

	return to
}

func SetupLogs(stage string, verbose bool) {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	if verbose {
		opts.Level = LevelTrace
		// opts.AddSource = true
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	ctxHandler := &contextHandler{handler}
	logger := slog.New(ctxHandler)
	slog.SetDefault(logger)
}

func SetupTraceLogs() {
	opts := &slog.HandlerOptions{
		Level: LevelTrace,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)
}

func FindHeader(headers map[string]string, header string) (string, bool) {
	requestID, ok := headers[header]
	if ok {
		return requestID, ok
	}

	requestID, ok = headers[strings.ToLower(header)]
	if ok {
		return requestID, ok
	}

	return "", false
}

func ErrAttr(err error) slog.Attr {
	return slog.Any("error", err)
}

func TraceIDAttr(tid string) slog.Attr {
	return slog.Attr{
		Key:   "traceID",
		Value: slog.StringValue(tid),
	}
}

func SessionIDAttr(sid string) slog.Attr {
	return slog.Attr{
		Key:   "sessID",
		Value: slog.StringValue(sid),
	}
}

type FmtLogger struct {
	Ctx   context.Context
	Level slog.Level
}

func (l *FmtLogger) Printf(s string, args ...interface{}) {
	msg := fmt.Sprintf(s, args...)
	slog.Log(l.Ctx, l.Level, msg)
}
