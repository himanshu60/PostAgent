package generator

import (
	"fmt"
	"strings"
)

func topicIdeaPrompt(query string, examples []string, avoidTitles []string, focusCategories []string) string {
	examplesSection := ""
	if len(examples) > 0 {
		examplesSection = fmt.Sprintf(`
Here are examples of the user's past LinkedIn posts (for style reference):
%s

`, strings.Join(examples, "\n---\n"))
	}

	avoidSection := ""
	if len(avoidTitles) > 0 {
		capped := avoidTitles
		if len(capped) > 40 {
			capped = capped[:40] // keep the prompt small; most-recent titles matter most
		}
		avoidSection = fmt.Sprintf(`
Topics ALREADY covered recently — do NOT suggest anything substantially similar to these, even if reworded differently:
%s

`, strings.Join(capped, "\n"))
	}

	focusSection := ""
	if len(focusCategories) > 0 {
		focusSection = fmt.Sprintf("\nFor variety this round, actively lean toward these categories where they fit naturally, rather than defaulting to your usual go-to topics: %s.\n", strings.Join(focusCategories, "; "))
	}

	return fmt.Sprintf(`You are a LinkedIn content strategist for a software engineer's professional page. The user wants to write about "%s".

%s%s%sGenerate 5 specific, CREATIVE, and DIVERSE topic ideas related to this theme that would make great LinkedIn posts.

Requirements:
- Each of the 5 ideas must take a genuinely different angle from the others — do not just rephrase the same idea five times.
- Do not default to a generic "AI" angle unless the query is specifically about AI.
- Avoid the single most obvious, overused angle on this topic; find a fresher perspective a busy engineer would actually stop scrolling for.
- A compelling title (under 12 words)
- A one-sentence hook
- 1-2 sentence explanation

Format each topic exactly like this (no markdown, no numbering):
TOPIC: <title>
HOOK: <hook sentence>
WHY: <explanation>

Separate each topic with a blank line.`, query, examplesSection, avoidSection, focusSection)
}

func topicRefinerPrompt(input TopicRefinerInput) string {
	return fmt.Sprintf(`You are a LinkedIn content strategist. Given a topic, suggest 3 different angles for a LinkedIn post.

Each angle should have:
- A compelling title (under 10 words)
- A hook sentence (under 20 words) that grabs attention

Topic: %s
Summary: %s
Keywords: %s

Respond with exactly 3 angles in this format (no markdown, no numbering):
Angle 1: <title> | <hook>
Angle 2: <title> | <hook>
Angle 3: <title> | <hook>`, input.Title, input.Summary, formatKeywords(input.Keywords))
}

// postFormat is one genuinely different post SHAPE (not the same skeleton
// reworded), so a feed of these reads like a person wrote them on different
// days. Some are lists, most are prose. Where a format uses numbered "N."
// lines, "Step N:" or Myth/Reality-style labels, normalizeBody/boldHeadings
// space and bold them; everything else passes through untouched.
type postFormat struct {
	Name string
	// ShowTitle publishes the bold title line above the post. Guides and lists
	// suit a headline; stories and opinions read more human without one.
	ShowTitle bool
	Emoji     string   // emojiNone | emojiLight | emojiHeadings | emojiMarks
	Lengths   []string // allowed length targets
	Spec      string
}

const (
	emojiNone     = "none"
	emojiLight    = "light"
	emojiHeadings = "headings"
	emojiMarks    = "marks"

	lengthShort  = "SHORT: 400-800 characters"
	lengthMedium = "MEDIUM: 800-1300 characters"
	lengthLong   = "LONG: 1300-1900 characters"
)

