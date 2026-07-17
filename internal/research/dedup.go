package research

import "strings"

// stopWords are excluded from title comparison so common connector words
// don't inflate similarity between otherwise-unrelated topics.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "in": true, "on": true,
	"for": true, "to": true, "and": true, "or": true, "is": true, "are": true,
	"how": true, "why": true, "what": true, "vs": true, "with": true, "your": true,
	"you": true, "will": true, "can": true, "new": true, "this": true, "that": true,
}

// wordSet normalizes a title into a set of significant lowercase words.
func wordSet(s string) map[string]bool {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	set := make(map[string]bool)
	for _, w := range strings.Fields(b.String()) {
		if len(w) <= 2 || stopWords[w] {
			continue
		}
		set[w] = true
	}
	return set
}

// jaccard returns the word-overlap similarity between two sets, 0 (no
// overlap) to 1 (identical).
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// DefaultDuplicateThreshold is the word-overlap ratio above which two titles
// are considered the same underlying topic, even if worded differently (e.g.
// a news story re-reported with a different headline). Used where being
// wrong in either direction is costly (an actual post going out), so it's
// deliberately conservative to avoid blocking genuinely fresh topics.
const DefaultDuplicateThreshold = 0.5

// IsDuplicateTitle reports whether candidate is an exact or near-duplicate
// (by word overlap) of any title already in history, at the default threshold.
func IsDuplicateTitle(candidate string, history []string) bool {
	return IsDuplicateTitleAt(candidate, history, DefaultDuplicateThreshold)
}

// devKeywords indicate a topic is software-engineering related. Used as the
// fallback relevance filter when the LLM classifier is unavailable (quota,
// outage) — deliberately generous, since it only runs when the better filter
// can't.
var devKeywords = []string{
	"software", "developer", "programming", "code", "coding", "engineer",
	"api", "devops", "cloud", "kubernetes", "docker", "database", "sql",
	"javascript", "python", "golang", "rust", "java", "typescript", "react",
	"backend", "frontend", "framework", "library", "open source", "opensource",
	"github", "git ", "linux", "server", "microservice", "architecture",
	"testing", "debug", "deploy", "ci/cd", "ci pipeline", "pipeline", "llm",
	"machine learning", "ai model", "agent", "compiler", "algorithm",
	"web dev", "security", "infrastructure", "observability", "sre",
	"refactor", "tech debt", "garbage collect", "runtime", "postgres",
	"mysql", "mongodb", "redis", "node.js", "npm", "sdk", "cli tool",
	"programming language", "golang", "rustlang", "vscode", "terminal",
}

// IsDevTopic reports whether a topic looks software-engineering related,
// using a keyword scan over its title and summary.
func IsDevTopic(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	for _, kw := range devKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// IsDuplicateTitleAt is IsDuplicateTitle with a caller-supplied threshold.
// Lower it (e.g. ~0.3) for low-stakes, easily-retried contexts like interactive
// idea suggestions, where being too aggressive just means one fewer suggestion
// shown rather than blocking something from ever being posted.
func IsDuplicateTitleAt(candidate string, history []string, threshold float64) bool {
	cSet := wordSet(candidate)
	if len(cSet) == 0 {
		return false
	}
	for _, h := range history {
		if jaccard(cSet, wordSet(h)) >= threshold {
			return true
		}
	}
	return false
}
