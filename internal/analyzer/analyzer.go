package analyzer

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func Analyze(posts []ExamplePost) *PostPattern {
	if len(posts) == 0 {
		return &PostPattern{
			Tone:            "professional",
			StructureFormat: "hook-body-cta",
		}
	}

	totalLen := 0
	totalEmoji := 0
	totalHashtags := 0
	totalBreaks := 0.0

	var bodies []string
	for _, p := range posts {
		bodies = append(bodies, p.Body)
		totalLen += utf8.RuneCountInString(p.Body)
		totalEmoji += countEmojis(p.Body)
		totalHashtags += countHashtags(p.Body)
		totalBreaks += float64(strings.Count(p.Body, "\n\n"))
	}

	n := float64(len(posts))
	return &PostPattern{
		AvgLength:       totalLen / len(posts),
		AvgEmojiCount:   int(float64(totalEmoji) / n),
		AvgHashtagCount: totalHashtags / len(posts),
		AvgLineBreaks:   totalBreaks / n,
		Tone:            detectTone(bodies),
		StructureFormat: detectStructure(bodies),
		RecentBodies:    bodies,
		PostCount:       len(posts),
	}
}

func (p *PostPattern) PromptSuffix() string {
	if p.PostCount == 0 {
		return ""
	}

	return fmt.Sprintf(`
--- Writing Style Guide (based on your %d recent posts) ---
- Average post length: %d characters
- Average emoji usage: %d per post
- Average hashtags: %d per post
- Average line breaks: %.0f (paragraph separations)
- Dominant structure: %s
- Typical tone: %s

Match this style closely. Write in YOUR authentic voice, not generic LinkedIn influencer style.`, p.PostCount, p.AvgLength, p.AvgEmojiCount, p.AvgHashtagCount, p.AvgLineBreaks, p.StructureFormat, p.Tone)
}

func countEmojis(s string) int {
	count := 0
	for _, r := range s {
		if r > 0x1F600 && r < 0x1F9FF {
			count++
		}
	}
	return count
}

func countHashtags(s string) int {
	count := 0
	for _, word := range strings.Fields(s) {
		if strings.HasPrefix(word, "#") {
			count++
		}
	}
	return count
}

func detectTone(bodies []string) string {
	all := strings.Join(bodies, " ")
	allLower := strings.ToLower(all)

	analytical := countWords(allLower, []string{"analyze", "research", "data", "study", "findings", "approach", "framework"})
	personal := countWords(allLower, []string{"i", "my", "we", "our", "learned", "experience", "journey"})
	action := countWords(allLower, []string{"try", "start", "build", "create", "implement", "do"})

	switch {
	case analytical > personal && analytical > action:
		return "thought-leadership"
	case personal > action:
		return "personal-story"
	default:
		return "professional"
	}
}

func detectStructure(bodies []string) string {
	for _, body := range bodies {
		lines := strings.Split(strings.TrimSpace(body), "\n")
		if len(lines) >= 1 {
			first := strings.TrimSpace(lines[0])
			if strings.HasSuffix(first, "?") {
				return "question-hook-body-cta"
			}
		}
		if strings.Contains(strings.ToLower(body), "here are") || strings.Contains(body, "1.") {
			return "list-based"
		}
	}
	return "hook-body-cta"
}

func countWords(s string, words []string) int {
	count := 0
	for _, w := range words {
		count += strings.Count(s, " "+w+" ")
	}
	return count
}