var postFormats = []postFormat{
	{Name: "listicle", ShowTitle: true, Emoji: emojiHeadings, Lengths: []string{lengthMedium, lengthLong}, Spec: `A numbered list of 5 points.

<1-2 line hook>

<1 line on why this matters>

<emoji> 1. <short heading>
<1-2 line explanation>

(continue to exactly 5 points, each with a different leading emoji)

📌 Example:
<one concrete real-world example>`},

	{Name: "story", Emoji: emojiNone, Lengths: []string{lengthMedium, lengthLong}, Spec: `A first-person story with a lesson. No lists, no numbers.

<1-2 line hook that drops the reader into a real moment>

<4 to 6 SHORT paragraphs (1-3 lines each): the situation, what went wrong or surprised you, the turning point, what you do differently now. Use specific details: a tool, a number, a time of day.>`},

	{Name: "one-idea", Emoji: emojiLight, Lengths: []string{lengthMedium}, Spec: `One sharp idea explored in prose. No lists.

<1-2 line hook stating the idea>

<2 to 4 SHORT paragraphs: what it means, why most people miss it, why it matters now>

<one short paragraph with a concrete real-world example that makes it click>`},

	{Name: "hot-take", Emoji: emojiNone, Lengths: []string{lengthShort, lengthMedium}, Spec: `A bold opinion. Punchy. No lists.

<1-2 line contrarian claim that makes people stop scrolling>

<2 to 3 SHORT paragraphs defending it with reasoning and one concrete detail>

<one honest nuance, e.g. "To be fair, ...">`},

	{Name: "quick-lessons", ShowTitle: true, Emoji: emojiNone, Lengths: []string{lengthShort, lengthMedium}, Spec: `Three quick lessons, kept tight.

<1 line hook>

1. <lesson in one line>
<one sentence on why>

2. <lesson in one line>
<one sentence on why>

3. <lesson in one line>
<one sentence on why>`},

	{Name: "then-vs-now", Emoji: emojiLight, Lengths: []string{lengthMedium}, Spec: `A then-vs-now comparison.

<1-2 line hook contrasting the old way of thinking with the new one>

Then: <how people used to think or do it, 1-2 lines>
Now: <what changed, 1-2 lines>

<2 to 3 SHORT paragraphs on what the shift means for engineers today, with one concrete example>`},

	{Name: "mistakes", Emoji: emojiNone, Lengths: []string{lengthMedium}, Spec: `Mistakes you made, and what you do now. First person.

<1-2 line hook admitting the mistakes>

<3 to 4 mistakes. Each is one line starting with "→ " naming the mistake, then one line on what you do instead now>

<1-2 line reflection>`},

	{Name: "how-to", ShowTitle: true, Emoji: emojiNone, Lengths: []string{lengthMedium, lengthLong}, Spec: `A practical step-by-step guide.

<1-2 line hook naming the problem it solves>

Step 1: <action, one line>
<optional short detail: a tool, command, or setting>

(continue to 4-6 steps, same pattern)

<1 line on the result readers should expect>`},

	{Name: "myth-vs-reality", ShowTitle: true, Emoji: emojiMarks, Lengths: []string{lengthMedium}, Spec: `Myths vs reality.

<1-2 line hook>

❌ Myth: <a common belief>
✅ Reality: <what's actually true, 1-2 lines>

(exactly 3 myth/reality pairs, a blank line between pairs)`},

	{Name: "checklist", ShowTitle: true, Emoji: emojiMarks, Lengths: []string{lengthShort, lengthMedium}, Spec: `A save-worthy checklist.

<1-2 line hook saying who this is for>

✅ <short, specific item>
✅ <short, specific item>

(5 to 7 items, each on its own line)

<1 line on why this checklist matters>`},

	{Name: "punchy", Emoji: emojiNone, Lengths: []string{lengthShort}, Spec: `Ultra-short, spoken rhythm. No lists.

<5 to 8 short sentences, each on its own line, with blank lines between small groups. Every line earns its place.>`},

	{Name: "question-led", Emoji: emojiNone, Lengths: []string{lengthShort, lengthMedium}, Spec: `A reflective post built around a real question.

<open with a genuine question you've been thinking about>

<2 to 3 SHORT paragraphs exploring different angles honestly>

<where you landed, in 1-2 lines>`},

	{Name: "framework", ShowTitle: true, Emoji: emojiLight, Lengths: []string{lengthMedium}, Spec: `A simple mental model or rule of thumb, with a name.

<1-2 line hook>

<name the framework in one line, e.g. "I use a 3-question rule:">

→ <part 1 in one line>
→ <part 2 in one line>
→ <part 3 in one line>

<2 short paragraphs: how you apply it, with one concrete example>`},

	{Name: "do-dont", ShowTitle: true, Emoji: emojiMarks, Lengths: []string{lengthShort, lengthMedium}, Spec: `Do's and don'ts.

<1-2 line hook>

Do:
✅ <short item>
✅ <short item>
✅ <short item>

Don't:
❌ <short item>
❌ <short item>
❌ <short item>

<1 line takeaway>`},
}

