package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mirr/internal/config"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}

func fixture(t *testing.T) (*Engine, string, string) {
	t.Helper()
	d := t.TempDir()
	src := filepath.Join(d, "source")
	dst := filepath.Join(d, "mirror.git")
	git(t, d, "init", "-b", "main", src)
	git(t, d, "init", "--bare", dst)
	git(t, src, "commit", "--allow-empty", "-m", "first")
	git(t, src, "tag", "-a", "v1.0.0", "-m", "first release")
	git(t, src, "tag", "v2.0.0-rc1")
	git(t, src, "tag", "internal")
	cfg := &config.Config{Version: 1, Repositories: map[string]config.Repository{"project": {Source: config.Endpoint{URL: src}, Mirrors: map[string]config.Mirror{"mirror": {Endpoint: config.Endpoint{URL: dst}, Settings: config.Settings{Mode: "release"}}}}}}
	return &Engine{Config: cfg}, src, dst
}

func setMode(e *Engine, mode string, policy *config.Policy) {
	r := e.Config.Repositories["project"]
	m := r.Mirrors["mirror"]
	m.Mode = mode
	m.Policy = policy
	r.Mirrors["mirror"] = m
	e.Config.Repositories["project"] = r
}

func run(t *testing.T, e *Engine, apply bool) Result {
	t.Helper()
	rs := e.Run(context.Background(), "project", "mirror", apply)
	if len(rs) != 1 {
		t.Fatalf("results: %+v", rs)
	}
	if Failed(rs) {
		t.Fatalf("failed: %+v", rs)
	}
	return rs[0]
}

func TestReleaseAndModeSwitch(t *testing.T) {
	e, _, dst := fixture(t)
	p := run(t, e, false)
	if len(p.Changes) != 1 || p.Changes[0].Ref != "refs/tags/v1.0.0" {
		t.Fatalf("plan %+v", p)
	}
	if len(p.Branches) != 1 || p.Branches[0].Name != "main" || p.Branches[0].State != "source_only" {
		t.Fatalf("release mode should still show source branches: %+v", p.Branches)
	}
	if len(p.Tags) != 3 {
		t.Fatalf("release mode should show all tags, including filtered tags: %+v", p.Tags)
	}
	if got := git(t, dst, "for-each-ref", "--format=%(refname)"); got != "" {
		t.Fatal(got)
	}
}

func TestBranchOverviewShowsMatchingAndExtraBranches(t *testing.T) {
	e, src, dst := fixture(t)
	setMode(e, "continuous", nil)
	run(t, e, true)
	git(t, src, "branch", "source-only")
	git(t, dst, "branch", "mirror-only", "main")
	git(t, src, "commit", "--allow-empty", "-m", "next")
	result := run(t, e, false)
	states := map[string]string{}
	for _, branch := range result.Branches {
		states[branch.Name] = branch.State
	}
	for name, want := range map[string]string{"main": "different", "source-only": "source_only", "mirror-only": "mirror_only"} {
		if states[name] != want {
			t.Fatalf("%s state = %q, want %q: %+v", name, states[name], want, result.Branches)
		}
	}
	result = run(t, e, true)
	for _, branch := range result.Branches {
		want := "same"
		if branch.Name == "mirror-only" {
			want = "mirror_only" // delete is disabled by default
		}
		if branch.State != want {
			t.Fatalf("sync result should show branch state after push: %+v", result.Branches)
		}
	}
}

