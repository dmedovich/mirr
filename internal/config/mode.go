package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SetMode edits the YAML node tree so comments and relative URLs survive a TUI edit.
func SetMode(filename, repository, mirror, mode string) error {
	if mode != "release" && mode != "continuous" {
		return fmt.Errorf("invalid mode")
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	lookup := func(n *yaml.Node, key string) *yaml.Node {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
		return nil
	}
	if len(doc.Content) != 1 {
		return fmt.Errorf("invalid YAML document")
	}
	n := lookup(lookup(lookup(lookup(doc.Content[0], "repositories"), repository), "mirrors"), mirror)
	if n == nil {
		return fmt.Errorf("mirror no longer exists")
	}
	if v := lookup(n, "mode"); v != nil {
		v.Value = mode
		v.Tag = "!!str"
	} else {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "mode"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mode})
	}
	b, err = yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("mode editing requires a regular config file")
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".mirr-config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}
