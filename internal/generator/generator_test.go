package generator

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/notecard"
)

// fakeLLM returns canned replies in order (or an error when reply is "ERR").
type fakeLLM struct{ replies []string }

func (f *fakeLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(f.replies) == 0 {
		return nil, errors.New("no reply queued")
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	if r == "ERR" {
		return nil, errors.New("llm down")
	}
	return &llm.ChatResponse{Content: r}, nil
}
func (f *fakeLLM) Provider() string { return "fake" }

func lowerSet(tags []string) map[string]bool {
	m := map[string]bool{}
	for _, t := range tags {
		m[strings.ToLower(t)] = true
	}
	return m
}

func TestSelectHashtagsTopicRelevantAndBounded(t *testing.T) {
	topic := models.Topic{
		Title:    "Why your Kubernetes cluster costs too much",
		Summary:  "Right-sizing pods and autoscaling in the cloud",
		Keywords: []string{"Show", "HN", "Kubernetes", "Hacker", "News"},
	}
	body := "We cut our cloud bill by 40% after fixing Kubernetes requests. Autoscaling and DevOps habits matter."
	model := []string{"#K8s", "#CI/CD", "#the", "#DevOps", "#AwesomeInfraTipsForEveryoneEverywhere2026"}

	got := selectHashtags(model, topic, body)
	if len(got) < minHashtags || len(got) > maxHashtags {
		t.Fatalf("want %d-%d hashtags, got %d: %v", minHashtags, maxHashtags, len(got), got)
	}
	if got[0] != "#Kubernetes" {
		t.Errorf("strongest topic tag should lead, got %v", got)
	}
	set := lowerSet(got)
	for _, want := range []string{"#kubernetes", "#devops", "#cloudcomputing", "#technology"} {
		if !set[want] {
			t.Errorf("missing %s (topic tags + field tag + reach tag), got %v", want, got)
		}
	}
	for _, junk := range []string{"#the", "#show", "#hn", "#hacker", "#news", "#k8s"} {
		if set[junk] {
			t.Errorf("junk or duplicate hashtag %s leaked: %v", junk, got)
		}
	}
	for _, tag := range got {
		if strings.ContainsAny(tag[1:], "#/ -.") {
			t.Errorf("hashtag %q has characters LinkedIn would break on", tag)
		}
	}
}

func TestSelectHashtagsFoldsDuplicatesForReach(t *testing.T) {
	// The draft that showed #LLM #LLMs #AI #SoftwareEngineering.
	topic := models.Topic{Title: "Show HN: Gemini 4 Argon plays StarCraft"}
	body := "My StarSkirmish Bench throws LLMs into StarCraft Brood War. Flagship models from OpenAI, Anthropic and Google. The LLMs write bot code on the fly; the games expose their strategy."
	got := selectHashtags([]string{"#LLM", "#LLMs", "#AI", "#SoftwareEngineering"}, topic, body)
	set := lowerSet(got)
	if set["#llms"] || set["#ai"] {
		t.Errorf("variants must fold into one tag: %v", got)
	}
	for _, want := range []string{"#llm", "#generativeai", "#artificialintelligence", "#technology"} {
		if !set[want] {
			t.Errorf("missing %s, got %v", want, got)
		}
	}
	if len(got) != maxHashtags {
		t.Errorf("a well-matched post should use all %d slots, got %v", maxHashtags, got)
	}
}

func TestSelectHashtagsCareerReachesEngineers(t *testing.T) {
	got := selectHashtags(nil, models.Topic{Title: "What I wish I knew before my first tech interview"}, "Interviewing taught me more about my career than any course.")
	set := lowerSet(got)
	if !set["#techinterviews"] || !set["#careergrowth"] || !set["#softwareengineering"] {
		t.Errorf("career post should get topic + #CareerGrowth + #SoftwareEngineering, got %v", got)
	}
}

func TestSelectHashtagsWholeWordMatching(t *testing.T) {
	// "maintain" contains "ai", "restful" contains "rest" — neither should match.
	topic := models.Topic{Title: "How to maintain a calm on-call rotation"}
	got := selectHashtags(nil, topic, "Rest well, and the rest of the week goes fine.")
	set := lowerSet(got)
	if set["#artificialintelligence"] || set["#apis"] {
		t.Errorf("substring false positive: %v", got)
	}
	if !set["#sre"] {
		t.Errorf("expected #SRE for on-call topic, got %v", got)
	}
}

func TestSelectHashtagsAlwaysHasMinimum(t *testing.T) {
	got := selectHashtags(nil, models.Topic{Title: "Some thoughts"}, "")
	if len(got) < minHashtags {
		t.Fatalf("want at least %d, got %v", minHashtags, got)
	}
	seen := map[string]bool{}
	for _, tag := range got {
		if seen[strings.ToLower(tag)] {
			t.Fatalf("duplicate %s in %v", tag, got)
		}
		seen[strings.ToLower(tag)] = true
	}
}

func TestCleanHashtagCanonicalCasing(t *testing.T) {
	for in, want := range map[string]string{
		"#kubernetes": "#Kubernetes", "devops": "#DevOps", "#CI/CD": "#CICD",
		"#123": "", "#a": "", "#Show": "", "#FeatureFlags": "#FeatureFlags",
		"#LLMs": "#LLM", "#AI": "#ArtificialIntelligence", "#k8s": "#Kubernetes", "#GenAI": "#GenerativeAI",
	} {
		if got := cleanHashtag(in); got != want {
			t.Errorf("cleanHashtag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickFormatAvoidsRecent(t *testing.T) {
	recent := []string{"listicle", "story", "hot-take", "how-to", "punchy"}
	for i := 0; i < 200; i++ {
		f := pickFormat(recent, recentFormatsToAvoid, rand.Intn)
		for _, r := range recent {
			if f.Name == r {
				t.Fatalf("picked recently used format %q", f.Name)
			}
		}
	}
	// Everything recent: still returns something.
	var all []string
	for _, f := range postFormats {
		all = append(all, f.Name)
	}
	if f := pickFormat(all, len(all), rand.Intn); f.Name == "" {
		t.Fatal("must pick a format even when all are recent")
	}
}

func TestPostFormatsAreWellFormed(t *testing.T) {
	names := map[string]bool{}
	for _, f := range postFormats {
		if f.Name == "" || f.Spec == "" || len(f.Lengths) == 0 {
			t.Errorf("incomplete format %+v", f)
		}
		if names[f.Name] {
			t.Errorf("duplicate format name %q", f.Name)
		}
		names[f.Name] = true
		if strings.Contains(f.Spec, "---") {
			t.Errorf("format %q contains '---', which parsePolished uses as the hashtag separator", f.Name)
		}
	}
	if len(postFormats) <= recentFormatsToAvoid {
		t.Fatal("need more formats than the avoid window, or picks become forced")
	}
}

func TestChooseFormatRotatesWithinBatch(t *testing.T) {
	g := NewLLMGenerator(&fakeLLM{}, zap.NewNop())
	seen := map[string]bool{}
	for i := 0; i < recentFormatsToAvoid+1; i++ {
		f := g.chooseFormat(context.Background(), "user-1")
		if seen[f.Name] {
			t.Fatalf("format %q repeated within %d consecutive posts", f.Name, recentFormatsToAvoid+1)
		}
		seen[f.Name] = true
	}
}

func TestChooseFormatUsesStoredHistory(t *testing.T) {
	g := NewLLMGenerator(&fakeLLM{}, zap.NewNop())
	stored := []string{"listicle", "story", "one-idea", "hot-take", "quick-lessons"}
	g.SetFormatHistory(func(_ context.Context, userID string, limit int) ([]string, error) {
		return stored, nil
	})
	for i := 0; i < 50; i++ {
		g.recentFormats = map[string][]string{} // only the DB history should matter
		f := g.chooseFormat(context.Background(), "user-1")
		for _, s := range stored {
			if f.Name == s {
				t.Fatalf("picked %q despite it being in stored history", f.Name)
			}
		}
	}
}

func TestEmojiRule(t *testing.T) {
	byName := map[string]postFormat{}
	for _, f := range postFormats {
		byName[f.Name] = f
	}
	if r := emojiRule(byName["story"], 3); !strings.Contains(r, "No emojis") {
		t.Errorf("story should be emoji-free, got %q", r)
	}
	if r := emojiRule(byName["one-idea"], 0); !strings.Contains(r, "No emojis") {
		t.Errorf("MaxEmoji=0 must disable light emoji, got %q", r)
	}
	if r := emojiRule(byName["one-idea"], 5); !strings.Contains(r, "2") {
		t.Errorf("light emoji should cap at 2, got %q", r)
	}
}

func TestSingleShotPromptIncludesPlan(t *testing.T) {
	plan := newPostPlan(postFormats[1], func(int) int { return 0 })
	p := singleShotPrompt("Topic X", "ctx", "casual", "style notes", 3, plan)
	for _, want := range []string{"Topic X", plan.Format.Spec, plan.Hook, plan.Closing, plan.Length, "5 hashtags people actually follow", "near-duplicates", "voice only"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBoldHeadingsNewLabels(t *testing.T) {
	in := normalizeBody("Hook line\nStep 1: Write the test\n❌ Myth: Tests slow you down\n✅ Reality: They speed you up\nDo:\nDon't:")
	out := boldHeadings(in)
	if !strings.Contains(out, "\n\n"+toBoldRunes("Step 1")+":") {
		t.Errorf("Step label should be spaced and bolded:\n%s", out)
	}
	if !strings.Contains(out, toBoldRunes("Myth")+": Tests slow you down\n✅ "+toBoldRunes("Reality")+":") {
		t.Errorf("Myth/Reality pair should be bolded and stay together:\n%s", out)
	}
	if !strings.Contains(out, toBoldRunes("Do")+":") || !strings.Contains(out, toBoldRunes("Don't")+":") {
		t.Errorf("Do/Don't should be bolded:\n%s", out)
	}
	if strings.Contains(out, toBoldRunes("Do you")) {
		t.Error("ordinary sentences must not be bolded")
	}
}

func TestParseNoteCard(t *testing.T) {
	c := parseNoteCard(`Here is the sketchnote:
**TITLE:** Stop guessing, start measuring
SUBTITLE: Most slowdowns hide in ==three places==
INTRO: I spent a week tuning the wrong query.
ICON: Rocket.
ICON_LABEL: Our API
Callout - What's your ==p99==?
KEYWORD: Profiling
BOX TITLE: Where time goes
ITEM 1: N+1 queries #perf
- ITEM: Cold caches
ITEM: Chatty services
ITEM: Big payloads
ITEM: one too many
NOTE: Averages lie, percentiles don't
NOTE: Measure in prod, not on a laptop
NOTE: extra note
LESSON: "Profile ==before== you optimize."
QUESTION: What's your go-to profiler?`)
	want := models.NoteCard{
		Headline: "Stop guessing, start measuring", Subtitle: "Most slowdowns hide in ==three places==",
		Intro: "I spent a week tuning the wrong query.", Icon: "rocket", IconLabel: "Our API",
		Callout: "What's your ==p99==?", Keyword: "Profiling", BoxTitle: "Where time goes",
		Points: []string{"N+1 queries", "Cold caches", "Chatty services", "Big payloads"},
		Notes:  []string{"Averages lie, percentiles don't", "Measure in prod, not on a laptop"},
		Footer: "Profile ==before== you optimize.", Question: "What's your go-to profiler?",
	}
	if fmt.Sprintf("%+v", c) != fmt.Sprintf("%+v", want) {
		t.Errorf("parseNoteCard =\n%+v\nwant\n%+v", c, want)
	}
}

func TestFallbackNoteCardFromHeadings(t *testing.T) {
	body := "Hook here.\n\nWe tried it on two teams.\n\n🎯 1. Keep it simple\nWhy it matters.\n\n🔥 2. Measure first\nMore.\n\n💡 Takeaway: Small steps win.\n\n💬 What would you add?"
	c := fallbackNoteCard("My Title", body)
	if c.Headline != "My Title" || len(c.Points) != 2 || c.Points[0] != "Keep it simple" || c.Footer != "Small steps win." {
		t.Errorf("unexpected fallback card: %+v", c)
	}
	if c.Subtitle != "Hook here." || c.Intro != "We tried it on two teams." || c.Question != "What would you add?" {
		t.Errorf("subtitle/intro/question not derived from the post: %+v", c)
	}
}

func TestFallbackNoteCardFromProse(t *testing.T) {
	body := "DeepSeek Flash isn't just fast; it redefines the sweet spot for quality and latency in production AI apps today.\n\nMany chase raw speed benchmarks. The real win is coherent output.\n\nYou don't have to trade speed for quality anymore.\n\n📌 In practice:\nAn AI pair programmer answers in under a second.\n\n💡 Takeaway: Pick the right model for your latency budget.\n\n💬 What app suffers most from latency?"
	c := fallbackNoteCard("DeepSeek: Flash 1017", body)
	if c.Subtitle != "DeepSeek Flash isn't just fast" {
		t.Errorf("long hook should be cut to its first clause, got %q", c.Subtitle)
	}
	if c.Intro != "Many chase raw speed benchmarks. The real win is coherent output." {
		t.Errorf("intro = %q", c.Intro)
	}
	if len(c.Points) != 0 {
		t.Errorf("prose must not become diagram items: %q", c.Points)
	}
	if len(c.Notes) != 2 || c.Notes[1] != "An AI pair programmer answers in under a second." {
		t.Errorf("notes = %q (the 'In practice:' label must be skipped)", c.Notes)
	}
	if c.Keyword != "DeepSeek" || c.Footer != "Pick the right model for your latency budget." || c.Question != "What app suffers most from latency?" {
		t.Errorf("keyword/lesson/question wrong: %+v", c)
	}
}

func TestNoteCardForUsesLLMThenFallsBack(t *testing.T) {
	g := NewLLMGenerator(&fakeLLM{replies: []string{"TITLE: Ship small\nICON: rocket\nITEM: Tiny PRs\nITEM: Fast reviews\nLESSON: Momentum wins"}}, zap.NewNop())
	c := g.NoteCardFor(context.Background(), "u", "Title", "Hook.\n\nNote here.", []string{"#A", "#B"})
	if c.Headline != "Ship small" || len(c.Points) != 2 || c.Footer != "Momentum wins" || c.Icon != "rocket" {
		t.Errorf("LLM card not used: %+v", c)
	}
	if c.Subtitle != "Hook." || c.Intro != "Note here." {
		t.Errorf("missing LLM fields should be filled from the post: %+v", c)
	}
	if c.Style == "" || len(c.Hashtags) != 2 {
		t.Errorf("style/hashtags not set: %+v", c)
	}

	g = NewLLMGenerator(&fakeLLM{replies: []string{"ERR"}}, zap.NewNop())
	c = g.NoteCardFor(context.Background(), "u", "Fallback title about Kubernetes", "Hook.\n\n1. First thing\n\n2. Second thing", nil)
	if c.Headline != "Fallback title about Kubernetes" || len(c.Points) != 2 || c.Icon != "server" {
		t.Errorf("fallback card not used on LLM error: %+v", c)
	}
	if _, err := notecard.Render(*c); err != nil {
		t.Fatalf("fallback card must render: %v", err)
	}
}

func TestNoteCardForRejectsUnknownIcon(t *testing.T) {
	g := NewLLMGenerator(&fakeLLM{replies: []string{"TITLE: X\nICON: unicorn"}}, zap.NewNop())
	if c := g.NoteCardFor(context.Background(), "u", "Database indexing", "Indexes matter.", nil); c.Icon != "database" {
		t.Errorf("unknown icon should be replaced by a guess, got %q", c.Icon)
	}
}

func TestSketchnoteImagePrompt(t *testing.T) {
	p := sketchnoteImagePrompt(models.NoteCard{Headline: "Ship small", Subtitle: "Tiny ==PRs== win", Points: []string{"A", "B"}, BoxTitle: "Rules", Footer: "Go", Hashtags: []string{"#X"}})
	for _, want := range []string{`"Ship small"`, `"Tiny PRs win"`, "A; B", "The Lesson: Go", "#X", "Spell every word"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "==") {
		t.Error("highlight markers must not leak into the image prompt")
	}
}

func TestNextCardStyleVaries(t *testing.T) {
	g := NewLLMGenerator(&fakeLLM{}, zap.NewNop())
	prev := g.nextCardStyle("u")
	for i := 0; i < 30; i++ {
		s := g.nextCardStyle("u")
		if s == prev {
			t.Fatalf("same card style %q twice in a row", s)
		}
		prev = s
	}
}

func TestGenerateFromTopicEndToEnd(t *testing.T) {
	post := "Most teams over-provision Kubernetes.\n\nWe cut 40% of our cloud bill.\n\n---\n\nHashtags: #Kubernetes #DevOps #CloudComputing #SoftwareEngineering"
	card := "TITLE: Right-size your pods\nICON: cloud\nITEM: Measure real usage\nITEM: Set requests honestly\nITEM: Autoscale on metrics\nLESSON: Pay for what you use"

	g := NewLLMGenerator(&fakeLLM{replies: []string{post, card}}, zap.NewNop())
	d, err := g.GenerateFromTopic(context.Background(), models.Topic{ID: "t1", UserID: "u1", Title: "Kubernetes cost"}, BrandVoice{Tone: "casual", MaxEmoji: 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Format == "" {
		t.Error("draft format not recorded")
	}
	var f postFormat
	for _, pf := range postFormats {
		if pf.Name == d.Format {
			f = pf
		}
	}
	if d.HideTitle == f.ShowTitle {
		t.Errorf("HideTitle=%v inconsistent with format %q ShowTitle=%v", d.HideTitle, d.Format, f.ShowTitle)
	}
	if strings.Contains(d.Body, "#Kubernetes") || strings.Contains(d.Body, "---") {
		t.Errorf("hashtags leaked into body: %q", d.Body)
	}
	if len(d.Hashtags) < minHashtags || len(d.Hashtags) > maxHashtags || d.Hashtags[0] != "#Kubernetes" {
		t.Errorf("hashtags = %v", d.Hashtags)
	}
	if d.Media == nil || d.Media.Card == nil || d.Media.SourceURL != "" || d.Media.Prompt == "" {
		t.Fatalf("expected a handwritten card with a stored prompt, got %+v", d.Media)
	}
	if d.Media.Card.Headline != "Right-size your pods" || !strings.HasPrefix(d.Media.AltText, "Handwritten sketchnote:") {
		t.Errorf("card/alt not from LLM: %+v", d.Media)
	}
	if len(d.Media.Card.Hashtags) == 0 || d.Media.Card.Hashtags[0] != d.Hashtags[0] {
		t.Errorf("card should carry the post's hashtags: %v vs %v", d.Media.Card.Hashtags, d.Hashtags)
	}
	if !strings.Contains(d.Media.Prompt, "sketchnote") {
		t.Errorf("paid-provider prompt should describe the sketchnote, got %q", d.Media.Prompt)
	}
}

func TestGenerateFromTopicPollinationsStillSupported(t *testing.T) {
	post := "Body text.\n\n---\n\nHashtags: #DevOps"
	g := NewLLMGenerator(&fakeLLM{replies: []string{post, "developers at laptops with servers"}}, zap.NewNop())
	g.SetImageProvider("Pollinations")
	d, err := g.GenerateFromTopic(context.Background(), models.Topic{Title: "CI pipelines"}, BrandVoice{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Media == nil || d.Media.Card != nil || !strings.HasPrefix(d.Media.SourceURL, "https://image.pollinations.ai/") {
		t.Fatalf("pollinations provider should keep the legacy URL image, got %+v", d.Media)
	}
}
