package entity

import (
	"regexp"
	"slices"
	"strings"
)

const (
	ScrubbedAddress   = "an email address"
	ScrubbedTrailer   = "a co-author trailer"
	ScrubbedAttribute = "an assistant attribution"
	ScrubbedSecret    = "a secret"
)

var ticketQuery = regexp.MustCompile(`(ticket=)[A-Za-z0-9._~+/=-]+`)

var forgeScrubs = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{
		kind:    ScrubbedTrailer,
		pattern: regexp.MustCompile(`(?im)^[ \t]*(?:co-authored-by|signed-off-by):.*$\n?`),
	},
	{
		kind:    ScrubbedAttribute,
		pattern: regexp.MustCompile(`(?im)^.*(?:generated with|co-authored with|written by) .*(?:claude|codex|copilot|chatgpt|gemini).*$\n?`),
	},
	{
		kind:    ScrubbedSecret,
		pattern: regexp.MustCompile(`\b(?:nrn|nrr|nrs|nrt|nru)_[A-Za-z0-9._~+/=-]{8,}`),
	},
	{
		kind:    ScrubbedSecret,
		pattern: regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{16,}|\bgithub_pat_[A-Za-z0-9_]{16,}|\bglpat-[A-Za-z0-9_-]{16,}`),
	},
	{
		kind:    ScrubbedAddress,
		pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	},
}

func Redacted(text string) string {
	return ticketQuery.ReplaceAllString(text, "${1}…")
}

func ScrubbedForForge(text string) (string, []string) {
	kinds := make([]string, 0, len(forgeScrubs))
	scrubbed := text

	for _, scrub := range forgeScrubs {
		if !scrub.pattern.MatchString(scrubbed) {
			continue
		}

		if !slices.Contains(kinds, scrub.kind) {
			kinds = append(kinds, scrub.kind)
		}

		scrubbed = scrub.pattern.ReplaceAllStringFunc(scrubbed, func(found string) string {
			if strings.HasSuffix(found, "\n") {
				return ""
			}

			if strings.ContainsAny(found, "\n") {
				return ""
			}

			return "…"
		})
	}

	if len(kinds) == 0 {
		return text, nil
	}

	return strings.TrimSpace(collapseBlankLines(scrubbed)), kinds
}

func collapseBlankLines(text string) string {
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	return text
}
