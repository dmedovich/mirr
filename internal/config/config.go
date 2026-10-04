package config

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type Credential struct {
	Provider string `yaml:"provider"`
	Account  string `yaml:"account"`
	Variable string `yaml:"variable"`
	Username string `yaml:"username"`
}
type Endpoint struct {
	URL  string `yaml:"url"`
	Auth string `yaml:"auth"`
}
type Tags struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}
type Policy struct {
	Delete     bool `yaml:"delete"`
	AllowForce bool `yaml:"allow_force"`
}
type Settings struct {
	Mode   string  `yaml:"mode"`
	Tags   *Tags   `yaml:"tags"`
	Policy *Policy `yaml:"policy"`
}
type Mirror struct {
	Endpoint `yaml:",inline"`
	Settings `yaml:",inline"`
}
type Repository struct {
	Source   Endpoint          `yaml:"source"`
	Defaults Settings          `yaml:"defaults"`
	Mirrors  map[string]Mirror `yaml:"mirrors"`
}
type Config struct {
	Version      int                   `yaml:"version"`
	Credentials  map[string]Credential `yaml:"credentials"`
	Repositories map[string]Repository `yaml:"repositories"`
}

func (r Repository) Effective(m Mirror) Mirror {
	if m.Mode == "" {
		m.Mode = r.Defaults.Mode
	}
	if m.Mode == "" {
		m.Mode = "release"
	}
	if m.Tags == nil {
		m.Tags = r.Defaults.Tags
	}
	if m.Tags == nil {
		m.Tags = &Tags{Include: []string{"v*"}, Exclude: []string{"*-rc*", "*-beta*", "*-alpha*"}}
	}
	if m.Policy == nil {
		m.Policy = r.Defaults.Policy
	}
	if m.Policy == nil {
		m.Policy = &Policy{}
	}
	return m
}

func (m Mirror) Includes(ref string) bool {
	if m.Mode == "continuous" {
		return strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/tags/")
	}
	if !strings.HasPrefix(ref, "refs/tags/") {
		return false
	}
	name := strings.TrimPrefix(ref, "refs/tags/")
	included := false
	for _, p := range m.Tags.Include {
		ok, _ := path.Match(p, name)
		included = included || ok
	}
	for _, p := range m.Tags.Exclude {
		ok, _ := path.Match(p, name)
		if ok {
			return false
		}
	}
	return included
}

func Load(filename string) (*Config, error) {
	f, e := os.Open(filename)
	if e != nil {
		return nil, e
	}
	defer func() { _ = f.Close() }()
	var c Config
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if e = d.Decode(&c); e != nil {
		return nil, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return nil, fmt.Errorf("config must contain one YAML document")
	}
	if err := c.Validate(filename); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks and normalizes a configuration before it is used or written.
func (c *Config) Validate(filename string) error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if len(c.Repositories) == 0 {
		return fmt.Errorf("no repositories configured")
	}
	for name, cr := range c.Credentials {
		if cr.Provider != "keyring" && cr.Provider != "env" {
			return fmt.Errorf("credential %s: invalid provider", name)
		}
		if cr.Provider == "keyring" && cr.Account == "" || cr.Provider == "env" && cr.Variable == "" {
			return fmt.Errorf("credential %s: account or variable is required", name)
		}
	}
	base, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return err
	}
	check := func(ep *Endpoint) (string, error) {
		identity, err := normalizeEndpoint(ep, base)
		if err != nil {
			return "", err
		}
		if ep.Auth != "" {
			if _, ok := c.Credentials[ep.Auth]; !ok {
				return "", fmt.Errorf("unknown credential %s", ep.Auth)
			}
			if !strings.HasPrefix(ep.URL, "https://") {
				return "", fmt.Errorf("auth profiles require HTTPS")
			}
		}
		return identity, nil
	}
	for name, r := range c.Repositories {
		if !validName(name) {
			return fmt.Errorf("invalid repository name %q", name)
		}
		sourceIdentity, e := check(&r.Source)
		if e != nil {
			return fmt.Errorf("%s source: %w", name, e)
		}
		if len(r.Mirrors) == 0 {
			return fmt.Errorf("%s: no mirrors", name)
		}
		for mn, m := range r.Mirrors {
			if !validName(mn) {
				return fmt.Errorf("%s: invalid mirror name %q", name, mn)
			}
			mirrorIdentity, e := check(&m.Endpoint)
			if e != nil {
				return fmt.Errorf("%s/%s: %w", name, mn, e)
			}
			if mirrorIdentity == sourceIdentity {
				return fmt.Errorf("%s/%s: mirror equals source", name, mn)
			}
			r.Mirrors[mn] = m
			m = r.Effective(m)
			if m.Mode != "release" && m.Mode != "continuous" {
				return fmt.Errorf("%s/%s: invalid mode", name, mn)
			}
			for _, p := range append(append([]string{}, m.Tags.Include...), m.Tags.Exclude...) {
				if _, e = path.Match(p, ""); e != nil {
					return fmt.Errorf("invalid tag pattern %q", p)
				}
			}
		}
		c.Repositories[name] = r
	}
	return nil
}

func validName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) < 0
}
