package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mirr/internal/auth"
	"mirr/internal/config"
)

type session struct {
	dir     string
	cfg     *config.Config
	secrets []string
}

func (s *session) git(ctx context.Context, ep config.Endpoint, args ...string) (string, error) {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	prefix := []string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.ext.allow=never"}
	if ep.Auth != "" {
		cr := s.cfg.Credentials[ep.Auth]
		secret, err := auth.Secret(ctx, cr)
		if err != nil {
			return "", err
		}
		s.secrets = append(s.secrets, secret)
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		helper := filepath.Join(s.dir, "askpass")
		if err = os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MIRR_EXEC\" __askpass \"$@\"\n"), 0o700); err != nil {
			return "", err
		}
		user := cr.Username
		if user == "" {
			user = "git"
		}
		env = append(env, "GIT_ASKPASS="+helper, "MIRR_EXEC="+exe, "MIRR_AUTH_SECRET="+secret, "MIRR_AUTH_USER="+user)
		prefix = append(prefix, "-c", "credential.helper=", "-c", "http.followRedirects=false")
	}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Dir = s.dir
	cmd.Env = env
	b, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(b))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		for _, secret := range s.secrets {
			out = strings.ReplaceAll(out, secret, "[redacted]")
		}
		return "", fmt.Errorf("git: %s: %w", out, err)
	}
	return out, nil
}

func (s *session) fetchRefs(ctx context.Context, ep config.Endpoint, namespace string) (map[string]string, error) {
	if _, err := s.git(ctx, ep, "fetch", "--no-tags", ep.URL,
		"+refs/heads/*:refs/"+namespace+"/heads/*",
		"+refs/tags/*:refs/"+namespace+"/tags/*"); err != nil {
		return nil, err
	}
	out, err := s.git(ctx, config.Endpoint{}, "for-each-ref", "--format=%(objectname) %(refname)", "refs/"+namespace+"/")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.HasSuffix(fields[1], "^{}") {
			continue
		}
		ref := strings.TrimPrefix(fields[1], "refs/"+namespace+"/")
		result["refs/"+ref] = fields[0]
	}
	return result, nil
}

func (s *session) pushChanges(ctx context.Context, ep config.Endpoint, changes []Change) error {
	args := []string{"push", "--atomic"}
	for _, change := range changes {
		args = append(args, "--force-with-lease="+change.Ref+":"+change.Before)
	}
	args = append(args, ep.URL)
	for _, change := range changes {
		args = append(args, change.After+":"+change.Ref)
	}
	_, err := s.git(ctx, ep, args...)
	return err
}
