package engine

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"mirr/internal/config"
)

type Change struct {
	Ref    string `json:"ref"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Action string `json:"action"`
}
type RefSnapshot struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Mirror string `json:"mirror,omitempty"`
	State  string `json:"state"`
}
type Result struct {
	Repository string        `json:"repository"`
	Mirror     string        `json:"mirror"`
	Mode       string        `json:"mode"`
	Changes    []Change      `json:"changes"`
	Branches   []RefSnapshot `json:"branches,omitempty"`
	Tags       []RefSnapshot `json:"tags,omitempty"`
	Status     string        `json:"status"`
	Error      string        `json:"error,omitempty"`
	CheckedAt  time.Time     `json:"checked_at"`
	ReviewID   string        `json:"-"`
}
type Engine struct{ Config *config.Config }

func (e *Engine) Run(ctx context.Context, project, target string, apply bool) []Result {
	return e.run(ctx, project, target, apply, nil)
}

// RunReviewed applies only the exact plan the caller already showed to the user.
// A changed endpoint, configuration or ref produces a fresh plan with status stale.
func (e *Engine) RunReviewed(ctx context.Context, project, target, reviewID string) []Result {
	return e.run(ctx, project, target, true, &reviewID)
}

func (e *Engine) run(ctx context.Context, project, target string, apply bool, expected *string) []Result {
	var results []Result
	names := []string{}
	for n := range e.Config.Repositories {
		if project == "" || n == project {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		r := e.Config.Repositories[n]
		ms := []string{}
		for mn := range r.Mirrors {
			if target == "" || target == mn {
				ms = append(ms, mn)
			}
		}
		sort.Strings(ms)
		for _, mn := range ms {
			m := r.Effective(r.Mirrors[mn])
			res := Result{Repository: n, Mirror: mn, Mode: m.Mode, Changes: []Change{}, Status: "current"}
			err := e.runMirror(ctx, r.Source, m, apply, expected, &res)
			res.CheckedAt = time.Now().UTC()
			if err != nil {
				res.Status = "error"
				res.Error = err.Error()
			}
			results = append(results, res)
		}
	}
	return results
}

func (e *Engine) runMirror(ctx context.Context, source config.Endpoint, m config.Mirror, apply bool, expected *string, res *Result) error {
	dir, err := os.MkdirTemp("", "mirr-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	s := &session{dir: dir, cfg: e.Config}
	if _, err = s.git(ctx, config.Endpoint{}, "init", "--bare", "."); err != nil {
		return err
	}
	src, err := s.fetchRefs(ctx, source, "source")
	if err != nil {
		return err
	}
	// A broken or empty source must never make delete: true clear a mirror.
	if len(src) == 0 {
		return fmt.Errorf("source has no branches or tags; refusing to synchronize")
	}
	dst, err := s.fetchRefs(ctx, m.Endpoint, "destination")
	if err != nil {
		res.Branches = unavailableRefs(src, "refs/heads/")
		res.Tags = unavailableRefs(src, "refs/tags/")
		return err
	}
	res.Branches = refOverview(src, dst, "refs/heads/")
	res.Tags = refOverview(src, dst, "refs/tags/")
	res.Changes, err = s.planChanges(ctx, m, src, dst)
	if err != nil {
		return err
	}
	res.ReviewID, err = reviewID(source, m, res.Changes)
	if err != nil {
		return err
	}
	if expected != nil && (*expected == "" || *expected != res.ReviewID) {
		res.Status = "stale"
		res.Error = "plan changed since confirmation; review these changes and confirm again"
		return nil
	}
	if hasConflict(res.Changes) {
		res.Status = "conflict"
		return nil
	}
	if len(res.Changes) == 0 {
		return nil
	}
	res.Status = "pending"
	if !apply {
		return nil
	}
	if err = s.pushChanges(ctx, m.Endpoint, res.Changes); err != nil {
		return err
	}
	for _, change := range res.Changes {
		if change.After == "" {
			delete(dst, change.Ref)
		} else {
			dst[change.Ref] = change.After
		}
	}
	res.Branches = refOverview(src, dst, "refs/heads/")
	res.Tags = refOverview(src, dst, "refs/tags/")
	res.Status = "synced"
	return nil
}

func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == "error" || r.Status == "conflict" || r.Status == "stale" {
			return true
		}
	}
	return false
}
