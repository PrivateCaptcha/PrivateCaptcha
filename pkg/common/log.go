package common

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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

		if svc, ok := ctx.Value(ServiceContextKey).(string); ok && (len(svc) > 0) {
			r.AddAttrs(ServiceAttr(svc))
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

func TraceContextFunc(ctx context.Context, traceID func() string) (context.Context, string) {
	tid, ok := ctx.Value(TraceIDContextKey).(string)
	if !ok || (len(tid) == 0) {
		tid = traceID()
	}

	return context.WithValue(ctx, TraceIDContextKey, tid), tid
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

func SetLogLevel(levelVar *slog.LevelVar, verbose bool) {
	level := slog.LevelDebug
	if verbose {
		level = LevelTrace
	}

	levelVar.Set(level)
}

func SetupLogs(stage string, verbose bool) *slog.LevelVar {
	levelVar := &slog.LevelVar{}
	SetLogLevel(levelVar, verbose)

	opts := &slog.HandlerOptions{
		Level: levelVar,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	ctxHandler := &contextHandler{handler}
	logger := slog.New(ctxHandler)
	slog.SetDefault(logger)
	return levelVar
}

func SetupTraceLogs() {
	opts := &slog.HandlerOptions{
		Level: LevelTrace,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)
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

func ServiceAttr(svc string) slog.Attr {
	return slog.Attr{
		Key:   "service",
		Value: slog.StringValue(svc),
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
