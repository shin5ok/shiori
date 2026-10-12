// Package configfile loads the shared application and deployment configuration.
package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"my-bookmark/internal/iap"
)

type Config struct {
	Tags      []string
	Allowlist iap.Allowlist
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	var c Config
	if len(data) > 64*1024 {
		return c, errors.New("config.yaml is too large")
	}
	var raw struct {
		Accounts []string `yaml:"accounts"`
		Domains  []string `yaml:"domains"`
		Tags     []string `yaml:"tags"`
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&raw); err != nil {
		return c, fmt.Errorf("parse config.yaml: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, errors.New("config.yaml must contain one document")
	}
	if len(raw.Tags) < 2 {
		return c, errors.New("config.yaml tags must contain at least two candidates")
	}
	seen := make(map[string]bool)
	for _, tag := range raw.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > 24 || strings.ContainsAny(tag, ",\r\n") || seen[tag] {
			return c, fmt.Errorf("invalid or duplicate tag %q", tag)
		}
		seen[tag] = true
		c.Tags = append(c.Tags, tag)
	}
	var err error
	c.Allowlist, err = iap.NewAllowlist(raw.Accounts, raw.Domains)
	return c, err
}
