package notecard

import (
	"math"
	"math/rand"
	"strings"

	"github.com/fogleman/gg"
)

// sketchLayout holds every positioned element of a sketchnote page.
type sketchLayout struct {
	sk    sketch
	area  rect
	scale float64

	title, subtitle, intro, iconLabel, callout textBlock
	keyword, boxTitle, lesson, question, tags  textBlock
	items, notes                               []textBlock

	leftX, leftW, rightX, rightW float64

	titleTop, subtitleTop, introTop, calloutTop float64
	iconCX, iconCY, iconS, labelX, labelTop     float64
	kwBox, panel                                rect
	boxTitleTop                                 float64
	itemPos                                     [][2]float64 // small-icon centre per item
	itemIconS                                   float64
	noteTop                                     []float64
	noteX                                       float64
	lessonTop, questionTop, tagsTop, bubbleS    float64
}

// Scale bounds: sparse cards grow (up to maxPageScale) so the page reads like
// a full sketchnote; dense ones shrink (down to minPageScale) before any
// content is dropped.
const (
	minPageScale = 0.6
	maxPageScale = 1.4
)

// fitSketch finds the largest scale at which the sketch fills the area
// without overflowing. Only if even the smallest scale overflows are optional
// parts dropped, least important first.
func fitSketch(th *theme, sk sketch, area rect) *sketchLayout {
	try := func(sk sketch) *sketchLayout {
		best, ok := layoutSketch(th, sk, area, minPageScale)
		if !ok {
			return nil
		}
		lo, hi := minPageScale, maxPageScale
		for i := 0; i < 7; i++ { // height shrinks with scale, so bisect
			mid := (lo + hi) / 2
			if L, ok := layoutSketch(th, sk, area, mid); ok {
				best, lo = L, mid
			} else {
				hi = mid
			}
		}
		return best
	}
	if L := try(sk); L != nil {
		return L
	}
	reducers := []func(*sketch){
		func(s *sketch) { s.notes = s.notes[:min(len(s.notes), 1)] },
		func(s *sketch) { s.callout = "" },
		func(s *sketch) { s.items = s.items[:min(len(s.items), 3)] },
		func(s *sketch) { s.notes = nil },
		func(s *sketch) { s.intro = "" },
		func(s *sketch) { s.subtitle = "" },
	}
	for _, reduce := range reducers {
		reduce(&sk)
		if L := try(sk); L != nil {
			return L
		}
	}
	L, _ := layoutSketch(th, sk, area, minPageScale)
	return L
}

