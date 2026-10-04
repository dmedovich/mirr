package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	base := `version: 1
repositories:
  app:
    source:
      url: ./source
    mirrors:
      backup:
        url: ./backup
`
	for _, tt := range []struct {
		name, text string
		bad        bool
	}{{"valid", base, false}, {"unknown", base + "typo: yes\n", true}, {"multiple", base + "---\nversion: 1\n", true}, {"mode", strings.Replace(base, "url: ./backup", "url: ./backup\n        mode: wrong", 1), true}, {"same", strings.Replace(base, "./backup", "./source", 1), true}, {"password", strings.Replace(base, "./backup", "https://user:secret@example.com/repo", 1), true}} {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), ".mirr.yaml")
			if e := os.WriteFile(p, []byte(tt.text), 0o600); e != nil {
				t.Fatal(e)
			}
			c, e := Load(p)
			if (e != nil) != tt.bad {
				t.Fatalf("error: %v", e)
			}
			if !tt.bad && !filepath.IsAbs(c.Repositories["app"].Source.URL) {
				t.Fatal("relative path not resolved")
			}
		})
	}
}

func TestReleaseScope(t *testing.T) {
	r := Repository{}
	m := r.Effective(Mirror{})
	for ref, want := range map[string]bool{"refs/tags/v1.0": true, "refs/tags/v2-rc1": false, "refs/heads/main": false, "refs/tags/internal": false} {
		if m.Includes(ref) != want {
			t.Fatal(ref)
		}
	}
}

func TestEndpointValidation(t *testing.T) {
	base := t.TempDir()
	for _, tc := range []struct {
		name, source, mirror string
		valid                bool
	}{
		{"standard SSH", "https://example.com/team/source.git", "git@github.com:team/mirror.git", true},
		{"HTTPS userinfo", "https://example.com/team/source.git", "https://token@example.com/team/mirror.git", false},
		{"same local alias", filepath.Join(base, "repo"), filepath.Join(base, "repo", "."), false},
		{"same file URL", filepath.Join(base, "repo"), "file://" + filepath.Join(base, "repo"), false},
		{"remote helper syntax", "https://example.com/team/source.git", "helper::repo", false},
		{"malformed HTTPS", "https://example.com/team/source.git", "https:/team/mirror.git", false},
		{"SSH option as host", "https://example.com/team/source.git", "ssh://git@-unsafe/repo.git", false},
		{"SSH option in SCP address", "https://example.com/team/source.git", "git@-unsafe:repo.git", false},
		{"incomplete HTTPS", "https://example.com/team/source.git", "https://example.com", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), ".mirr.yaml")
			content := "version: 1\nrepositories:\n  app:\n    source:\n      url: " + tc.source + "\n    mirrors:\n      target:\n        url: " + tc.mirror + "\n"
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestSetModePreservesCommentsAndPaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mirr.yaml")
	input := `# keep this comment
version: 1
repositories:
  app:
    source:
      url: ./source
    mirrors:
      backup:
        url: ./backup
`
	if e := os.WriteFile(p, []byte(input), 0o600); e != nil {
		t.Fatal(e)
	}
	if e := SetMode(p, "app", "backup", "continuous"); e != nil {
		t.Fatal(e)
	}
	c, e := Load(p)
	if e != nil {
		t.Fatal(e)
	}
	if c.Repositories["app"].Mirrors["backup"].Mode != "continuous" {
		t.Fatal("mode not saved")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "# keep this comment") || !strings.Contains(string(b), "./source") {
		t.Fatal(string(b))
	}
}
