package generator

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/notecard"
)

// recentStylesToAvoid keeps a user's consecutive images from repeating style.
const recentStylesToAvoid = 2

// noteLineRe matches "TITLE: ...", "ITEM 2: ...", "**Lesson** - ..." etc.
// Longer labels come first so "icon_label" isn't read as "icon".
var noteLineRe = regexp.MustCompile(`(?i)^\W*(icon[ _]label|box[ _]title|headline|title|subtitle|intro|icon|callout|keyword|item|point|note|lesson|footer|takeaway|question)\W*\d*\s*[:.\-–]\s*(.+)$`)

// NoteCardFor builds a handwritten sketchnote card for a post: the LLM
// distils the post into a title, highlighted subtitle, first-person note,
// doodle, diagram items, annotations, lesson and question. Anything the LLM
// misses (or everything, if it's unavailable) is filled from the post itself.
// The style is picked to differ from this user's last few cards.
func (g *LLMGenerator) NoteCardFor(ctx context.Context, userID, title, body string, hashtags []string) *models.NoteCard {
	plain := notecard.Unbold(body)
	card := g.noteCardText(ctx, title, plain)
	fb := fallbackNoteCard(title, plain)
	if card.Headline == "" {
		card.Headline = fb.Headline
	}
	if len(card.Points) < 2 {
		card.Points = fb.Points
	}
	if card.Subtitle == "" {
		card.Subtitle = fb.Subtitle
	}
	if card.Intro == "" {
		card.Intro = fb.Intro
	}
	if card.Footer == "" {
		card.Footer = fb.Footer
	}
	if card.Question == "" {
		card.Question = fb.Question
	}
	if len(card.Notes) == 0 {
		card.Notes = fb.Notes
	}
	if card.Keyword == "" {
		card.Keyword = fb.Keyword
	}
	if !notecard.IsIcon(card.Icon) {
		card.Icon = notecard.GuessIcon(title + " " + plain)
	}
	if len(hashtags) > 5 {
		hashtags = hashtags[:5]
	}
	card.Hashtags = append([]string(nil), hashtags...)
	card.Style = g.nextCardStyle(userID)
	card.Seed = rand.Int63()
	return &card
}

func (g *LLMGenerator) noteCardText(ctx context.Context, title, body string) models.NoteCard {
	if r := []rune(body); len(r) > 2500 {
		body = string(r[:2500])
	}
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You turn LinkedIn posts into hand-drawn sketchnotes. Respond only in the requested format."},
			{Role: "user", Content: noteCardPrompt(title, body)},
		},
	})
	if err != nil {
		g.logger.Warn("Sketchnote text generation failed, building it from the post", zap.Error(err))
		return models.NoteCard{}
	}
	return parseNoteCard(resp.Content)
}

func noteCardPrompt(title, body string) string {
	return fmt.Sprintf(`Turn this LinkedIn post into the text for a hand-drawn sketchnote: one page of handwritten notes an engineer sketches to explain the idea, with a doodle, a small diagram and arrows.

Post title: %s
Post:
%s

Reply in EXACTLY this format, one field per line, nothing else:
TITLE: <3-7 words>
SUBTITLE: <one line, max 12 words; wrap the key phrase in ==double equals==>
INTRO: <1-2 first-person sentences, max 28 words, like a quick personal note>
ICON: <one of: %s>
ICON_LABEL: <2-4 words naming what the doodle stands for>
CALLOUT: <a short question or thought, max 10 words, with one ==highlighted== word or phrase>
KEYWORD: <1-2 words: the star concept, tool or name>
BOX_TITLE: <2-6 words: what the diagram lists>
ITEM: <1-4 words>
ITEM: <1-4 words>
ITEM: <1-4 words>
NOTE: <one annotation, max 14 words>
NOTE: <one annotation, max 14 words>
LESSON: <1-2 sentences, max 26 words, with one ==highlighted== phrase>
QUESTION: <one closing question for readers, max 12 words>

Rules:
- 3 or 4 ITEM lines (concrete things from the post) and exactly 2 NOTE lines.
- Plain words: no emojis, no hashtags, no numbering, no markdown other than ==highlight==.
- Concrete beats generic ("Cache at the edge", not "Optimize performance").`, title, body, strings.Join(notecard.IconNames, ", "))
}

func parseNoteCard(text string) models.NoteCard {
	var c models.NoteCard
	set := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	for _, line := range strings.Split(stripPostPrefixes(text), "\n") {
		m := noteLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		v := cleanNoteText(m[2])
		if v == "" {
			continue
		}
		switch strings.ReplaceAll(strings.ToLower(m[1]), " ", "_") {
		case "title", "headline":
			set(&c.Headline, v)
		case "subtitle":
			set(&c.Subtitle, v)
		case "intro":
			set(&c.Intro, v)
		case "icon":
			set(&c.Icon, strings.ToLower(strings.Trim(v, ".")))
		case "icon_label":
			set(&c.IconLabel, v)
		case "callout":
			set(&c.Callout, v)
		case "keyword":
			set(&c.Keyword, v)
		case "box_title":
			set(&c.BoxTitle, v)
		case "item", "point":
			if len(c.Points) < notecard.MaxPoints {
				c.Points = append(c.Points, v)
			}
		case "note":
			if len(c.Notes) < notecard.MaxNotes {
				c.Notes = append(c.Notes, v)
			}
		case "question":
			set(&c.Question, v)
		default: // lesson, footer, takeaway
			set(&c.Footer, v)
		}
	}
	return c
}

