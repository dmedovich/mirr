package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

func normalizeEndpoint(ep *Endpoint, base string) (string, error) {
	value := ep.URL
	if value == "" || strings.HasPrefix(value, "-") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("invalid repository URL")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", fmt.Errorf("invalid repository URL")
		}
		switch u.Scheme {
		case "https":
			if !validRemoteHost(u.Hostname()) || u.Path == "" || u.Path == "/" || u.User != nil {
				return "", fmt.Errorf("HTTPS URL needs a host and path and must not contain credentials")
			}
		case "ssh":
			if !validRemoteHost(u.Hostname()) || u.Path == "" || u.Path == "/" {
				return "", fmt.Errorf("SSH URL needs a host and path")
			}
			if u.User != nil {
				if _, hasPassword := u.User.Password(); hasPassword {
					return "", fmt.Errorf("passwords in URLs are forbidden")
				}
				if !validRemoteUser(u.User.Username()) {
					return "", fmt.Errorf("invalid SSH username")
				}
			}
		case "file":
			if u.User != nil || (u.Host != "" && u.Host != "localhost") || !filepath.IsAbs(u.Path) {
				return "", fmt.Errorf("file URL requires an absolute local path")
			}
			return localIdentity(u.Path), nil
		default:
			return "", fmt.Errorf("use HTTPS, SSH or a local path")
		}
		return value, nil
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || !strings.Contains(value, ":") || strings.Contains(strings.SplitN(value, ":", 2)[0], "/") {
		if !filepath.IsAbs(value) {
			value = filepath.Join(base, value)
		}
		ep.URL = filepath.Clean(value)
		return localIdentity(ep.URL), nil
	}
	host, repo, _ := strings.Cut(value, ":")
	if host == "http" || host == "https" || host == "ssh" || host == "file" || host == "ext" {
		return "", fmt.Errorf("invalid repository URL")
	}
	if host == "" || repo == "" || strings.HasPrefix(repo, ":") || strings.Count(host, "@") > 1 ||
		strings.IndexFunc(value, unicode.IsSpace) >= 0 || strings.ContainsAny(value, "'\"`$;|&<>\\") || strings.Contains(host, "/") {
		return "", fmt.Errorf("invalid SSH repository address")
	}
	if strings.Contains(host, "@") {
		user, server, _ := strings.Cut(host, "@")
		if !validRemoteUser(user) || !validRemoteHost(server) {
			return "", fmt.Errorf("invalid SSH repository address")
		}
	} else if !validRemoteHost(host) {
		return "", fmt.Errorf("invalid SSH repository address")
	}
	return value, nil
}

func validRemoteHost(host string) bool {
	return host != "" && !strings.HasPrefix(host, "-") && strings.IndexFunc(host, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) < 0 && !strings.ContainsAny(host, "'\"`$;|&<>\\")
}

func validRemoteUser(user string) bool {
	return user != "" && !strings.HasPrefix(user, "-") && strings.IndexFunc(user, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) < 0 && !strings.ContainsAny(user, "'\"`$;|&<>\\")
}

func localIdentity(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return "local:" + path
}
