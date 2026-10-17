package generator

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/models"
)

var (
	quotedRe        = regexp.MustCompile(`["'“”‘’][^"'“”‘’]*["'“”‘’]`)
	parentheticalRe = regexp.MustCompile(`\([^)]*\)`)
)

// textInducingWords are scene phrases that make image models draw (garbled)
// text, so we strip clauses containing them.
var textInducingWords = []string{
	"label", "inscri", "text", "word", "sign", "banner", "title", "reads",
	"writ", "letter", "caption", "screen displaying", "monitor showing",
	"book", "document", "poster", "billboard", "headline", "tombstone",
	"manual", "page", "note", "map", "chart", "graph", "diagram", "code on",
}

// sanitizeScene removes quoted text, parentheticals, and clauses that would
// cause the image model to render words — keeping only pure visual description.
func sanitizeScene(s string) string {
	s = quotedRe.ReplaceAllString(s, "")
	s = parentheticalRe.ReplaceAllString(s, "")
	parts := strings.Split(s, ",")
	var kept []string
	for _, p := range parts {
		lp := strings.ToLower(p)
		bad := false
		for _, w := range textInducingWords {
			if strings.Contains(lp, w) {
				bad = true
				break
			}
		}
		if !bad {
			if t := strings.TrimSpace(p); t != "" {
				kept = append(kept, t)
			}
		}
	}
	return strings.TrimSpace(strings.Join(kept, ", "))
}

// pollinationsImageURL builds a free image-generation URL (Pollinations.ai).
// Fetching this URL renders a topic-relevant illustration — no API key needed.
func pollinationsImageURL(prompt string, seed int) string {
	return fmt.Sprintf(
		"https://image.pollinations.ai/prompt/%s?width=1280&height=960&nologo=true&model=flux&seed=%d",
		url.PathEscape(prompt), seed,
	)
}

// pollinationsPrompt wraps a content-specific scene in a clean, professional
// style. The explicit "no text" instruction avoids the garbled pseudo-text that
// AI image models produce, since they render real words poorly.
func pollinationsPrompt(scene, title string) string {
	subject := strings.TrimSpace(scene)
	if subject == "" {
		subject = fmt.Sprintf("a concept illustration of %q, connected devices, cloud, gears and data flow", title)
	}
	return fmt.Sprintf(
		"Flat modern vector illustration, isometric 3D style. %s. Minimal clean professional design, "+
			"dark blue gradient background, vibrant teal and purple accents, soft glowing elements, high detail. "+
			"No text, no words, no letters, no labels, no numbers, no captions, no watermark",
		subject,
	)
}

// imageSceneForTopic asks the LLM to describe a concrete visual scene for the
// topic (objects only, no text). This makes the generated image relate to the
// actual content instead of being a generic tech graphic. Falls back to "" on error.
func (g *LLMGenerator) imageSceneForTopic(ctx context.Context, topic models.Topic) string {
	prompt := fmt.Sprintf(`Describe, in ONE vivid sentence, a professional illustration scene that visually represents this topic.

Topic: %s
Summary: %s

Rules:
- Describe only concrete visual objects, characters, and setting (e.g. developers at laptops, servers, gears, shields, magnifying glass, connected nodes).
- Make it specific to the topic's meaning, not generic.
- Do NOT mention any signs, labels, inscriptions, screens with text, books, banners, tombstones, posters, or anything that contains words.
- Do NOT include any words, letters, quotes, or the topic title itself.
- Respond with ONLY the single sentence.`, topic.Title, topic.Summary)

	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You write concise visual scene descriptions for an AI image generator. No text elements."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		g.logger.Warn("image scene generation failed, using generic scene")
		return ""
	}
	scene := strings.TrimSpace(resp.Content)
	scene = stripPostPrefixes(scene)
	// Keep it short and single-line.
	if i := strings.IndexByte(scene, '\n'); i >= 0 {
		scene = strings.TrimSpace(scene[:i])
	}
	scene = sanitizeScene(scene)
	// Trim to a whole word within the length budget.
	if len(scene) > 260 {
		scene = scene[:260]
		if i := strings.LastIndexByte(scene, ' '); i > 0 {
			scene = scene[:i]
		}
	}
	return scene
}

// openAIImagePrompt builds a prompt for OpenAI, which renders text well — so we
// ask for a professional cover WITH the title as clean readable typography.
func openAIImagePrompt(title string) string {
	return fmt.Sprintf(
		"A modern, professional LinkedIn infographic cover image. Large clean readable title text: %q. "+
			"Flat vector illustration, dark blue gradient background, minimalistic, corporate style, "+
			"technology theme, vibrant blue, cyan, white and purple accents, soft glowing elements, "+
			"professional typography, high quality, ultra HD. No watermark, no logos.",
		title,
	)
}

// sketchnoteImagePrompt asks a paid image model (Gemini/OpenAI, which render
// text well) for the same hand-drawn sketchnote the free renderer draws,
// spelling out every piece of text so the image matches the post.
func sketchnoteImagePrompt(c models.NoteCard) string {
	plain := func(s string) string { return strings.NewReplacer("==", "", "[[", "", "]]", "").Replace(s) }
	var b strings.Builder
	b.WriteString("A realistic top-down photo of a hand-drawn sketchnote on a sheet of white paper lying on a desk, ")
	b.WriteString("with black and blue pens and a yellow highlighter beside it. Neat, legible handwriting in black and blue marker, ")
	b.WriteString("yellow highlighter on key phrases, simple doodles, boxes and arrows connecting ideas. ")
	fmt.Fprintf(&b, "Title at the top in large blue handwriting: %q. ", plain(c.Headline))
	if c.Subtitle != "" {
		fmt.Fprintf(&b, "Subtitle: %q. ", plain(c.Subtitle))
	}
	if c.Intro != "" {
		fmt.Fprintf(&b, "A handwritten note: %q. ", plain(c.Intro))
	}
	if c.Icon != "" {
		fmt.Fprintf(&b, "A doodle of a %s", c.Icon)
		if c.IconLabel != "" {
			fmt.Fprintf(&b, " labelled %q", plain(c.IconLabel))
		}
		b.WriteString(". ")
	}
	if c.Keyword != "" {
		fmt.Fprintf(&b, "A boxed word in capitals: %q. ", strings.ToUpper(plain(c.Keyword)))
	}
	if len(c.Points) > 0 {
		fmt.Fprintf(&b, "A small diagram titled %q listing: %s. ", plain(c.BoxTitle), plain(strings.Join(c.Points, "; ")))
	}
	if len(c.Notes) > 0 {
		fmt.Fprintf(&b, "Annotations with arrows: %s. ", plain(strings.Join(c.Notes, " / ")))
	}
	if c.Footer != "" {
		fmt.Fprintf(&b, "Highlighted near the bottom: \"The Lesson: %s\". ", plain(c.Footer))
	}
	if c.Question != "" {
		fmt.Fprintf(&b, "A speech bubble with: %q. ", plain(c.Question))
	}
	if len(c.Hashtags) > 0 {
		fmt.Fprintf(&b, "Hashtags in blue at the bottom: %s. ", strings.Join(c.Hashtags, " "))
	}
	b.WriteString("Spell every word exactly as given. No logos, no watermark.")
	return b.String()
}

// stableSeed derives a deterministic seed from the title so re-generating the
// same topic yields the same image (and different topics get different images).
func stableSeed(s string) int {
	h := 0
	for _, r := range s {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return h % 1000000
}
