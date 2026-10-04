package history

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"mirr/internal/engine"
)

func pathFor(configFile string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(configFile)
	if err != nil {
		return "", err
	}
	id := sha256.Sum256([]byte(abs))
	return filepath.Join(base, "mirr", fmt.Sprintf("%x.json", id[:12])), nil
}

// Load returns the latest recorded result for each mirror. Missing history is empty.
func Load(configFile string) ([]engine.Result, error) {
	path, err := pathFor(configFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("history %s is invalid: expected a JSON array", path)
	}
	var results []engine.Result
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("history %s is invalid: %w", path, err)
	}
	return results, nil
}

// Save preserves results for mirrors outside the current sync selection.
func Save(configFile string, current []engine.Result) (retErr error) {
	path, err := pathFor(configFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close history lock: %w", err))
		}
	}()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("unlock history: %w", err))
		}
	}()

	previous, err := Load(configFile)
	if err != nil {
		return err
	}
	byMirror := make(map[string]engine.Result, len(previous)+len(current))
	key := func(result engine.Result) string { return result.Repository + "\x00" + result.Mirror }
	for _, result := range previous {
		byMirror[key(result)] = result
	}
	for _, result := range current {
		byMirror[key(result)] = result
	}
	keys := make([]string, 0, len(byMirror))
	for key := range byMirror {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	merged := make([]engine.Result, 0, len(keys))
	for _, key := range keys {
		merged = append(merged, byMirror[key])
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".history-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
