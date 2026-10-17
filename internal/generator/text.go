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
var labelHeadingRe = regexp.MustCompile(`^([^\w\n]{0,12}?)(Example|Takeaway|Pro Tip|Key Takeaway|Bottom line|TL;DR|Discussion question|Final Thought|Step \d{1,2})(\s*:)`)

// pairLabelRe matches labels that come in tight pairs ("Myth:" / "Reality:",
// "Then:" / "Now:", "Do:" / "Don't:"). They're bolded like other labels but
// don't get a blank line forced before them, so each pair stays together.
var pairLabelRe = regexp.MustCompile(`^([^\w\n]{0,12}?)(Myth|Reality|Then|Now|Do|Don't|Don’t)(\s*:)`)

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
		m := labelHeadingRe.FindStringSubmatch(line)
		if m == nil {
			m = pairLabelRe.FindStringSubmatch(line)
		}
		if m != nil {
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
