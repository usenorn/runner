package entity

import (
	"path"
	"path/filepath"
	"strings"
)

type IgnoreDecision int

const (
	IgnoreKeep IgnoreDecision = iota
	IgnoreSkip
	IgnoreDenied
)

func Denylist() []string {
	return []string{
		".norn/",
		".env",
		".env.*",
		"*.pem",
		"*.key",
		"*.p12",
		"*.pfx",
		"*.keystore",
		"id_rsa*",
		"id_dsa*",
		"id_ecdsa*",
		"id_ed25519*",
		".netrc",
		".npmrc",
		".pypirc",
		".ssh/",
		".aws/",
		".azure/",
		".gcloud/",
		"**/.config/gcloud/",
		"**/.kube/config",
		"**/.docker/config.json",
	}
}

func DefaultIgnores() []string {
	return []string{
		"node_modules/",
		"dist/",
		"build/",
		"out/",
		".next/",
		".nuxt/",
		".svelte-kit/",
		".turbo/",
		"target/",
		"__pycache__/",
		".venv/",
		"venv/",
		".tox/",
		".gradle/",
		".terraform/",
		".cache/",
		".pytest_cache/",
		".mypy_cache/",
		".idea/",
		".vscode/",
		".fleet/",
		".DS_Store",
		"*.log",
		"pgdata/",
		"mysql-data/",
		"redis-data/",
		"valkey-data/",
	}
}

type IgnoreRule struct {
	segments []string
	negated  bool
	dirOnly  bool
	anchored bool
}

func ParseIgnore(text string) []IgnoreRule {
	lines := strings.Split(text, "\n")
	rules := make([]IgnoreRule, 0, len(lines))

	for _, line := range lines {
		rule, ok := parseIgnoreLine(line)
		if !ok {
			continue
		}

		rules = append(rules, rule)
	}

	return rules
}

func parseIgnoreLine(line string) (IgnoreRule, bool) {
	pattern := strings.TrimRight(line, " \t\r")
	if pattern == "" || strings.HasPrefix(pattern, "#") {
		return IgnoreRule{}, false
	}

	rule := IgnoreRule{}

	if after, found := strings.CutPrefix(pattern, "!"); found {
		rule.negated = true
		pattern = after
	}

	if before, found := strings.CutSuffix(pattern, "/"); found {
		rule.dirOnly = true
		pattern = before
	}

	rule.anchored = strings.Contains(pattern, "/")
	pattern = strings.Trim(pattern, "/")

	if pattern == "" {
		return IgnoreRule{}, false
	}

	rule.segments = strings.Split(pattern, "/")

	return rule, true
}

func (r IgnoreRule) matches(relPath string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}

	segments := strings.Split(relPath, "/")

	if r.anchored {
		return matchSegments(r.segments, segments)
	}

	return matchSegments(r.segments, segments[len(segments)-1:])
}

func matchSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}

	if pattern[0] == "**" {
		for index := range len(segments) + 1 {
			if matchSegments(pattern[1:], segments[index:]) {
				return true
			}
		}

		return false
	}

	if len(segments) == 0 {
		return false
	}

	if matched, err := path.Match(pattern[0], segments[0]); err != nil || !matched {
		return false
	}

	return matchSegments(pattern[1:], segments[1:])
}

type IgnoreSet struct {
	denied []IgnoreRule
	rules  []IgnoreRule
}

func NewIgnoreSet(layers ...[]IgnoreRule) IgnoreSet {
	set := IgnoreSet{
		denied: ParseIgnore(strings.Join(Denylist(), "\n")),
		rules:  ParseIgnore(strings.Join(DefaultIgnores(), "\n")),
	}

	for _, layer := range layers {
		set.rules = append(set.rules, layer...)
	}

	return set
}

func (s IgnoreSet) Decide(relPath string, isDir bool) IgnoreDecision {
	cleaned := path.Clean(filepath.ToSlash(relPath))
	if cleaned == "." || cleaned == "/" {
		return IgnoreKeep
	}

	segments := strings.Split(strings.Trim(cleaned, "/"), "/")

	for index := range segments {
		partial := strings.Join(segments[:index+1], "/")
		leaf := index == len(segments)-1

		if decision := s.decide(partial, isDir || !leaf); decision != IgnoreKeep {
			return decision
		}
	}

	return IgnoreKeep
}

func (s IgnoreSet) decide(relPath string, isDir bool) IgnoreDecision {
	for _, rule := range s.denied {
		if rule.matches(relPath, isDir) {
			return IgnoreDenied
		}
	}

	decision := IgnoreKeep

	for _, rule := range s.rules {
		if !rule.matches(relPath, isDir) {
			continue
		}

		if rule.negated {
			decision = IgnoreKeep
		} else {
			decision = IgnoreSkip
		}
	}

	return decision
}
