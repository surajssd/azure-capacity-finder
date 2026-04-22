package whouses

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

const cacheDir = "/tmp/azure-capacity-finder"

// loadOrFetch tries to load data from a cache file. If the file doesn't exist
// or refresh is true, it calls fetchFn and caches the result.
func loadOrFetch[T any](cacheFile string, refresh bool, fetchFn func() (T, error)) (T, error) {
	path := filepath.Join(cacheDir, cacheFile)

	if refresh {
		os.Remove(path)
	}

	// Try loading from cache.
	if data, err := os.ReadFile(path); err == nil {
		slog.Info("using cached data", "file", path)

		var result T
		if err := json.Unmarshal(data, &result); err == nil {
			return result, nil
		}

		slog.Warn("cache file corrupt, refetching", "file", path)
	}

	// Fetch from API.
	result, err := fetchFn()
	if err != nil {
		var zero T
		return zero, err
	}

	// Cache the result.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return result, nil // non-fatal
	}

	data, err := json.Marshal(result)
	if err != nil {
		return result, nil // non-fatal
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("failed to write cache", "file", path, "error", err)
	} else {
		slog.Info("cached data", "file", path)
	}

	return result, nil
}