// recentFormatsToAvoid is how many of a user's most recent formats are skipped
// when picking the next one, so the same shape never shows up back to back.
const recentFormatsToAvoid = 5

// hookStyles and closingStyles vary the opening and ending independently of
// the format, so even two posts of the same format start and end differently.
var hookStyles = []string{
	"Open with a specific number or metric that surprised you.",
	"Open with a short confession about a mistake you made.",
	"Open with a bold, slightly contrarian one-liner.",
	"Open with a one-line scene from a real workday (a 2 AM page, a code review comment, a standup).",
	"Open with a question a junior engineer once asked you.",
	"Open with a common belief, then flip it on the second line.",
	"Open with a punchy sentence of 3 to 7 words.",
}

var closingStyles = []string{
	"End with one specific question that invites readers to share their own experience. Not a generic \"Thoughts?\".",
	"End with a short, punchy one-line takeaway. No question, no call to action.",
	"End by asking whether readers agree or disagree, and why.",
	"End with one practical thing to try this week. No question.",
	"End with a one-line personal reflection, like you're telling a friend. No question.",
	"End by asking readers what they would add.",
}

// postPlan is the randomized recipe for one post.
type postPlan struct {
	Format  postFormat
	Hook    string
	Closing string
	Length  string
}

// pickFormat chooses a format at random, skipping the `avoid` most recent
// distinct formats in recent (most-recent-first).
func pickFormat(recent []string, avoid int, intn func(int) int) postFormat {
	skip := make(map[string]bool, avoid)
	for _, name := range recent {
		if len(skip) >= avoid {
			break
		}
		skip[name] = true
	}
	var pool []postFormat
	for _, f := range postFormats {
		if !skip[f.Name] {
			pool = append(pool, f)
		}
	}
	if len(pool) == 0 {
		pool = postFormats
	}
	return pool[intn(len(pool))]
}

func newPostPlan(f postFormat, intn func(int) int) postPlan {
	return postPlan{
		Format:  f,
		Hook:    hookStyles[intn(len(hookStyles))],
		Closing: closingStyles[intn(len(closingStyles))],
		Length:  f.Lengths[intn(len(f.Lengths))],
	}
}

func emojiRule(f postFormat, maxEmoji int) string {
	light := min(maxEmoji, 2)
	switch {
	case f.Emoji == emojiMarks:
		return "Use only the ✅ / ❌ marks shown in the format. No other emojis."
	case f.Emoji == emojiHeadings:
		return fmt.Sprintf("Each numbered point starts with one emoji. Beyond those, at most %d more.", light)
	case f.Emoji == emojiLight && light > 0:
		return fmt.Sprintf("At most %d emojis, only where they add meaning.", light)
	default:
		return "No emojis at all."
	}
}

