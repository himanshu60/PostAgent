// Package notecard renders handwritten sketchnote post images: a hand-drawn
// page (paper on a desk with pens, a notebook, graph paper, a whiteboard or a
// chalkboard) carrying the post's title, a highlighted subtitle, a first-person
// note, a doodle, a labelled diagram, annotations, the lesson, a closing
// question and hashtags — all in real handwriting fonts.
//
// Unlike AI image models (which garble text), every word here is legible and
// on-topic, it costs nothing, and it needs no network. Rendering is
// deterministic for a given card (style + seed), so the dashboard preview and
// the image published to LinkedIn are identical.
package notecard

import (
	"bytes"
	"embed"
	"fmt"
	"image/png"
	"math/rand"
	"strings"
	"sync"
	"unicode"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

// Width and Height are the output size: a 4:5 portrait, which takes the most
// vertical space LinkedIn gives an image in the mobile feed.
const (
	Width  = 1200
	Height = 1500
)

// Card styles.
const (
	StylePaper      = "paper"
	StyleNotebook   = "notebook"
	StyleGrid       = "grid"
	StyleWhiteboard = "whiteboard"
	StyleChalkboard = "chalkboard"
)

// Styles lists every available style.
var Styles = []string{StylePaper, StyleNotebook, StyleGrid, StyleWhiteboard, StyleChalkboard}

// styleWeight favours the paper-on-a-desk look, which reads most like a real
// photographed sketchnote; boards add occasional variety.
var styleWeight = map[string]int{StylePaper: 4, StyleNotebook: 2, StyleGrid: 2, StyleWhiteboard: 1, StyleChalkboard: 1}

// Content limits keep the page dense but scannable.
const (
	MaxPoints = 4 // diagram items
	MaxNotes  = 2 // annotations
)

//go:embed fonts/*.ttf
var fontFiles embed.FS

type fontID int

const (
	fontKalamBold fontID = iota
	fontKalam
	fontPatrick
	fontCaveatBrush
	fontGochi
	fontMarker
	fontArchitect
)

var fontPaths = map[fontID]string{
	fontKalamBold:   "fonts/Kalam-Bold.ttf",
	fontKalam:       "fonts/Kalam-Regular.ttf",
	fontPatrick:     "fonts/PatrickHand-Regular.ttf",
	fontCaveatBrush: "fonts/CaveatBrush-Regular.ttf",
	fontGochi:       "fonts/GochiHand-Regular.ttf",
	fontMarker:      "fonts/PermanentMarker-Regular.ttf",
	fontArchitect:   "fonts/ArchitectsDaughter-Regular.ttf",
}

var (
	fontsOnce sync.Once
	fonts     map[fontID]*opentype.Font
	fontsErr  error
)

// loadFonts parses the embedded fonts once. Parsed fonts are safe for
// concurrent use; faces (which hold scratch buffers) are created per render.
func loadFonts() error {
	fontsOnce.Do(func() {
		fonts = make(map[fontID]*opentype.Font, len(fontPaths))
		for id, p := range fontPaths {
			data, err := fontFiles.ReadFile(p)
			if err != nil {
				fontsErr = fmt.Errorf("read %s: %w", p, err)
				return
			}
			f, err := opentype.Parse(data)
			if err != nil {
				fontsErr = fmt.Errorf("parse %s: %w", p, err)
				return
			}
			fonts[id] = f
		}
	})
	return fontsErr
}

func newFace(id fontID, size float64) font.Face {
	face, err := opentype.NewFace(fonts[id], &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		// Only possible with invalid options, which are constant here.
		panic(fmt.Sprintf("notecard: new face: %v", err))
	}
	return face
}

func measure(face font.Face, s string) float64 {
	return float64(font.MeasureString(face, s)) / 64
}

// RandomStyle picks a style (weighted toward paper), avoiding any listed in
// avoid unless that would leave nothing to choose from.
func RandomStyle(r *rand.Rand, avoid ...string) string {
	skip := make(map[string]bool, len(avoid))
	for _, a := range avoid {
		skip[a] = true
	}
	var pool []string
	for _, s := range Styles {
		if !skip[s] {
			for i := 0; i < styleWeight[s]; i++ {
				pool = append(pool, s)
			}
		}
	}
	if len(pool) == 0 {
		return Styles[r.Intn(len(Styles))]
	}
	return pool[r.Intn(len(pool))]
}

// Render draws the card and returns PNG bytes.
func Render(card models.NoteCard) ([]byte, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	th := themeFor(card.Style)
	r := rand.New(rand.NewSource(card.Seed))
	pal := th.palette(r)
	sk := buildSketch(card, th)

	dc := gg.NewContext(Width, Height)
	dc.SetLineCapRound()
	dc.SetLineJoinRound()
	surface := th.background(dc, r, pal)
	lay := fitSketch(th, sk, surface.area)

	dc.Push()
	if surface.tilt != 0 {
		dc.RotateAbout(surface.tilt, Width/2, Height/2)
	}
	if surface.page != nil {
		surface.page(dc)
	}
	drawSketch(dc, r, th, pal, lay)
	dc.Pop()
	if surface.props != nil {
		surface.props(dc)
	}
	if th.finish != nil {
		th.finish(dc, r, pal)
	}

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// AltText describes the card for screen readers (LinkedIn image alt text).
func AltText(card models.NoteCard) string {
	parts := []string{plainRich(card.Headline)}
	for _, s := range []string{card.Subtitle, card.Intro} {
		if s = plainRich(s); s != "" {
			parts = append(parts, s)
		}
	}
	if len(card.Points) > 0 {
		parts = append(parts, plainRich(strings.Join(card.Points, ", ")))
	}
	for _, s := range []string{card.Footer, card.Question} {
		if s = plainRich(s); s != "" {
			parts = append(parts, s)
		}
	}
	return "Handwritten sketchnote: " + strings.Join(parts, " | ")
}

// --- Content ---

// sketch is the cleaned, length-limited content of a card.
type sketch struct {
	title, subtitle, intro, icon, iconLabel, callout string
	keyword, boxTitle, lesson, question              string
	items, notes                                     []string
	tags                                             string
}

func buildSketch(card models.NoteCard, th *theme) sketch {
	body := fonts[th.bodyFont]
	sk := sketch{
		title:     limitLen(cleanText(plainRich(card.Headline), fonts[th.titleFont]), 90),
		subtitle:  limitLen(cleanText(card.Subtitle, body), 110),
		intro:     limitLen(cleanText(card.Intro, body), 190),
		iconLabel: limitLen(cleanText(plainRich(card.IconLabel), body), 34),
		callout:   limitLen(cleanText(card.Callout, body), 80),
		keyword:   limitLen(strings.ToUpper(cleanText(plainRich(card.Keyword), fonts[th.markerFont])), 22),
		boxTitle:  limitLen(cleanText(plainRich(card.BoxTitle), body), 44),
		lesson:    limitLen(cleanText(card.Footer, body), 200),
		question:  limitLen(cleanText(card.Question, body), 110),
		icon:      strings.ToLower(strings.TrimSpace(card.Icon)),
	}
	if sk.title == "" {
		sk.title = "Notes"
	}
	if sk.icon == "" {
		sk.icon = "bulb"
	}
	for _, p := range card.Points {
		if len(sk.items) == MaxPoints {
			break
		}
		if p = limitLen(cleanText(plainRich(stripListMarker(p)), body), 34); p != "" {
			sk.items = append(sk.items, p)
		}
	}
	for _, n := range card.Notes {
		if len(sk.notes) == MaxNotes {
			break
		}
		if n = limitLen(cleanText(stripListMarker(n), body), 96); n != "" {
			sk.notes = append(sk.notes, n)
		}
	}
	var tags []string
	for _, t := range card.Hashtags {
		if t = cleanText(t, body); strings.HasPrefix(t, "#") && len(tags) < 5 {
			tags = append(tags, t)
		}
	}
	sk.tags = strings.Join(tags, " ")
	if len(sk.items) > 0 && sk.boxTitle == "" {
		sk.boxTitle = "Key points"
	}
	return sk
}

var punctReplacer = strings.NewReplacer(
	"’", "'", "‘", "'", "“", `"`, "”", `"`,
	"—", " - ", "–", "-", "…", "...", "•", "-",
	" ", " ", "→", "->", "**", "", "__", "",
)

// cleanText normalizes typography to plain ASCII-ish punctuation and drops any
// rune the font can't draw (emoji, symbols), so nothing renders as a box.
func cleanText(s string, f *opentype.Font) string {
	s = punctReplacer.Replace(Unbold(s))
	var b strings.Builder
	var buf sfnt.Buffer
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r):
		default:
			if gi, err := f.GlyphIndex(&buf, r); err == nil && gi != 0 {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	return strings.Trim(out, " -:;,")
}

// Unbold maps Unicode "mathematical sans-serif bold" letters/digits (used for
// bold headings in LinkedIn posts) back to plain ASCII.
func Unbold(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x1D5D4 && r <= 0x1D5ED:
			return 'A' + (r - 0x1D5D4)
		case r >= 0x1D5EE && r <= 0x1D607:
			return 'a' + (r - 0x1D5EE)
		case r >= 0x1D7EC && r <= 0x1D7F5:
			return '0' + (r - 0x1D7EC)
		}
		return r
	}, s)
}

// stripListMarker removes a leading "1.", "2)", "-", "*" or "#" so the card's
// own drawn bullets aren't doubled.
func stripListMarker(s string) string {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		s = s[i+1:]
	}
	return strings.TrimLeft(s, "-*#> \t")
}

// limitLen trims s to at most n bytes on a word boundary, never leaving a
// highlight marker half-open.
func limitLen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	cut = strings.TrimRight(cut, " ,;:-=[")
	if strings.Count(cut, "==")%2 == 1 {
		cut += "=="
	}
	if strings.Count(cut, "[[") > strings.Count(cut, "]]") {
		cut += "]]"
	}
	return cut + "..."
}
