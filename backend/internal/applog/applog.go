package applog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

type Logger struct {
	*slog.Logger
	file *rotatingFile
}

func Open(logDir, level string, maxBytes int64, backups int) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	file, err := openRotatingFile(filepath.Join(logDir, "subadmin.log"), maxBytes, backups)
	if err != nil {
		return nil, err
	}
	handler := &requestIDHandler{next: slog.NewJSONHandler(io.MultiWriter(os.Stdout, file), &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				attr.Value = slog.StringValue(attr.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			return attr
		},
	})}
	logger := slog.New(handler).With("component", "subadmin")
	slog.SetDefault(logger)
	return &Logger{Logger: logger, file: file}, nil
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(RequestIDKey).(string)
	return value
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

type requestIDHandler struct {
	next slog.Handler
}

func (h *requestIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *requestIDHandler) Handle(ctx context.Context, record slog.Record) error {
	if requestID := RequestID(ctx); requestID != "" {
		record.AddAttrs(slog.String("request_id", requestID))
	}
	return h.next.Handle(ctx, record)
}

func (h *requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &requestIDHandler{next: h.next.WithAttrs(attrs)}
}

func (h *requestIDHandler) WithGroup(name string) slog.Handler {
	return &requestIDHandler{next: h.next.WithGroup(name)}
}

type rotatingFile struct {
	mu        sync.Mutex
	path      string
	maxBytes  int64
	backups   int
	file      *os.File
	sizeBytes int64
}

func openRotatingFile(path string, maxBytes int64, backups int) (*rotatingFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	return &rotatingFile{path: path, maxBytes: maxBytes, backups: backups, file: file, sizeBytes: size}, nil
}

func (w *rotatingFile) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxBytes > 0 && w.sizeBytes+int64(len(data)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.sizeBytes += int64(n)
	return n, err
}

func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *rotatingFile) rotateLocked() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}
	if w.backups > 0 {
		for index := w.backups - 1; index >= 1; index-- {
			oldPath := backupPath(w.path, index)
			newPath := backupPath(w.path, index+1)
			if _, err := os.Stat(oldPath); err == nil {
				_ = os.Rename(oldPath, newPath)
			}
		}
		if _, err := os.Stat(w.path); err == nil {
			_ = os.Rename(w.path, backupPath(w.path, 1))
		}
	} else {
		_ = os.Remove(w.path)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.sizeBytes = 0
	return nil
}

func backupPath(path string, index int) string {
	return path + "." + strconv.Itoa(index)
}

func parseLevel(level string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
