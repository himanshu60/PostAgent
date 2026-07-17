package generator

import (
	"fmt"
	"math/rand"
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

// singleShotPrompt produces the final LinkedIn post in ONE LLM call (structure,
// polish, and hashtags together). Local models are slow, so collapsing the old
// refine→write→polish pipeline into one call cuts generation time by ~3x.
// postStructures are format variants rotated between posts so the feed
// doesn't look templated. Every variant keeps the same required skeleton —
// 5 numbered emoji-headed points, a 📌 Example, a 💡 Takeaway, and a closing
// 💬 question — but frames the points differently. The numbered "emoji N.
// heading" line format is load-bearing: normalizeBody/boldHeadings depend on
// it for spacing and Unicode-bold styling.
var postStructures = []string{
	// 1. Classic teaching breakdown
	`<1-2 line hook that grabs attention>

<1-2 line intro explaining why this matters>

<emoji> 1. <short heading>
<1-2 line explanation>

(continue for exactly 5 numbered points, each with a different leading emoji)

📌 Example:
<one concrete real-world example>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question to encourage comments>`,

	// 2. Myth vs reality
	`<1-2 line hook challenging a common belief about the topic>

<1-2 line intro: why these misconceptions cost engineers real time/money>

<emoji> 1. <a common myth about the topic, stated as the myth>
Reality: <1-2 lines correcting it>

(continue for exactly 5 numbered myths, each with a different leading emoji and a "Reality:" line)

📌 Example:
<one concrete real-world example where believing a myth went wrong>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question asking which myth readers have encountered>`,

	// 3. Mistakes and fixes
	`<1-2 line hook: a bold claim about mistakes people make with this topic>

<2-3 line short personal-experience style intro ("I've seen this go wrong...")>

<emoji> 1. <short heading naming the mistake>
The fix: <1-2 lines on what to do instead>

(continue for exactly 5 numbered mistakes, each with a different leading emoji and a "The fix:" line)

📌 Example:
<one concrete real-world example of the most costly mistake>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question asking readers about mistakes they've seen>`,

	// 4. Question-driven
	`<1-2 line hook posed as the big question readers are asking>

<1-2 line intro on why this question matters right now>

<emoji> 1. <short question as the heading>
<1-2 line direct answer>

(continue for exactly 5 numbered questions, each with a different leading emoji)

📌 Example:
<one concrete real-world example that ties the answers together>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question inviting readers' own answers>`,

	// 5. Do this, not that
	`<1-2 line hook contrasting how most people do it vs how experienced engineers do it>

<1-2 line intro on the gap between the two>

<emoji> 1. <short heading naming the practice>
Do: <one line best practice>
Not: <one line common anti-pattern>

(continue for exactly 5 numbered practices, each with a different leading emoji and Do:/Not: lines)

📌 Example:
<one concrete real-world example showing the "Do" paying off>

💡 Takeaway: <one valuable takeaway line>

💬 <one discussion question to encourage comments>`,
}

func pickRandomStructure() string {
	return postStructures[rand.Intn(len(postStructures))]
}

func singleShotPrompt(title, summary, tone, styleGuide string, maxEmoji int, structure string) string {
	return fmt.Sprintf(`You are a Senior Software Engineer, Technical Writer, and LinkedIn Top Voice.

Do not just write ABOUT the topic — TEACH it, as if explaining to a junior developer who is smart but new to it.

Topic: %s
Context: %s
Tone: %s
%s
Write the post using EXACTLY this structure:

%s

---

Hashtags: <10-15 relevant hashtags starting with #>

Rules:
- Every numbered point MUST start with an emoji, the number, and a short heading on its OWN line, e.g. "🎯 1. Simplicity", with its explanation on the NEXT line(s).
- Put a BLANK line between every section and every numbered point.
- Explain in very simple English for a mixed audience (beginners, developers, managers, founders).
- Use at most %d emojis beyond the point headings.
- Keep the ENTIRE post under 2000 characters.
- Do NOT use markdown like **, ##, or backticks. LinkedIn does not render them.

IMPORTANT: Output ONLY the post starting directly with the hook. No title line, no preamble like "Here is the post".`,
		title, summary, tone, styleGuide, structure, maxEmoji)
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
- Suggest 10-15 relevant, specific hashtags (mix broad and niche).
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
