package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mirr/internal/config"
	"mirr/internal/history"
)

func TestInitValidatesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mirr.yaml")
	command := root()
	command.SetArgs([]string{"-c", path, "init", "--source", "https://source.example/repo.git", "--mirror", "https://token@mirror.example/repo.git"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected invalid mirror URL")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid config was created: %v", err)
	}
	command = root()
	command.SetArgs([]string{"-c", path, "init", "--source", "https://source.example/repo.git", "--mirror", "git@mirror.example:team/repo.git"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("created config is invalid: %v", err)
	}
}

func TestCLILocalSyncAndHistoryPreflight(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	t.Setenv("XDG_CACHE_HOME", cache)
	source := filepath.Join(base, "source")
	mirror := filepath.Join(base, "mirror.git")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git(base, "init", "-b", "main", source)
	git(base, "init", "--bare", mirror)
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	configFile := filepath.Join(base, ".mirr.yaml")
	run := func(args ...string) error {
		t.Helper()
		cmd := root()
		cmd.SetArgs(append([]string{"-c", configFile}, args...))
		return cmd.Execute()
	}
	if err := run("init", "--source", source, "--mirror", mirror, "--name", "test", "--mode", "continuous"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"plan", "test"}, {"sync", "test"}, {"plan", "test"}} {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if got, want := git(mirror, "rev-parse", "refs/heads/main"), git(source, "rev-parse", "HEAD"); got != want {
		t.Fatalf("mirror head %s, source head %s", got, want)
	}
	results, err := history.Load(configFile)
	if err != nil || len(results) != 1 || results[0].Status != "synced" {
		t.Fatalf("history %+v, error %v", results, err)
	}
	files, err := filepath.Glob(filepath.Join(cache, "mirr", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("history files %v, error %v", files, err)
	}
	if err := os.WriteFile(files[0], []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "second")
	if err := run("sync", "test"); err == nil {
		t.Fatal("sync ignored corrupt history")
	}
	if got := git(mirror, "rev-parse", "refs/heads/main"); got == git(source, "rev-parse", "HEAD") {
		t.Fatal("sync pushed despite corrupt history")
	}
}