// layoutSketch positions everything at the given scale. Each text block picks
// its own size within a range so it respects its line budget; ok reports
// whether the whole page fits the area's height.
func layoutSketch(th *theme, sk sketch, A rect, s float64) (*sketchLayout, bool) {
	L := &sketchLayout{sk: sk, area: A, scale: s}
	bf, bs, ts := th.bodyFont, th.bodyScale*s, th.titleScale*s
	block := func(fid fontID, maxSize, minSize float64, text string, maxW float64, maxLines int, lh float64, balanced bool) textBlock {
		b, _ := fitBlock(fid, maxSize, minSize, text, maxW, maxLines, lh, balanced)
		return b
	}

	L.title = block(th.titleFont, 92*ts, 62*ts, sk.title, A.W*0.94, 2, 1.06, true)
	L.subtitle = block(bf, 44*bs, 34*bs, sk.subtitle, A.W*0.94, 2, 1.18, true)

	hasRight := sk.keyword != "" || len(sk.items) > 0 || len(sk.notes) > 0
	gutter := A.W * 0.05
	L.leftX, L.leftW = A.X, A.W*0.47
	L.rightX, L.rightW = A.X+L.leftW+gutter, A.W-L.leftW-gutter
	if !hasRight {
		L.leftW = A.W
	}

	L.intro = block(bf, 36*bs, 30*bs, sk.intro, L.leftW, 6, 1.2, false)
	L.iconS = 180 * s
	if !hasRight {
		L.iconS = 230 * s
	}
	labelW := math.Max(L.leftW-L.iconS*1.25, 120*s)
	L.iconLabel = block(bf, 34*bs, 26*bs, sk.iconLabel, labelW, 3, 1.08, true)
	L.callout = block(bf, 38*bs, 30*bs, sk.callout, L.leftW, 3, 1.18, false)

	kwPad, kwIcon := 14*s, 46*s
	L.keyword = block(th.markerFont, 50*s, 34*s, sk.keyword, L.rightW-kwPad*2-kwIcon-12*s, 2, 1.12, true)
	panelPad := 16 * s
	L.boxTitle = block(bf, 31*bs, 25*bs, sk.boxTitle, L.rightW-panelPad*2, 2, 1.1, true)
	L.itemIconS = 40 * s
	itemGap := 10 * s
	cols := 2
	if len(sk.items) < 2 {
		cols = 1
	}
	wrapItems := func(cols int) ([]textBlock, bool) {
		colW := (L.rightW - panelPad*2 - float64(cols-1)*14*s) / float64(cols)
		var out []textBlock
		all := true
		for _, it := range sk.items {
			b, fits := fitBlock(bf, 31*bs, 25*bs, it, colW-L.itemIconS-itemGap, 2, 1.12, true)
			all = all && fits
			out = append(out, b)
		}
		return out, all
	}
	items, fits := wrapItems(cols)
	if !fits && cols == 2 {
		cols = 1
		items, _ = wrapItems(1)
	}
	L.items = items
	L.noteX = L.rightX + 46*s
	for _, n := range sk.notes {
		L.notes = append(L.notes, block(bf, 32*bs, 26*bs, n, L.rightW-46*s, 3, 1.16, false))
	}
	// Siblings share one size (the smallest any of them needed) so a list
	// doesn't read as mixed handwriting sizes.
	if size := smallestSize(L.notes); size > 0 {
		for i, n := range sk.notes {
			L.notes[i] = block(bf, size, size, n, L.rightW-46*s, 3, 1.16, false)
		}
	}
	if size := smallestSize(L.items); size > 0 {
		colW := (L.rightW - panelPad*2 - float64(cols-1)*14*s) / float64(cols)
		for i, it := range sk.items {
			L.items[i] = block(bf, size, size, it, colW-L.itemIconS-itemGap, 2, 1.12, true)
		}
	}

	lesson := ""
	if sk.lesson != "" {
		lesson = "==[[The Lesson:]]== " + sk.lesson
	}
	L.lesson = block(bf, 38*bs, 30*bs, lesson, A.W, 4, 1.2, false)
	L.bubbleS = 54 * s
	L.question = block(bf, 38*bs, 30*bs, sk.question, A.W-L.bubbleS-18*s, 2, 1.18, false)
	L.tags = block(bf, 33*bs, 26*bs, sk.tags, A.W, 2, 1.15, false)

	// Heights.
	gTitle, gSub := 16*s, 34*s
	leftH := L.intro.h
	if !L.intro.empty() {
		leftH += 26 * s
	}
	iconBlockH := math.Max(L.iconS, L.iconLabel.h)
	leftH += iconBlockH
	if !L.callout.empty() {
		leftH += 18*s + L.callout.h
	}
	rightH, kwH := 0.0, 0.0
	if !L.keyword.empty() {
		kwH = L.keyword.h + kwPad*2
		rightH += kwH + 24*s
	}
	var rowH []float64
	panelH := 0.0
	if len(L.items) > 0 {
		for i := 0; i < len(L.items); i += cols {
			h := 0.0
			for j := i; j < i+cols && j < len(L.items); j++ {
				h = math.Max(h, math.Max(L.items[j].h, L.itemIconS))
			}
			rowH = append(rowH, h+12*s)
		}
		panelH = panelPad + L.boxTitle.h + 14*s
		for _, h := range rowH {
			panelH += h
		}
		panelH += panelPad
		rightH += panelH + 24*s
	}
	for _, n := range L.notes {
		rightH += n.h + 16*s
	}
	middleH := math.Max(leftH, rightH)

	gMid, gLesson, gQ := 34*s, 20*s, 14*s
	qH := 0.0
	if !L.question.empty() {
		qH = math.Max(L.question.h, L.bubbleS) + gQ
	}
	lessonH := 0.0
	if !L.lesson.empty() {
		lessonH = L.lesson.h + gLesson
	}
	subH := gSub * 0.5
	if !L.subtitle.empty() {
		subH = L.subtitle.h + gSub
	}
	total := L.title.h + gTitle + subH + middleH + gMid + lessonH + qH + L.tags.h
	fitsPage := total <= A.H

	// Spread spare height into the gaps (capped), then centre the rest.
	extra := math.Max(0, A.H-total)
	eSub := math.Min(extra*0.22, 60*s)
	eMid := math.Min(extra*0.3, 80*s)
	eLesson := math.Min(extra*0.15, 40*s)
	eQ := math.Min(extra*0.1, 30*s)
	rest := extra - eSub - eMid - eLesson - eQ

	y := A.Y + rest*0.4
	L.titleTop = y
	y += L.title.h + gTitle
	L.subtitleTop = y
	y += subH + eSub

	midTop := y
	ly := midTop + math.Max(0, middleH-leftH)/2
	L.introTop = ly
	if !L.intro.empty() {
		ly += L.intro.h + 26*s
	}
	if L.iconLabel.empty() {
		L.iconCX = L.leftX + L.leftW/2
	} else {
		L.iconCX = L.leftX + L.iconS*0.55
		L.labelX = L.iconCX + L.iconS*0.62
		L.labelTop = ly + iconBlockH/2 - L.iconLabel.h/2
	}
	L.iconCY = ly + iconBlockH/2
	ly += iconBlockH
	if !L.callout.empty() {
		ly += 18 * s
		L.calloutTop = ly
	}

	ry := midTop + math.Max(0, middleH-rightH)/2
	if !L.keyword.empty() {
		w := math.Min(L.rightW, L.keyword.maxW+kwPad*2+kwIcon+12*s)
		L.kwBox = rect{X: L.rightX, Y: ry, W: w, H: kwH}
		ry += kwH + 24*s
	}
	if len(L.items) > 0 {
		L.panel = rect{X: L.rightX, Y: ry, W: L.rightW, H: panelH}
		L.boxTitleTop = ry + panelPad
		iy := L.boxTitleTop + L.boxTitle.h + 14*s
		colW := (L.rightW - panelPad*2 - float64(cols-1)*14*s) / float64(cols)
		for i := range L.items {
			row, col := i/cols, i%cols
			x := L.rightX + panelPad + float64(col)*(colW+14*s) + L.itemIconS/2
			L.itemPos = append(L.itemPos, [2]float64{x, iy + rowH[row]/2 - 6*s})
			if col == cols-1 || i == len(L.items)-1 {
				iy += rowH[row]
			}
		}
		ry += panelH + 24*s
	}
	for _, n := range L.notes {
		L.noteTop = append(L.noteTop, ry)
		ry += n.h + 16*s
	}

	y = midTop + middleH + gMid + eMid
	L.lessonTop = y
	y += lessonH + eLesson
	L.questionTop = y
	y += qH + eQ
	L.tagsTop = y
	return L, fitsPage
}

