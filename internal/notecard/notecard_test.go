package notecard

import (
	"bytes"
	"fmt"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

var sampleCards = []models.NoteCard{
	{ // Full sketchnote, like a hand-drawn summary of a Show HN post.
		Headline:  "Show HN: Heldkeys",
		Subtitle:  "See which credentials your ==AI coding agent== can reach",
		Intro:     "I've been leaning heavily on AI agents for boilerplate, refactoring, and debugging.",
		Icon:      "robot",
		IconLabel: "AI Coding Agent",
		Callout:   "What does it really have ==access== to?",
		Keyword:   "Heldkeys",
		BoxTitle:  "Local dev & CI/CD pipeline",
		Points:    []string{".env files", "AWS keys", "SSH keys", "DB logins"},
		Notes: []string{
			"It lives in my shell, IDE, or CI runner. It can REACH things!",
			"Not about AI 'stealing' credentials, but being prompted to expose them.",
		},
		Footer:   "Your AI agent inherits the ==security context== of its environment. Know its credential reach.",
		Question: "What security practices do you use for AI agents?",
		Hashtags: []string{"#AIAgents", "#CyberSecurity", "#DevOps", "#SoftwareEngineering"},
	},
	{ // Dense content to exercise shrinking.
		Headline:  "Code reviews that actually help your team ship faster without the drama",
		Subtitle:  "Small PRs and kind comments beat ==heroic reviewers== every single time, in every team I've worked with",
		Intro:     "Last quarter our median PR sat for 3 days. Nobody was lazy; the PRs were just too big to review in one sitting, so they waited.",
		Icon:      "users",
		IconLabel: "The whole team",
		Callout:   "Would you review a ==2,000 line== PR today?",
		Keyword:   "Small PRs",
		BoxTitle:  "What changed for us",
		Points:    []string{"PRs under 400 lines", "Review within 4 hours", "Automated style checks", "Pair on tricky changes"},
		Notes:     []string{"Median review time dropped from 3 days to 5 hours", "Don't nitpick style a linter can catch"},
		Footer:    "Optimise for ==fast feedback==, not perfect code. Merged beats perfect.",
		Question:  "What's the one review rule your team swears by?",
		Hashtags:  []string{"#CodeReview", "#Productivity", "#SoftwareEngineering"},
	},
	{ // Minimal fallback-style card: headline, items and a lesson only.
		Headline: "🚀 **Kubernetes** isn’t your problem — your YAML is",
		Points:   []string{"1. Too many knobs 🤯", "2) Copy-pasted manifests", "- No resource limits"},
		Footer:   "Template it once, reuse it everywhere.",
	},
	{Headline: "One idea worth sharing", Icon: "bulb", Footer: "Ship small, learn fast", Question: "Agree?"},
}

func TestRenderAllStyles(t *testing.T) {
	for _, style := range Styles {
		for i, c := range sampleCards {
			c.Style, c.Seed = style, int64(i+1)
			data, err := Render(c)
			if err != nil {
				t.Fatalf("%s/%d: render: %v", style, i, err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s/%d: decode: %v", style, i, err)
			}
			if b := img.Bounds(); b.Dx() != Width || b.Dy() != Height {
				t.Fatalf("%s/%d: size %dx%d", style, i, b.Dx(), b.Dy())
			}
			if len(data) > 5<<20 {
				t.Errorf("%s/%d: image too large for LinkedIn comfort: %d bytes", style, i, len(data))
			}
		}
	}
}

func TestEveryIconRenders(t *testing.T) {
	for _, icon := range append(IconNames, "bubble", "unknown-icon") {
		c := sampleCards[0]
		c.Icon, c.Style, c.Seed = icon, StylePaper, 3
		if _, err := Render(c); err != nil {
			t.Fatalf("icon %s: %v", icon, err)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	c := sampleCards[0]
	c.Style, c.Seed = StyleChalkboard, 42
	a, err := Render(c)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Render(c)
	if !bytes.Equal(a, b) {
		t.Fatal("same card rendered differently; preview and published image would differ")
	}
}

func TestRenderUnknownStyleFallsBack(t *testing.T) {
	if _, err := Render(models.NoteCard{Headline: "x", Style: "sticky"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(models.NoteCard{}); err != nil {
		t.Fatal(err)
	}
}

func TestRenderHugeContentStillRenders(t *testing.T) {
	long := strings.Repeat("supercalifragilistic ", 20)
	c := models.NoteCard{
		Headline: long, Subtitle: long, Intro: long, IconLabel: long, Callout: long, Keyword: long,
		BoxTitle: long, Points: []string{long, long, long, long, long, long}, Notes: []string{long, long, long},
		Footer: long, Question: long, Hashtags: []string{"#" + strings.Repeat("x", 60)}, Style: StylePaper, Seed: 7,
	}
	if _, err := Render(c); err != nil {
		t.Fatal(err)
	}
}

func TestFitKeepsFullCardAtReadableScale(t *testing.T) {
	if err := loadFonts(); err != nil {
		t.Fatal(err)
	}
	for _, style := range Styles {
		th := themeFor(style)
		c := sampleCards[0]
		sk := buildSketch(c, th)
		area := rect{X: 157, Y: 132, W: 886, H: 1242} // paper content area
		if style == StyleWhiteboard || style == StyleChalkboard {
			area = rect{X: 100, Y: 96, W: 1000, H: 1286}
		}
		L := fitSketch(th, sk, area)
		if L.scale < 0.9 {
			t.Errorf("%s: full card shrank to scale %.2f; text would be too small", style, L.scale)
		}
		if len(L.sk.notes) != 2 || L.sk.intro == "" || L.sk.callout == "" {
			t.Errorf("%s: content was dropped from a normal-size card", style)
		}
	}
}

func TestParseRich(t *testing.T) {
	ws := parseRich("See ==your AI agent== now ==[[The Lesson:]]== ok")
	var got []string
	for _, w := range ws {
		got = append(got, fmt.Sprintf("%s:%v:%v", w.s, w.hl, w.strong))
	}
	want := "See:false:false your:true:false AI:true:false agent:true:false now:false:false The:true:true Lesson::true:true ok:false:false"
	if strings.Join(got, " ") != want {
		t.Errorf("parseRich =\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
	if plainRich("a ==b== [[c]]") != "a b c" {
		t.Error("plainRich should strip markers")
	}
}

func TestLimitLenKeepsMarkersBalanced(t *testing.T) {
	s := limitLen("one two ==three four five six seven eight== nine", 24)
	if strings.Count(s, "==")%2 != 0 {
		t.Errorf("unbalanced highlight markers: %q", s)
	}
}

func TestCleanText(t *testing.T) {
	if err := loadFonts(); err != nil {
		t.Fatal(err)
	}
	f := fonts[fontPatrick]
	cases := map[string]string{
		"🚀 Ship it":             "Ship it",
		"It’s “fine” — really…": `It's "fine" - really...`,
		"𝗕𝗼𝗹𝗱 𝟭𝟬":               "Bold 10",
		"**Markdown** bold":     "Markdown bold",
		"  spaced\n\tout  ":     "spaced out",
	}
	for in, want := range cases {
		if got := cleanText(in, f); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripListMarker(t *testing.T) {
	for in, want := range map[string]string{
		"1. First": "First", "12) Twelve": "Twelve", "- dash": "dash", "* star": "star", "Plain": "Plain", "2026 roadmap": "2026 roadmap",
	} {
		if got := stripListMarker(in); got != want {
			t.Errorf("stripListMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmallIconFor(t *testing.T) {
	for in, want := range map[string]string{
		"AWS keys": "lock", "DB logins": "lock", "Postgres tables": "database", "A decision": "check",
		"Review within 4 hours": "clock", "Deploy pipeline": "server",
	} {
		if got := smallIconFor(in, "check"); got != want {
			t.Errorf("smallIconFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRandomStyleAvoids(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		if s := RandomStyle(r, StyleNotebook, StylePaper); s == StyleNotebook || s == StylePaper {
			t.Fatalf("picked avoided style %s", s)
		}
	}
	if s := RandomStyle(r, Styles...); s == "" {
		t.Fatal("must still pick when everything is avoided")
	}
}

// TestWriteSamples dumps every style to NOTECARD_SAMPLES_DIR for a visual check.
func TestWriteSamples(t *testing.T) {
	dir := os.Getenv("NOTECARD_SAMPLES_DIR")
	if dir == "" {
		t.Skip("set NOTECARD_SAMPLES_DIR to write sample images")
	}
	for _, style := range Styles {
		for i, c := range sampleCards {
			c.Style, c.Seed = style, int64(i+11)
			data, err := Render(c)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, fmt.Sprintf("%s-%d.png", style, i))
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s (%d KB)", p, len(data)/1024)
		}
	}
}
