package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"mirr/internal/config"
	"os/exec"
	"sort"
	"strings"
)

func (s *session) planChanges(ctx context.Context, mirror config.Mirror, source, destination map[string]string) ([]Change, error) {
	all := make(map[string]struct{}, len(source)+len(destination))
	for ref := range source {
		all[ref] = struct{}{}
	}
	for ref := range destination {
		all[ref] = struct{}{}
	}
	ordered := make([]string, 0, len(all))
	for ref := range all {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)

	changes := make([]Change, 0)
	for _, ref := range ordered {
		if !mirror.Includes(ref) {
			continue
		}
		before, after := destination[ref], source[ref]
		if before == after {
			continue
		}
		change := Change{Ref: ref, Before: before, After: after}
		switch {
		case after == "":
			if !mirror.Policy.Delete {
				continue
			}
			change.Action = "delete"
		case before == "":
			change.Action = "create"
		default:
			change.Action = "update"
			if strings.HasPrefix(ref, "refs/tags/") {
				change.Action = "force"
			} else {
				_, err := s.git(ctx, config.Endpoint{}, "merge-base", "--is-ancestor", before, after)
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 {
						return nil, err
					}
					change.Action = "force"
				}
			}
			if change.Action == "force" && !mirror.Policy.AllowForce {
				change.Action = "conflict"
			}
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func hasConflict(changes []Change) bool {
	for _, change := range changes {
		if change.Action == "conflict" {
			return true
		}
	}
	return false
}

func refOverview(source, destination map[string]string, prefix string) []RefSnapshot {
	names := make(map[string]struct{})
	for ref := range source {
		if strings.HasPrefix(ref, prefix) {
			names[strings.TrimPrefix(ref, prefix)] = struct{}{}
		}
	}
	for ref := range destination {
		if strings.HasPrefix(ref, prefix) {
			names[strings.TrimPrefix(ref, prefix)] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	refs := make([]RefSnapshot, 0, len(ordered))
	for _, name := range ordered {
		ref := prefix + name
		item := RefSnapshot{Name: name, Source: source[ref], Mirror: destination[ref]}
		switch {
		case item.Source == "":
			item.State = "mirror_only"
		case item.Mirror == "":
			item.State = "source_only"
		case item.Source == item.Mirror:
			item.State = "same"
		default:
			item.State = "different"
		}
		refs = append(refs, item)
	}
	return refs
}

func unavailableRefs(source map[string]string, prefix string) []RefSnapshot {
	refs := refOverview(source, nil, prefix)
	for i := range refs {
		refs[i].State = "unavailable"
	}
	return refs
}

func reviewID(source config.Endpoint, mirror config.Mirror, changes []Change) (string, error) {
	data, err := json.Marshal(struct {
		Source  config.Endpoint
		Mirror  config.Mirror
		Changes []Change
	}{source, mirror, changes})
	if err != nil {
		return "", err
	}
	id := sha256.Sum256(data)
	return fmt.Sprintf("%x", id), nil
}