func TestSynchronizationModes(t *testing.T) {
	e, src, dst := fixture(t)
	_ = src
	r := run(t, e, true)
	if r.Status != "synced" {
		t.Fatal(r)
	}
	for _, tag := range r.Tags {
		if tag.Name == "v1.0.0" && tag.State != "same" {
			t.Fatalf("sync result should show updated tag inventory: %+v", r.Tags)
		}
	}
	out := git(t, dst, "for-each-ref", "--format=%(refname)")
	if out != "refs/tags/v1.0.0" {
		t.Fatal(out)
	}
	if run(t, e, true).Status != "current" {
		t.Fatal("not idempotent")
	}
	setMode(e, "continuous", nil)
	run(t, e, true)
	before := git(t, dst, "rev-parse", "refs/heads/main")
	git(t, src, "commit", "--allow-empty", "-m", "second")
	git(t, src, "tag", "v1.1.0")
	setMode(e, "release", &config.Policy{Delete: true})
	run(t, e, true)
	if git(t, dst, "rev-parse", "refs/heads/main") != before {
		t.Fatal("release updated branch")
	}
	out = git(t, dst, "for-each-ref", "--format=%(refname)")
	if !strings.Contains(out, "refs/tags/internal") || !strings.Contains(out, "refs/tags/v1.1.0") {
		t.Fatal(out)
	}
	setMode(e, "continuous", nil)
	run(t, e, true)
	if git(t, dst, "rev-parse", "refs/heads/main") != git(t, src, "rev-parse", "HEAD") {
		t.Fatal("continuous did not catch up")
	}
}

func TestMovedTagConflictsAndAtomicity(t *testing.T) {
	e, src, dst := fixture(t)
	run(t, e, true)
	before := git(t, dst, "rev-parse", "refs/tags/v1.0.0")
	git(t, src, "commit", "--allow-empty", "-m", "second")
	git(t, src, "tag", "-f", "v1.0.0")
	git(t, src, "tag", "v1.1.0")
	rs := e.Run(context.Background(), "project", "mirror", true)
	if rs[0].Status != "conflict" {
		t.Fatalf("%+v", rs)
	}
	if git(t, dst, "rev-parse", "refs/tags/v1.0.0") != before {
		t.Fatal("rewrote tag")
	}
	if strings.Contains(git(t, dst, "for-each-ref", "--format=%(refname)"), "v1.1.0") {
		t.Fatal("partial push on conflict")
	}
	setMode(e, "release", &config.Policy{AllowForce: true})
	run(t, e, true)
	if git(t, dst, "rev-parse", "refs/tags/v1.0.0") != git(t, src, "rev-parse", "HEAD") {
		t.Fatal("force failed")
	}
}

func TestDeletePolicyAndMirrorFailureIsolation(t *testing.T) {
	e, src, dst := fixture(t)
	run(t, e, true)
	git(t, src, "tag", "-d", "v1.0.0")
	run(t, e, true)
	if git(t, dst, "for-each-ref", "--format=%(refname)") == "" {
		t.Fatal("unexpected delete")
	}
	setMode(e, "release", &config.Policy{Delete: true})
	run(t, e, true)
	if git(t, dst, "for-each-ref", "--format=%(refname)") != "" {
		t.Fatal("delete failed")
	}
	git(t, src, "tag", "v3.0.0")
	r := e.Config.Repositories["project"]
	r.Mirrors["broken"] = config.Mirror{Endpoint: config.Endpoint{URL: filepath.Join(t.TempDir(), "missing")}}
	e.Config.Repositories["project"] = r
	rs := e.Run(context.Background(), "project", "", true)
	if len(rs) != 2 || rs[0].Status != "error" || rs[1].Status != "synced" {
		t.Fatalf("%+v", rs)
	}
	if len(rs[0].Branches) != 1 || rs[0].Branches[0].Name != "main" || rs[0].Branches[0].State != "unavailable" {
		t.Fatalf("source branches disappeared when mirror failed: %+v", rs[0].Branches)
	}
	if len(rs[0].Tags) == 0 || rs[0].Tags[0].State != "unavailable" {
		t.Fatalf("source tags disappeared when mirror failed: %+v", rs[0].Tags)
	}
}