// singleShotPrompt produces the final LinkedIn post in ONE LLM call (structure,
// polish, and hashtags together). Local models are slow, so collapsing the old
// refine→write→polish pipeline into one call cuts generation time by ~3x.
func singleShotPrompt(title, summary, tone, styleGuide string, maxEmoji int, plan postPlan) string {
	if strings.TrimSpace(styleGuide) != "" {
		styleGuide += "\n(Use these notes for voice only. The format below decides the structure.)\n"
	}
	return fmt.Sprintf(`You are a Senior Software Engineer writing your OWN LinkedIn post, not marketing copy.

Write like a real person sharing hard-won experience: natural, specific, a little opinionated. Teach the idea the way you'd explain it to a smart junior developer over coffee.

Topic: %s
Context: %s
Tone: %s
%s
Write the post in this format:

%s

Length: %s
Hook: %s (If the format already dictates the opening, follow the format.)
Ending: %s
Emojis: %s

After the post, write a line with three dashes, then the hashtags:

---

Hashtags: <5 hashtags people actually follow on LinkedIn: 3 specific to this exact topic (e.g. #Kubernetes, #GenerativeAI) and 2 broad ones with big audiences (e.g. #ArtificialIntelligence, #SoftwareEngineering, #Technology). Most-followed spelling only, no near-duplicates (never both #LLM and #LLMs), nothing invented or very long.>

Rules:
- Follow the format above. Never print the word "FORMAT" or any <placeholder> brackets.
- Sound human: concrete details, real tools and numbers where they fit, a genuine point of view. Vary sentence length. Contractions are fine.
- Never use these AI-sounding words or phrases: delve, leverage, game-changer, unlock, landscape, realm, tapestry, navigate, harness, elevate, seamless, robust, cutting-edge, revolutionize, supercharge, "in today's fast-paced world", "in the ever-evolving", "let's dive in", "buckle up", "it's not just X, it's Y".
- Use at most one em dash (—) in the whole post.
- Put a BLANK line between paragraphs.
- Plain English for a mixed audience (beginners, developers, managers, founders).
- No markdown (**, ##, backticks). LinkedIn doesn't render it.

IMPORTANT: Output ONLY the post, starting directly with the hook. No title line, no preamble like "Here is the post".`,
		title, summary, tone, styleGuide, plan.Format.Spec, plan.Length, plan.Hook, plan.Closing, emojiRule(plan.Format, maxEmoji))
}

func draftWriterPrompt(input DraftWriterInput) string {
	styleSection := input.StyleGuide

	return fmt.Sprintf(`You are a Senior Software Engineer, Technical Writer, and LinkedIn Top Voice.

Do not just write ABOUT the topic — TEACH it, as if explaining to a junior developer who is smart but new to it.

Topic: %s
Angle: %s
Opening hook idea: %s
Tone: %s
%s
Write the post using EXACTLY this structure:

<1-2 line hook that grabs attention>

<1-2 line intro explaining why this matters>

Here are the key points 👇

<emoji> 1. <short heading>
<1-2 line explanation>

<emoji> 2. <short heading>
<1-2 line explanation>

(continue for 3 to 6 numbered points, each with a different leading emoji)

📌 Example:
<one concrete real-world example>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question to encourage comments>

Rules:
- Every numbered point MUST start with an emoji, the number, and a short heading, e.g. "🎯 1. Simplicity".
- Explain in very simple English for a mixed audience (beginners, developers, managers, founders).
- Keep paragraphs short (1-3 lines) with a blank line between them.
- Use at most %d emojis overall beyond the point headings.
- Keep the ENTIRE post under 2000 characters.
- Do NOT use markdown symbols like ## or backticks. LinkedIn does not render them.

IMPORTANT: Output ONLY the post body, starting directly with the hook. Do NOT write the title line, and do NOT include any preamble like "Here is the post".`,
		input.Title, input.Angle.Title, input.Angle.Hook, input.BrandVoice.Tone,
		styleSection, input.BrandVoice.MaxEmoji)
}

func polisherPrompt(input PolisherInput) string {
	styleSection := input.StyleGuide

	return fmt.Sprintf(`Polish this LinkedIn post for maximum engagement while keeping its structure and meaning:

%s

Requirements:
- Fix any grammar or awkward phrasing.
- Keep short paragraphs (1-3 lines each) with blank lines between them.
- Keep the numbered points and their leading emoji headings intact.
- Use at most %d emojis, naturally.
- Keep the entire post under 2000 characters.
- Tone: %s.
- Suggest 3-5 relevant hashtags: 1 broad one for the field and 2-4 specific to the topic.
%s
Respond in this format ONLY (no markdown, no meta-text, no introductions):

[polished post body]

---

Hashtags: #tag1 #tag2 #tag3

IMPORTANT: Start directly with the polished post body. Do NOT include any phrases like "Here is the polished post" or "Here's your post". Just output the post body directly.`, input.Body,
		input.BrandVoice.MaxEmoji, input.BrandVoice.Tone, styleSection)
}

func formatKeywords(kw []string) string {
	if len(kw) == 0 {
		return "N/A"
	}
	result := ""
	for i, k := range kw {
		if i > 0 {
			result += ", "
		}
		result += k
	}
	return result
}
