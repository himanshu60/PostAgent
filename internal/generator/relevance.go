package generator

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/llm"
)

// TargetAudience describes whose LinkedIn feed the generated posts are for.
// Every auto-picked topic must be relevant to this audience.
const TargetAudience = "software engineers and developers (programming, tools, architecture, DevOps, AI for developers, engineering careers)"

// FilterRelevantTopics returns the indices of candidate titles that fit the
// target audience, in the LLM's order of preference (most relevant first).
// One batched call regardless of candidate count. Returns an error when the
// LLM call or parsing fails so callers can fall back to a keyword heuristic
// — it never guesses.
func (g *LLMGenerator) FilterRelevantTopics(ctx context.Context, titles []string) ([]int, error) {
	if len(titles) == 0 {
		return nil, nil
	}

	var list strings.Builder
	for i, t := range titles {
		fmt.Fprintf(&list, "%d. %s\n", i+1, t)
	}

	prompt := fmt.Sprintf(`You pick LinkedIn post topics for an audience of %s.

Candidate topics:
%s
Which topics are DIRECTLY relevant to this audience? Exclude consumer gadget news, business/finance stories, agriculture, entertainment, and anything a software engineer wouldn't post about professionally.

Answer with ONLY the numbers of relevant topics, most relevant first, comma-separated (e.g. "3,1,7"). If none are relevant, answer "none".`,
		TargetAudience, list.String())

	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a strict topic-relevance classifier. Respond with only comma-separated numbers or the word none."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("relevance filter: %w", err)
	}

	ans := strings.TrimSpace(resp.Content)
	if strings.EqualFold(ans, "none") {
		return []int{}, nil
	}

	var indices []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(ans, ",") {
		n, perr := strconv.Atoi(strings.TrimSpace(part))
		if perr != nil || n < 1 || n > len(titles) || seen[n] {
			continue
		}
		seen[n] = true
		indices = append(indices, n-1)
	}
	if len(indices) == 0 && len(ans) > 0 {
		return nil, fmt.Errorf("relevance filter: unparseable answer %q", ans)
	}

	g.logger.Info("Relevance filter kept topics",
		zap.Int("candidates", len(titles)), zap.Int("kept", len(indices)))
	return indices, nil
}
