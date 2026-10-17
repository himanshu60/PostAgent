package notecard

import (
	"image/color"
	"math"
	"math/rand"
	"strings"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
)

// word is one rendered word: hl paints a highlighter stroke behind it, strong
// draws it in the accent ink (used for labels like "The Lesson:").
type word struct {
	s          string
	hl, strong bool
}

// parseRich splits text into words, treating ==phrase== as highlighted and
// [[phrase]] as strong. Unbalanced markers are simply dropped.
func parseRich(s string) []word {
	var out []word
	hl, strong := false, false
	for _, f := range strings.Fields(s) {
		w := word{}
		if strings.HasPrefix(f, "==") {
			hl, f = true, f[2:]
		}
		if strings.HasPrefix(f, "[[") {
			strong, f = true, f[2:]
		}
		w.hl, w.strong = hl, strong
		if i := strings.Index(f, "=="); i >= 0 {
			hl, f = false, f[:i]+f[i+2:]
		}
		if i := strings.Index(f, "]]"); i >= 0 {
			strong, f = false, f[:i]+f[i+2:]
		}
		f = strings.ReplaceAll(strings.ReplaceAll(f, "==", ""), "[[", "")
		if f != "" {
			w.s = f
			out = append(out, w)
		}
	}
	return out
}

// plainRich strips the markup, for alt text and length checks.
func plainRich(s string) string {
	r := strings.NewReplacer("==", "", "[[", "", "]]", "")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

type richLine struct {
	words []word
	w     float64
}

func lineWidth(face font.Face, ws []word) float64 {
	if len(ws) == 0 {
		return 0
	}
	w := measure(face, " ") * float64(len(ws)-1)
	for _, x := range ws {
		w += measure(face, x.s)
	}
	return w
}

// wrapRich greedily breaks words into lines no wider than maxW. ok is false if
// a single word can't fit (the caller then shrinks the text).
func wrapRich(face font.Face, ws []word, maxW float64) ([]richLine, bool) {
	var lines []richLine
	var cur []word
	ok := true
	for _, w := range ws {
		if measure(face, w.s) > maxW {
			ok = false
		}
		if len(cur) > 0 && lineWidth(face, append(cur[:len(cur):len(cur)], w)) > maxW {
			lines = append(lines, richLine{words: cur, w: lineWidth(face, cur)})
			cur = nil
		}
		cur = append(cur, w)
	}
	if len(cur) > 0 {
		lines = append(lines, richLine{words: cur, w: lineWidth(face, cur)})
	}
	return lines, ok
}

// wrapRichBalanced narrows the width as far as possible without adding a
// line, so no lone word dangles on the last line.
func wrapRichBalanced(face font.Face, ws []word, maxW float64) ([]richLine, bool) {
	lines, ok := wrapRich(face, ws, maxW)
	if !ok || len(lines) < 2 {
		return lines, ok
	}
	lo, hi := maxW*0.4, maxW
	for i := 0; i < 14; i++ {
		mid := (lo + hi) / 2
		if l, ok := wrapRich(face, ws, mid); ok && len(l) <= len(lines) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return wrapRich(face, ws, hi)
}

// textBlock is a wrapped paragraph in one face.
type textBlock struct {
	lines []richLine
	face  font.Face
	size  float64
	lh    float64
	h     float64
	maxW  float64 // widest line
}

func (b textBlock) empty() bool { return len(b.lines) == 0 }

// fitBlock picks the largest size in [minSize, maxSize] at which text wraps
// into at most maxLines lines without overflowing words. fits is false if
// even minSize needs more lines (the block is then returned at minSize).
func fitBlock(fid fontID, maxSize, minSize float64, text string, maxW float64, maxLines int, lhMul float64, balanced bool) (textBlock, bool) {
	if strings.TrimSpace(text) == "" {
		return textBlock{}, true
	}
	ws := parseRich(text)
	size := maxSize
	for {
		face := newFace(fid, size)
		lines, ok := wrapRich(face, ws, maxW)
		fits := ok && len(lines) <= maxLines
		if fits || size <= minSize {
			if balanced {
				lines, _ = wrapRichBalanced(face, ws, maxW)
			}
			b := textBlock{lines: lines, face: face, size: size, lh: size * lhMul}
			b.h = float64(len(lines)) * b.lh
			for _, l := range lines {
				b.maxW = math.Max(b.maxW, l.w)
			}
			return b, fits
		}
		size = math.Max(minSize, size*0.93)
	}
}

// baseline returns the baseline y of line i for a block whose top is at top.
func (b textBlock) baseline(top float64, i int) float64 {
	return top + float64(i)*b.lh + b.size*0.86
}

type inkStyle struct {
	ink, strong, hl color.NRGBA
	double          bool // faint second pass, like marker ink
	center          bool
}

// drawBlock writes a block with its top-left at (x, top) (or centred on x).
func drawBlock(dc *gg.Context, r *rand.Rand, b textBlock, x, top float64, st inkStyle) {
	if b.empty() {
		return
	}
	dc.SetFontFace(b.face)
	space := measure(b.face, " ")
	for i, l := range b.lines {
		lx := x
		if st.center {
			lx = x - l.w/2
		}
		y := b.baseline(top, i)
		lx += jit(r, 2.5)
		dc.Push()
		dc.RotateAbout(jit(r, 0.009), lx, y)

		// Highlighter runs first, under the ink.
		cx, runStart, runEnd := lx, -1.0, 0.0
		flush := func() {
			if runStart >= 0 {
				highlight(dc, r, runStart, y, runEnd-runStart, b.size, st.hl)
				runStart = -1
			}
		}
		for _, w := range l.words {
			ww := measure(b.face, w.s)
			if w.hl {
				if runStart < 0 {
					runStart = cx
				}
				runEnd = cx + ww
			} else {
				flush()
			}
			cx += ww + space
		}
		flush()

		cx = lx
		for _, w := range l.words {
			c := st.ink
			if w.strong {
				c = st.strong
			}
			if st.double {
				dc.SetColor(withAlpha(c, 0.3))
				dc.DrawString(w.s, cx+0.9, y+0.6)
			}
			dc.SetColor(c)
			dc.DrawString(w.s, cx, y+jit(r, 0.8))
			cx += measure(b.face, w.s) + space
		}
		dc.Pop()
	}
}
