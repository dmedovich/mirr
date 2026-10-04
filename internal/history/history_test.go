package history

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mirr/internal/engine"
)

func TestSavePreservesOtherMirrors(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configFile := t.TempDir() + "/.mirr.yaml"
	first := engine.Result{Repository: "project", Mirror: "one", Status: "synced"}
	second := engine.Result{Repository: "project", Mirror: "two", Status: "synced"}
	if err := Save(configFile, []engine.Result{first, second}); err != nil {
		t.Fatal(err)
	}
	first.Status = "current"
	if err := Save(configFile, []engine.Result{first}); err != nil {
		t.Fatal(err)
	}
	results, err := Load(configFile)
	if err != nil || len(results) != 2 || results[0].Status != "current" || results[1].Status != "synced" {
		t.Fatalf("results=%+v, error=%v", results, err)
	}
	path, err := pathFor(configFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history permissions: %v, %v", info, err)
	}
}

func TestCorruptHistoryIsNotOverwritten(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configFile := t.TempDir() + "/.mirr.yaml"
	path, err := pathFor(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("invalid JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = Save(configFile, []engine.Result{{Repository: "project", Mirror: "one"}}); err == nil {
		t.Fatal("expected invalid history error")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "invalid JSON" {
		t.Fatalf("history changed: %q, %v", data, err)
	}
}

func TestConcurrentSavesPreserveBothMirrors(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configFile := filepath.Join(t.TempDir(), ".mirr.yaml")
	var group sync.WaitGroup
	group.Add(2)
	errors := make(chan error, 2)
	for _, mirror := range []string{"one", "two"} {
		go func(mirror string) {
			defer group.Done()
			errors <- Save(configFile, []engine.Result{{Repository: "project", Mirror: mirror}})
		}(mirror)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	results, err := Load(configFile)
	if err != nil || len(results) != 2 {
		t.Fatalf("results=%+v, error=%v", results, err)
	}
}