func smallestSize(bs []textBlock) float64 {
	size := 0.0
	for _, b := range bs {
		if !b.empty() && (size == 0 || b.size < size) {
			size = b.size
		}
	}
	return size
}

// negativeWords mark an annotation as a caution (drawn with a warning sign).
var negativeWords = []string{"not ", "don't", "never", "avoid", "risk", "mistake", "careful", "danger", "leak", "exposed", "vuln", "warning", "fail"}

func drawSketch(dc *gg.Context, r *rand.Rand, th *theme, pal palette, L *sketchLayout) {
	s := L.scale
	A := L.area
	sk := L.sk
	ink := inkStyle{ink: pal.ink, strong: pal.title, hl: pal.highlight, double: th.double}
	big := pen{dc: dc, r: r, ink: pal.ink, width: th.stroke * s, fills: pal.fills}
	small := pen{dc: dc, r: r, ink: pal.ink, width: th.stroke * 0.7 * s, fills: pal.fills}

	// Title, centred, with a double underline.
	t := ink
	t.ink, t.center = pal.title, true
	drawBlock(dc, r, L.title, A.X+A.W/2, L.titleTop, t)
	if n := len(L.title.lines); n > 0 {
		last := L.title.lines[n-1]
		y := L.title.baseline(L.titleTop, n-1) + L.title.size*0.17
		x1, x2 := A.X+A.W/2-last.w/2-10, A.X+A.W/2+last.w/2+10
		dc.SetColor(pal.title)
		dc.SetLineWidth(th.stroke * 1.2 * s)
		sketchLine(dc, r, x1, y, x2, y+jit(r, 3), 1.6)
		sketchLine(dc, r, x1+24*s, y+9*s, x2-30*s, y+9*s+jit(r, 3), 1.6)
	}

	sub := ink
	sub.center = true
	drawBlock(dc, r, L.subtitle, A.X+A.W/2, L.subtitleTop, sub)

	// Left column: note → doodle (+ label) → callout.
	drawBlock(dc, r, L.intro, L.leftX, L.introTop, ink)
	if !L.intro.empty() {
		dc.SetColor(withAlpha(pal.ink, 0.85))
		dc.SetLineWidth(th.stroke * 0.8 * s)
		x := L.leftX + math.Min(L.intro.maxW*0.35, L.leftW*0.3)
		curvedArrow(dc, r, x, L.introTop+L.intro.h+4*s, L.iconCX-L.iconS*0.2, L.iconCY-L.iconS*0.5, 0.25, 14*s)
	}
	drawIcon(big, sk.icon, L.iconCX, L.iconCY, L.iconS)
	if !L.iconLabel.empty() {
		drawBlock(dc, r, L.iconLabel, L.labelX, L.labelTop, ink)
		dc.SetColor(withAlpha(pal.ink, 0.8))
		dc.SetLineWidth(th.stroke * 0.7 * s)
		ly := L.labelTop + L.iconLabel.lh*0.55
		sketchLine(dc, r, L.labelX-8*s, ly, L.iconCX+L.iconS*0.44, ly+10*s, 1)
	}
	drawBlock(dc, r, L.callout, L.leftX, L.calloutTop, ink)

	// Right column: keyword box → diagram panel → annotations.
	if !L.keyword.empty() {
		b := L.kwBox
		dc.SetColor(pal.highlight)
		dc.DrawRectangle(b.X+4, b.Y+4, b.W-8, b.H-8)
		dc.Fill()
		dc.SetColor(pal.title)
		dc.SetLineWidth(th.stroke * 1.1 * s)
		sketchRect(dc, r, b, 1.4)
		sketchRect(dc, r, rect{X: b.X + 5*s, Y: b.Y + 5*s, W: b.W - 10*s, H: b.H - 10*s}, 1.2)
		kw := ink
		kw.ink = pal.title
		drawBlock(dc, r, L.keyword, b.X+14*s, b.Y+14*s, kw)
		drawIcon(small, sk.icon, b.X+b.W-14*s-23*s, b.Y+b.H/2, 46*s)
	}
	if len(L.items) > 0 {
		b := L.panel
		dc.SetColor(pal.ink)
		dc.SetLineWidth(th.stroke * 0.9 * s)
		sketchRect(dc, r, b, 1.4)
		bt := ink
		bt.center = true
		drawBlock(dc, r, L.boxTitle, b.X+b.W/2, L.boxTitleTop, bt)
		y := L.boxTitleTop + L.boxTitle.h + 5*s
		dc.SetColor(withAlpha(pal.ink, 0.6))
		dc.SetLineWidth(th.stroke * 0.6 * s)
		sketchLine(dc, r, b.X+14*s, y, b.X+b.W-14*s, y, 1)
		fallbackIcon := "check"
		if sk.icon == "lock" || sk.icon == "key" || sk.icon == "shield" {
			fallbackIcon = "lock"
		}
		for i, it := range L.items {
			p := L.itemPos[i]
			drawIcon(small, smallIconFor(sk.items[i], fallbackIcon), p[0], p[1], L.itemIconS)
			drawBlock(dc, r, it, p[0]+L.itemIconS/2+10*s, p[1]-it.h/2, ink)
		}
		// The doodle points across the gutter at the diagram, starting past
		// the label so the arrow never crosses text.
		sx, sy := L.iconCX+L.iconS*0.5, L.iconCY
		if !L.iconLabel.empty() {
			sx, sy = L.labelX+L.iconLabel.maxW+12*s, L.labelTop+L.iconLabel.h/2
		}
		ex, ey := b.X-10*s, b.Y+b.H*0.5
		if ex-sx > 40*s {
			dc.SetColor(pal.accent)
			dc.SetLineWidth(th.stroke * 1.1 * s)
			curvedArrow(dc, r, sx, sy, ex, ey, -0.18, 18*s)
		}
	}
	for i, n := range L.notes {
		top := L.noteTop[i]
		low := " " + strings.ToLower(sk.notes[i]) + " "
		warn := false
		for _, w := range negativeWords {
			if strings.Contains(low, w) {
				warn = true
				break
			}
		}
		cx, cy := L.rightX+18*s, top+n.size*0.5
		if warn {
			drawIcon(small, "warning", cx, cy, 36*s)
		} else {
			dc.SetColor(pal.accent)
			dc.SetLineWidth(th.stroke * 0.8 * s)
			starMark(dc, r, cx, cy, 30*s)
		}
		drawBlock(dc, r, n, L.noteX, top, ink)
		if i == 0 && len(L.items) > 0 {
			dc.SetColor(withAlpha(pal.ink, 0.75))
			dc.SetLineWidth(th.stroke * 0.7 * s)
			curvedArrow(dc, r, L.noteX+n.maxW*0.5, top-4*s, L.panel.X+L.panel.W*0.62, L.panel.Y+L.panel.H+6*s, 0.25, 12*s)
		}
	}

	// Lesson, question, hashtags across the full width.
	drawBlock(dc, r, L.lesson, A.X, L.lessonTop, ink)
	if !L.question.empty() {
		drawIcon(small, "bubble", A.X+L.bubbleS/2, L.questionTop+L.bubbleS*0.5, L.bubbleS)
		q := ink
		q.ink = pal.title
		top := L.questionTop + math.Max(0, L.bubbleS-L.question.h)/2
		drawBlock(dc, r, L.question, A.X+L.bubbleS+18*s, top, q)
	}
	tags := ink
	tags.ink = pal.tag
	drawBlock(dc, r, L.tags, A.X, L.tagsTop, tags)
}
