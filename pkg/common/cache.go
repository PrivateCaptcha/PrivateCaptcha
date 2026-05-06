package common

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/maypok86/otter/v2"
)

func SaveCacheToFile[TKey comparable, TValue any](ctx context.Context, dir, filename string, maxItems int, cache *otter.Cache[TKey, TValue], filter func(TValue) bool) error {
	if len(dir) == 0 {
		slog.DebugContext(ctx, "Skipping saving cache without cache dir")
		return nil
	}

	if cache == nil {
		slog.WarnContext(ctx, "Cannot persist nil cache", "file", filename)
		return nil
	}

	if _, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			slog.ErrorContext(ctx, "Failed to stat cache directory", "dir", dir, ErrAttr(err))
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			slog.ErrorContext(ctx, "Failed to create cache directory", "dir", dir, ErrAttr(err))
			return err
		}
	}

	filePath := filepath.Join(dir, filename)
	tmp, err := os.CreateTemp(dir, filename+".*.tmp")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create cache tmp file", "dir", dir, ErrAttr(err))
		return err
	}
	tmpPath := tmp.Name()
	// best-effort cleanup if we bail out before rename
	defer os.Remove(tmpPath)

	count, err := SaveCacheToWriter(ctx, tmp, cache, maxItems, filter)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to save cache", "file", tmpPath, ErrAttr(err))
		_ = tmp.Close()
		return err
	}

	slog.DebugContext(ctx, "Persisted cached items", "count", count)

	if err := tmp.Sync(); err != nil {
		slog.ErrorContext(ctx, "Failed to fsync cache file", "file", tmpPath, ErrAttr(err))
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		slog.ErrorContext(ctx, "Failed to close cache tmp file", "file", tmpPath, ErrAttr(err))
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		slog.ErrorContext(ctx, "Failed to rename cache file", "file", filePath, ErrAttr(err))
		return err
	}

	return nil
}

func SaveCacheToWriter[TKey comparable, TValue any](ctx context.Context, w io.Writer, cache *otter.Cache[TKey, TValue], maxItems int, filter func(TValue) bool) (int, error) {
	timeEnc := gob.NewEncoder(w)
	if err := timeEnc.Encode(time.Now()); err != nil {
		return 0, err
	}

	enc := gob.NewEncoder(w)

	maximum := cache.GetMaximum()
	if err := enc.Encode(maximum); err != nil {
		return 0, err
	}

	size := uint64(0)
	count := 0
	for entry := range cache.Hottest() {
		if (size >= maximum) || (count >= maxItems) {
			break
		}

		if err := ctx.Err(); err != nil {
			slog.WarnContext(ctx, "Truncated cache due to context cancellation", ErrAttr(err))
			// it's not reported as error because we care only about best-effort here
			break
		}

		if filter != nil && !filter(entry.Value) {
			continue
		}

		if err := enc.Encode(entry); err != nil {
			return count, err
		}

		size += uint64(entry.Weight)
		count++
	}

	return count, nil
}

func LoadCacheFromFile[TKey comparable, TValue any](ctx context.Context, dir, filename string, ttl time.Duration, cache *otter.Cache[TKey, TValue]) error {
	if len(dir) == 0 {
		slog.DebugContext(ctx, "Skipping reading cache without cache dir")
		return nil
	}

	if cache == nil {
		slog.WarnContext(ctx, "Cannot load to nil cache", "file", filename)
		return nil
	}

	filePath := filepath.Join(dir, filename)
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "Cache file does not exist", "file", filePath)
			return nil
		}
		slog.ErrorContext(ctx, "Failed to open cache file", "file", filePath, ErrAttr(err))
		return err
	}
	defer file.Close()

	br := bufio.NewReader(file)
	if err := LoadCacheFromReader(ctx, br, cache, ttl); err != nil {
		slog.ErrorContext(ctx, "Failed to load cache from file", "file", filePath, ErrAttr(err))
		if rmErr := os.Remove(filePath); rmErr != nil {
			slog.ErrorContext(ctx, "Failed to remove corrupted cache file", "file", filePath, ErrAttr(rmErr))
		} else {
			slog.DebugContext(ctx, "Removed corrupted cache file", "file", filePath)
		}
		return err
	}

	slog.DebugContext(ctx, "Read cache from file", "file", filePath)

	return nil
}

func LoadCacheFromReader[TKey comparable, TValue any](ctx context.Context, r io.Reader, cache *otter.Cache[TKey, TValue], ttl time.Duration) error {
	dec := gob.NewDecoder(r)
	var saveTime time.Time
	if err := dec.Decode(&saveTime); err != nil {
		slog.ErrorContext(ctx, "Failed to read embedded time", ErrAttr(err))
		return err
	}

	if age := time.Since(saveTime); age > ttl {
		// skip loading
		slog.WarnContext(ctx, "Ignoring too old cache file (embedded time)", "age", age.String())
		return nil
	}

	return otter.LoadCacheFrom(cache, r)
}