func TestDivergedBranch(t *testing.T) {
	e, src, dst := fixture(t)
	setMode(e, "continuous", nil)
	run(t, e, true)
	git(t, src, "commit", "--allow-empty", "-m", "source second")
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-b", "main", dst, other)
	git(t, other, "commit", "--allow-empty", "-m", "destination second")
	git(t, other, "push", "origin", "main")
	rs := e.Run(context.Background(), "project", "mirror", true)
	if rs[0].Status != "conflict" {
		t.Fatalf("%+v", rs)
	}
}

func TestEmptySourceCannotDeleteMirror(t *testing.T) {
	e, _, dst := fixture(t)
	run(t, e, true)
	before := git(t, dst, "rev-parse", "refs/tags/v1.0.0")
	empty := filepath.Join(t.TempDir(), "empty.git")
	git(t, filepath.Dir(empty), "init", "--bare", empty)
	r := e.Config.Repositories["project"]
	r.Source.URL = empty
	e.Config.Repositories["project"] = r
	setMode(e, "release", &config.Policy{Delete: true})
	result := e.Run(context.Background(), "project", "mirror", true)[0]
	if result.Status != "error" || !strings.Contains(result.Error, "source has no branches or tags") {
		t.Fatalf("expected empty source error, got %+v", result)
	}
	if got := git(t, dst, "rev-parse", "refs/tags/v1.0.0"); got != before {
		t.Fatal("empty source changed mirror")
	}
}

func TestReviewedSyncRejectsChangedPlan(t *testing.T) {
	e, src, dst := fixture(t)
	setMode(e, "continuous", nil)
	plan := run(t, e, false)
	git(t, src, "commit", "--allow-empty", "-m", "new source commit")
	result := e.RunReviewed(context.Background(), "project", "mirror", plan.ReviewID)[0]
	if result.Status != "stale" || !Failed([]Result{result}) {
		t.Fatalf("expected stale plan: %+v", result)
	}
	if refs := git(t, dst, "for-each-ref", "--format=%(refname)"); refs != "" {
		t.Fatalf("stale plan changed mirror: %s", refs)
	}
	result = e.RunReviewed(context.Background(), "project", "mirror", result.ReviewID)[0]
	if result.Status != "synced" {
		t.Fatalf("reviewed plan did not sync: %+v", result)
	}
}

func TestReviewedSyncRejectsChangedDestination(t *testing.T) {
	e, _, _ := fixture(t)
	setMode(e, "continuous", nil)
	plan := run(t, e, false)
	other := filepath.Join(t.TempDir(), "other.git")
	git(t, filepath.Dir(other), "init", "--bare", other)
	r := e.Config.Repositories["project"]
	m := r.Mirrors["mirror"]
	m.URL = other
	r.Mirrors["mirror"] = m
	e.Config.Repositories["project"] = r
	result := e.RunReviewed(context.Background(), "project", "mirror", plan.ReviewID)[0]
	if result.Status != "stale" {
		t.Fatalf("changed destination was accepted: %+v", result)
	}
	if refs := git(t, other, "for-each-ref", "--format=%(refname)"); refs != "" {
		t.Fatalf("changed destination received refs: %s", refs)
	}
}

func TestCredentialErrorsAreRedacted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIRR_TEST_TOKEN", "test-secret-value")
	if e := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nprintf '%s' \"$MIRR_AUTH_SECRET\"\nexit 1\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := &session{dir: dir, cfg: &config.Config{Credentials: map[string]config.Credential{"test": {Provider: "env", Variable: "MIRR_TEST_TOKEN"}}}}
	_, e := s.git(context.Background(), config.Endpoint{URL: "https://example.com/repo", Auth: "test"}, "ls-remote")
	if e == nil || strings.Contains(e.Error(), "test-secret-value") || !strings.Contains(e.Error(), "[redacted]") {
		t.Fatalf("unsafe error: %v", e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "askpass"))
	if e != nil || strings.Contains(string(b), "test-secret-value") {
		t.Fatal("secret leaked to helper file")
	}
}
