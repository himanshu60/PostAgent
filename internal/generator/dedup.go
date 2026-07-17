package generator

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/llm"
)

// semanticHistoryLimit caps how many past titles are sent to the LLM for the
// duplicate check, to keep the prompt small and fast.
const semanticHistoryLimit = 30

// IsSameTopic asks the LLM whether candidate covers substantially the same
// underlying subject as any already-published topic, even if reworded
// differently. Used as a semantic backstop after a cheap word-overlap filter
// (which catches exact/near-exact repeats but misses looser paraphrases).
// On any LLM error, fails open (returns false) so a transient API issue never
// blocks the whole day's generation.
func (g *LLMGenerator) IsSameTopic(ctx context.Context, candidate string, history []string) bool {
	if len(history) == 0 {
		return false
	}
	if len(history) > semanticHistoryLimit {
		history = history[:semanticHistoryLimit]
	}

	prompt := fmt.Sprintf(`Already-published LinkedIn post topics:
%s

New candidate topic: %q

Does the candidate cover substantially the SAME underlying subject as any already-published topic above, even if worded differently? Answer with exactly one word: YES or NO.`,
		strings.Join(history, "\n"), candidate)

	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a strict topic-deduplication checker. Respond with only YES or NO, nothing else."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		g.logger.Warn("Semantic duplicate check failed, treating as not-duplicate", zap.Error(err))
		return false
	}

	ans := strings.ToUpper(strings.TrimSpace(resp.Content))
	return strings.HasPrefix(ans, "YES")
}