// cleanNoteText strips markdown/quote wrappers and any hashtags, keeping the
// ==highlight== markers the renderer understands.
func cleanNoteText(s string) string {
	var kept []string
	for _, w := range strings.Fields(s) {
		if !strings.HasPrefix(w, "#") {
			kept = append(kept, w)
		}
	}
	return strings.Trim(strings.Join(kept, " "), "*_`\"' ")
}

// fallbackNoteCard derives a sketchnote from the post itself: the hook becomes
// the subtitle, the next paragraph the personal note, numbered/label headings
// and bullet lines the diagram items, other short sentences the annotations, a
// "Takeaway:"-style line (or the last statement) the lesson, and the closing
// question the question. A "Name: ..." title puts Name in the keyword box.
func fallbackNoteCard(title, body string) models.NoteCard {
	c := models.NoteCard{Headline: strings.TrimSpace(title)}
	if i := strings.Index(c.Headline, ":"); i > 0 && i <= 20 {
		c.Keyword = strings.TrimSpace(c.Headline[:i])
	}
	var paragraphs []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if m := pointHeadingRe.FindStringSubmatch(t); m != nil && len([]rune(t)) <= 80 {
			c.Points = append(c.Points, m[3])
			continue
		}
		if m := labelHeadingRe.FindStringSubmatch(t); m != nil {
			rest := strings.TrimSpace(t[len(m[0]):])
			switch m[2] {
			case "Takeaway", "Key Takeaway", "Bottom line", "TL;DR", "Final Thought":
				if c.Footer == "" {
					c.Footer = rest
				}
			default:
				if rest != "" {
					c.Points = append(c.Points, rest)
				}
			}
			continue
		}
		if item := strings.TrimLeft(t, "→-•✅✔❌ "); item != t && item != "" {
			c.Points = append(c.Points, item)
			continue
		}
		if strings.HasSuffix(t, ":") && len([]rune(t)) <= 40 {
			continue // a label like "📌 In practice:" introduces the next line
		}
		if q := strings.TrimLeft(t, "💬 "); strings.HasSuffix(q, "?") && len(q) <= 110 {
			c.Question = q // the last question in the post wins
			continue
		}
		paragraphs = append(paragraphs, t)
	}
	if len(paragraphs) > 0 {
		s := firstSentence(paragraphs[0])
		if len(s) > 110 {
			for _, sep := range []string{"; ", ": ", " - ", ", but "} {
				if i := strings.Index(s, sep); i >= 20 {
					s = s[:i]
					break
				}
			}
		}
		if len(s) <= 110 {
			c.Subtitle = s
		}
	}
	if len(paragraphs) > 1 {
		c.Intro = firstSentences(paragraphs[1], 170)
	}
	rest := paragraphs[min(len(paragraphs), 2):]
	if c.Footer == "" && len(rest) > 0 {
		c.Footer = firstSentences(rest[len(rest)-1], 170)
		rest = rest[:len(rest)-1]
	}
	for _, p := range rest {
		if len(c.Notes) == notecard.MaxNotes {
			break
		}
		if s := firstSentence(p); len(s) >= 12 && len(s) <= 96 {
			c.Notes = append(c.Notes, s)
		}
	}
	if len(c.Points) < 2 {
		c.Points = nil // a one-item diagram looks broken; prose stays as notes
	}
	if len(c.Points) > notecard.MaxPoints {
		c.Points = c.Points[:notecard.MaxPoints]
	}
	return c
}

func firstSentence(p string) string {
	for i, r := range p {
		if (r == '.' || r == '?' || r == '!') && (i+1 == len(p) || p[i+1] == ' ') {
			return strings.TrimSpace(p[:i+1])
		}
	}
	return strings.TrimSpace(p)
}

// firstSentences returns as many leading sentences of p as fit in max bytes.
func firstSentences(p string, max int) string {
	out := ""
	for p != "" {
		s := firstSentence(p)
		if len(out)+len(s)+1 > max {
			break
		}
		out = strings.TrimSpace(out + " " + s)
		p = strings.TrimSpace(strings.TrimPrefix(p, s))
	}
	return out
}

// nextCardStyle picks a card style different from this user's recent ones.
func (g *LLMGenerator) nextCardStyle(userID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	recent := g.recentStyles[userID]
	style := notecard.RandomStyle(rand.New(rand.NewSource(rand.Int63())), recent...)
	g.recentStyles[userID] = prependCapped(style, recent, recentStylesToAvoid)
	return style
}

// prependCapped returns [v, list...] truncated to n entries.
func prependCapped(v string, list []string, n int) []string {
	out := append([]string{v}, list...)
	if len(out) > n {
		out = out[:n]
	}
	return out
}
