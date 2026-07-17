package generator

import (
	"regexp"
	"strings"
)

var markdownBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)

// pointHeadingRe matches a numbered section-heading line, with an optional
// leading emoji/symbol prefix, e.g. "🎯 1. Simplicity" or "1. Code is King".
// The prefix allows any non-word runes so multi-rune emojis (🏋️‍♂️ etc.) match.
var pointHeadingRe = regexp.MustCompile(`^([^\w\n]{0,12}?)(\d{1,2}[.)]\s+)(\S.*?)\s*$`)

// labelHeadingRe matches label-style headings like "📌 Example:" or "Takeaway:".
var labelHeadingRe = regexp.MustCompile(`^([^\w\n]{0,12}?)(Example|Takeaway|Pro Tip|Key Takeaway|Bottom line|TL;DR|Discussion question|Final Thought)(\s*:)`)

// normalizeBody enforces the post's visual rhythm regardless of what the LLM
// produced: a blank line before every section heading, no runs of 3+ newlines,
// and no trailing whitespace. Runs BEFORE bolding (it needs ASCII digits).
func normalizeBody(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		isHeading := pointHeadingRe.MatchString(ln) || labelHeadingRe.MatchString(ln)
		if isHeading && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "") // blank line before each heading
		}
		out = append(out, ln)
	}
	joined := strings.Join(out, "\n")
	for strings.Contains(joined, "\n\n\n") {
		joined = strings.ReplaceAll(joined, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(joined)
}

// boldHeadings converts numbered section headings and known labels to Unicode
// bold so they stand out on LinkedIn. It is idempotent: already-bold (non-ASCII)
// characters are left untouched by toBoldRunes.
func boldHeadings(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Numbered heading: short line, bold the whole thing.
		if len([]rune(trimmed)) <= 80 {
			if m := pointHeadingRe.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + toBoldRunes(m[2]+m[3])
				continue
			}
		}
		if m := labelHeadingRe.FindStringSubmatch(line); m != nil {
			rest := line[len(m[0]):]
			lines[i] = m[1] + toBoldRunes(m[2]) + m[3] + rest
		}
	}
	return strings.Join(lines, "\n")
}

// convertMarkdownBold turns markdown/asterisk bold (**text** or __text__) into
// Unicode bold characters, which LinkedIn renders as bold (it ignores markdown).
// Any stray, unmatched asterisks are then removed so they don't show literally.
func convertMarkdownBold(s string) string {
	s = markdownBoldRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.Trim(m, "*_")
		return toBoldRunes(inner)
	})
	// Drop leftover markdown emphasis markers that weren't part of a pair.
	s = strings.ReplaceAll(s, "**", "")
	return s
}

// evergreenTags are broad, tech-focused hashtags used to top up a post so it
// always has a healthy hashtag count, since small models are unreliable here.
var evergreenTags = []string{
	"#SoftwareDevelopment", "#SoftwareEngineering", "#Programming", "#Coding",
	"#WebDevelopment", "#BackendDevelopment", "#TechCommunity", "#Technology",
	"#DeveloperCommunity", "#CloudComputing", "#SystemDesign", "#Developers",
}

// ensureHashtags merges the model's hashtags with topic keywords and evergreen
// tags (deduped, case-insensitive) to reach a target of ~12 relevant hashtags.
func ensureHashtags(existing []string, keywords []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(tag string) {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "#")
		// LinkedIn hashtags allow only letters and digits — "#CI/CD" would
		// break at the slash. Strip everything else.
		var clean strings.Builder
		for _, r := range tag {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				clean.WriteRune(r)
			}
		}
		tag = clean.String()
		if tag == "" {
			return
		}
		tag = "#" + tag
		key := strings.ToLower(tag)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, tag)
	}

	for _, h := range existing {
		add(h)
	}
	for _, k := range keywords {
		add("#" + pascalCase(k))
	}
	for _, e := range evergreenTags {
		if len(out) >= 12 {
			break
		}
		add(e)
	}
	if len(out) > 15 {
		out = out[:15]
	}
	return out
}

// pascalCase turns "remote work" into "RemoteWork" for use in a hashtag.
func pascalCase(s string) string {
	var b strings.Builder
	for _, word := range strings.Fields(s) {
		r := []rune(word)
		b.WriteString(strings.ToUpper(string(r[0])))
		if len(r) > 1 {
			b.WriteString(string(r[1:]))
		}
	}
	return b.String()
}

// toBoldRunes maps ASCII letters/digits to Unicode mathematical sans-serif bold.
func toBoldRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(0x1D5D4 + (r - 'A'))
		case r >= 'a' && r <= 'z':
			b.WriteRune(0x1D5EE + (r - 'a'))
		case r >= '0' && r <= '9':
			b.WriteRune(0x1D7EC + (r - '0'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
