package notecard

import (
	"image"
	"image/color"
	"math"
	"math/rand"

	"github.com/fogleman/gg"
)

type palette struct {
	title, ink, accent, warn color.NRGBA
	highlight, tag           color.NRGBA
	fills                    iconFills
	board                    color.NRGBA // chalkboard grain target
}

// surface is what a theme's background hands to the layout: where text may
// go, an optional page tilt, the page itself (drawn inside the tilt) and desk
// props such as pens (drawn on top, untilted).
type surface struct {
	area  rect
	tilt  float64
	page  func(dc *gg.Context)
	props func(dc *gg.Context)
}

type theme struct {
	name                            string
	titleFont, bodyFont, markerFont fontID
	titleScale, bodyScale           float64 // fonts differ in size; normalise visually
	double                          bool    // marker-style double ink pass
	stroke                          float64 // pen width for doodles
	palette                         func(r *rand.Rand) palette
	background                      func(dc *gg.Context, r *rand.Rand, pal palette) surface
	finish                          func(dc *gg.Context, r *rand.Rand, pal palette)
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.Intn(len(xs))] }

func themeFor(style string) *theme {
	switch style {
	case StyleNotebook:
		return notebookTheme
	case StyleGrid:
		return gridTheme
	case StyleWhiteboard:
		return whiteboardTheme
	case StyleChalkboard:
		return chalkboardTheme
	default:
		return paperTheme
	}
}

var paperFills = iconFills{
	a:     rgba(186, 216, 248, 1),
	b:     rgba(214, 219, 228, 1),
	c:     rgba(190, 232, 190, 1),
	warm:  rgba(255, 210, 110, 1),
	light: rgba(253, 253, 251, 1),
}

func penPalette(r *rand.Rand, title color.NRGBA) palette {
	return palette{
		title:     title,
		ink:       rgba(30, 30, 36, 1),
		accent:    rgba(36, 86, 200, 1),
		warn:      rgba(214, 48, 49, 1),
		highlight: pick(r, []color.NRGBA{rgba(255, 232, 64, 0.62), rgba(255, 232, 64, 0.62), rgba(140, 236, 150, 0.5)}),
		tag:       rgba(34, 74, 196, 1),
		fills:     paperFills,
	}
}

// vignette darkens the edges slightly so flat colours read as a real surface.
func vignette(dc *gg.Context, c color.NRGBA) {
	g := gg.NewRadialGradient(Width/2, Height/2, Height*0.3, Width/2, Height/2, Height*0.8)
	g.AddColorStop(0, withAlpha(c, 0))
	g.AddColorStop(1, c)
	dc.SetFillStyle(g)
	dc.DrawRectangle(0, 0, Width, Height)
	dc.Fill()
}

// --- Paper on a desk (paper, notebook, grid) ---

type paperKind int

const (
	plainPaper paperKind = iota
	ruledPaper
	gridPaper
)

// deskAndPaper paints a desk, then returns a tilted sheet of paper and the
// pens/highlighter lying around it — like a photographed sketchnote.
func deskAndPaper(dc *gg.Context, r *rand.Rand, kind paperKind) surface {
	switch r.Intn(3) {
	case 0: // light desk
		dc.SetColor(rgba(232, 232, 228, 1))
		dc.Clear()
		vignette(dc, rgba(90, 90, 80, 0.22))
	case 1: // wood
		dc.SetColor(rgba(196, 156, 112, 1))
		dc.Clear()
		dc.SetLineWidth(2)
		for i := 0; i < 70; i++ {
			dc.SetColor(rgba(140, 98, 60, 0.18+r.Float64()*0.18))
			x := r.Float64() * Width
			amp, per := 2+r.Float64()*5, 250+r.Float64()*350
			dc.MoveTo(x, 0)
			for y := 0.0; y <= Height; y += 20 {
				dc.LineTo(x+amp*math.Sin(y/per*2*math.Pi), y)
			}
			dc.Stroke()
		}
		vignette(dc, rgba(60, 35, 10, 0.3))
	default: // dark slate desk
		dc.SetColor(rgba(46, 50, 58, 1))
		dc.Clear()
		vignette(dc, rgba(0, 0, 0, 0.4))
	}

	paper := rect{X: 95 + jit(r, 6), Y: 70 + jit(r, 6), W: 1010, H: 1360}
	sheet := rgba(253, 253, 250, 1)
	if kind == ruledPaper {
		sheet = rgba(252, 249, 238, 1)
	}
	area := rect{X: paper.X + 62, Y: paper.Y + 62, W: paper.W - 124, H: paper.H - 118}
	if kind == ruledPaper {
		area.X, area.W = paper.X+112, paper.W-112-58
	}

	page := func(dc *gg.Context) {
		for i := 16; i >= 1; i-- { // soft shadow
			o := float64(i)
			dc.SetColor(rgba(0, 0, 0, 0.018))
			dc.DrawRoundedRectangle(paper.X+o*0.5, paper.Y+o*1.2, paper.W+o*0.3, paper.H, 4)
			dc.Fill()
		}
		dc.SetColor(sheet)
		dc.DrawRectangle(paper.X, paper.Y, paper.W, paper.H)
		dc.Fill()
		switch kind {
		case ruledPaper:
			dc.SetColor(rgba(142, 178, 224, 0.55))
			dc.SetLineWidth(1.6)
			for y := paper.Y + 120; y < paper.Y+paper.H-20; y += 46 {
				dc.DrawLine(paper.X, y, paper.X+paper.W, y)
				dc.Stroke()
			}
			dc.SetColor(rgba(226, 120, 120, 0.75))
			dc.SetLineWidth(2.2)
			dc.DrawLine(paper.X+90, paper.Y, paper.X+90, paper.Y+paper.H)
			dc.Stroke()
			for _, f := range []float64{0.15, 0.5, 0.85} {
				dc.SetColor(rgba(70, 70, 70, 0.18))
				dc.DrawCircle(paper.X+38, paper.Y+paper.H*f, 17)
				dc.Fill()
			}
		case gridPaper:
			const cell = 30.0
			for x := paper.X + cell; x < paper.X+paper.W; x += cell {
				dc.SetColor(rgba(170, 196, 228, 0.42))
				dc.SetLineWidth(1)
				dc.DrawLine(x, paper.Y, x, paper.Y+paper.H)
				dc.Stroke()
			}
			for y := paper.Y + cell; y < paper.Y+paper.H; y += cell {
				dc.SetColor(rgba(170, 196, 228, 0.42))
				dc.SetLineWidth(1)
				dc.DrawLine(paper.X, y, paper.X+paper.W, y)
				dc.Stroke()
			}
		}
		// Faint light falloff across the sheet.
		g := gg.NewLinearGradient(paper.X, paper.Y, paper.X+paper.W, paper.Y+paper.H)
		g.AddColorStop(0, rgba(255, 255, 255, 0))
		g.AddColorStop(1, rgba(120, 110, 90, 0.07))
		dc.SetFillStyle(g)
		dc.DrawRectangle(paper.X, paper.Y, paper.W, paper.H)
		dc.Fill()
	}

	// Pens and a highlighter lying on the desk, overlapping the page edges.
	type prop struct {
		x, y, length, angle, d float64
		body, cap              color.NRGBA
		highlighter            bool
	}
	black, blue, gray := rgba(34, 34, 38, 1), rgba(30, 70, 170, 1), rgba(120, 124, 130, 1)
	yellow, yellowCap := rgba(248, 226, 60, 1), rgba(232, 204, 30, 1)
	props := []prop{
		{x: paper.X - 8 + jit(r, 6), y: 420 + jit(r, 120), length: 560, angle: gg.Radians(92 + jit(r, 4)), d: 26, body: pick(r, []color.NRGBA{black, blue}), cap: black},
		{x: paper.X - 52 + jit(r, 6), y: 760 + jit(r, 160), length: 520, angle: gg.Radians(86 + jit(r, 4)), d: 22, body: gray, cap: black},
		{x: paper.X + paper.W + 34 + jit(r, 6), y: 900 + jit(r, 160), length: 470, angle: gg.Radians(268 + jit(r, 5)), d: 46, body: yellow, cap: yellowCap, highlighter: true},
	}
	if r.Intn(2) == 0 {
		props = append(props, prop{x: paper.X + paper.W + 70 + jit(r, 6), y: 330 + jit(r, 100), length: 520, angle: gg.Radians(94 + jit(r, 4)), d: 30, body: black, cap: gray})
	}
	drawProps := func(dc *gg.Context) {
		for _, p := range props {
			drawPen(dc, p.x, p.y, p.length, p.angle, p.d, p.body, p.cap, p.highlighter)
		}
	}
	return surface{area: area, tilt: gg.Radians(jit(r, 0.7)), page: page, props: drawProps}
}

// drawPen draws a pen/marker (or a chunky highlighter) centred at (cx, cy),
// rotated by angle, with a cast shadow and simple cylindrical shading.
func drawPen(dc *gg.Context, cx, cy, length, angle, d float64, body, capColor color.NRGBA, highlighter bool) {
	x0 := cx - length/2
	dc.Push()
	dc.RotateAbout(angle, cx, cy)
	// Shadow.
	dc.SetColor(rgba(0, 0, 0, 0.22))
	dc.DrawRoundedRectangle(x0+10, cy-d/2+d*0.45, length*0.98, d, d/2)
	dc.Fill()
	// Tip.
	tipLen := length * 0.07
	if highlighter {
		dc.SetColor(rgba(250, 236, 120, 1))
		dc.MoveTo(x0+tipLen, cy-d*0.32)
		dc.LineTo(x0, cy-d*0.05)
		dc.LineTo(x0, cy+d*0.25)
		dc.LineTo(x0+tipLen, cy+d*0.32)
		dc.ClosePath()
		dc.Fill()
	} else {
		dc.SetColor(rgba(176, 180, 186, 1))
		dc.MoveTo(x0+tipLen, cy-d*0.4)
		dc.LineTo(x0+tipLen*0.15, cy-d*0.08)
		dc.LineTo(x0+tipLen*0.15, cy+d*0.08)
		dc.LineTo(x0+tipLen, cy+d*0.4)
		dc.ClosePath()
		dc.Fill()
		dc.SetColor(rgba(20, 20, 20, 1))
		dc.DrawCircle(x0+tipLen*0.12, cy, d*0.09)
		dc.Fill()
	}
	// Body with a highlight and a shade stripe for roundness.
	bodyX, bodyW := x0+tipLen, length*0.7
	dc.SetColor(body)
	dc.DrawRoundedRectangle(bodyX, cy-d/2, bodyW, d, d*0.2)
	dc.Fill()
	dc.SetColor(rgba(255, 255, 255, 0.32))
	dc.DrawRectangle(bodyX+4, cy-d*0.34, bodyW-8, d*0.16)
	dc.Fill()
	dc.SetColor(rgba(0, 0, 0, 0.16))
	dc.DrawRectangle(bodyX, cy+d*0.22, bodyW, d*0.26)
	dc.Fill()
	// Cap with a clip.
	capX := bodyX + bodyW - 4
	capW := length - (capX - x0)
	dc.SetColor(capColor)
	dc.DrawRoundedRectangle(capX, cy-d*0.56, capW, d*1.12, d*0.45)
	dc.Fill()
	dc.SetColor(rgba(255, 255, 255, 0.25))
	dc.DrawRectangle(capX+6, cy-d*0.4, capW-18, d*0.14)
	dc.Fill()
	if !highlighter {
		dc.SetColor(rgba(190, 194, 200, 1))
		dc.DrawRoundedRectangle(capX+capW*0.15, cy-d*0.72, capW*0.7, d*0.22, d*0.1)
		dc.Fill()
	}
	dc.Pop()
}

var paperTheme = &theme{
	name:      StylePaper,
	titleFont: fontKalamBold, bodyFont: fontPatrick, markerFont: fontMarker,
	titleScale: 0.96, bodyScale: 1.0,
	stroke:  3.4,
	palette: func(r *rand.Rand) palette { return penPalette(r, rgba(30, 64, 175, 1)) },
	background: func(dc *gg.Context, r *rand.Rand, pal palette) surface {
		return deskAndPaper(dc, r, plainPaper)
	},
}

var notebookTheme = &theme{
	name:      StyleNotebook,
	titleFont: fontKalamBold, bodyFont: fontKalam, markerFont: fontMarker,
	titleScale: 0.92, bodyScale: 0.86,
	stroke:  3.4,
	palette: func(r *rand.Rand) palette { return penPalette(r, rgba(24, 52, 140, 1)) },
	background: func(dc *gg.Context, r *rand.Rand, pal palette) surface {
		return deskAndPaper(dc, r, ruledPaper)
	},
}

var gridTheme = &theme{
	name:      StyleGrid,
	titleFont: fontKalamBold, bodyFont: fontArchitect, markerFont: fontMarker,
	titleScale: 0.94, bodyScale: 0.88,
	stroke: 3.2,
	palette: func(r *rand.Rand) palette {
		p := penPalette(r, rgba(24, 70, 160, 1))
		p.ink = rgba(48, 50, 56, 1)
		p.accent = pick(r, []color.NRGBA{rgba(236, 120, 30, 1), rgba(36, 86, 200, 1)})
		return p
	},
	background: func(dc *gg.Context, r *rand.Rand, pal palette) surface {
		return deskAndPaper(dc, r, gridPaper)
	},
}

// --- Whiteboard: aluminium frame, marker colours, marker tray ---

var whiteboardTheme = &theme{
	name:      StyleWhiteboard,
	titleFont: fontMarker, bodyFont: fontGochi, markerFont: fontMarker,
	titleScale: 0.8, bodyScale: 1.0,
	double: true,
	stroke: 3.8,
	palette: func(r *rand.Rand) palette {
		title := pick(r, []color.NRGBA{rgba(26, 82, 180, 1), rgba(200, 40, 40, 1), rgba(24, 120, 70, 1)})
		accent := rgba(26, 82, 180, 1)
		if title == accent {
			accent = rgba(200, 40, 40, 1)
		}
		return palette{
			title: title, ink: rgba(32, 34, 40, 1), accent: accent, warn: rgba(210, 45, 45, 1),
			highlight: rgba(255, 220, 60, 0.45), tag: rgba(26, 82, 180, 1), fills: paperFills,
		}
	},
	background: func(dc *gg.Context, r *rand.Rand, pal palette) surface {
		dc.SetColor(rgba(168, 174, 181, 1))
		dc.Clear()
		dc.SetColor(rgba(214, 218, 222, 1))
		dc.DrawRectangle(10, 10, Width-20, Height-20)
		dc.Fill()
		g := gg.NewLinearGradient(0, 0, Width, Height)
		g.AddColorStop(0, rgba(255, 255, 255, 1))
		g.AddColorStop(0.55, rgba(248, 249, 250, 1))
		g.AddColorStop(1, rgba(234, 237, 240, 1))
		dc.SetFillStyle(g)
		dc.DrawRectangle(28, 28, Width-56, Height-112)
		dc.Fill()
		dc.SetLineWidth(18)
		for i := 0; i < 7; i++ { // ghosts of half-erased marker
			dc.SetColor(rgba(120, 130, 145, 0.04))
			x, y := 80+r.Float64()*(Width-500), 80+r.Float64()*(Height-300)
			sketchLine(dc, r, x, y, math.Min(x+140+r.Float64()*280, Width-90), y+jit(r, 40), 6)
		}
		dc.SetColor(rgba(150, 156, 163, 1))
		dc.DrawRectangle(0, Height-84, Width, 84)
		dc.Fill()
		dc.SetColor(rgba(120, 126, 133, 1))
		dc.DrawRectangle(0, Height-84, Width, 8)
		dc.Fill()
		for i, c := range []color.NRGBA{pal.title, pal.accent, pal.ink} {
			x := 640 + float64(i)*170 + jit(r, 15)
			dc.SetColor(rgba(245, 245, 245, 1))
			dc.DrawRoundedRectangle(x, Height-62, 130, 28, 10)
			dc.Fill()
			dc.SetColor(c)
			dc.DrawRoundedRectangle(x+96, Height-64, 48, 32, 8)
			dc.Fill()
		}
		return surface{area: rect{X: 92, Y: 88, W: Width - 184, H: Height - 88 - 128}}
	},
}

// --- Chalkboard: wooden frame, slate board, chalk dust, grainy chalk text ---

var chalkboardTheme = &theme{
	name:      StyleChalkboard,
	titleFont: fontCaveatBrush, bodyFont: fontPatrick, markerFont: fontCaveatBrush,
	titleScale: 1.06, bodyScale: 1.0,
	stroke: 3.4,
	palette: func(r *rand.Rand) palette {
		board := pick(r, []color.NRGBA{rgba(38, 62, 50, 1), rgba(40, 44, 48, 1)})
		return palette{
			title:     rgba(250, 232, 140, 1),
			ink:       rgba(244, 243, 236, 1),
			accent:    rgba(150, 210, 245, 1),
			warn:      rgba(246, 166, 196, 1),
			highlight: rgba(255, 236, 140, 0.2),
			tag:       rgba(150, 210, 245, 1),
			fills: iconFills{
				a: rgba(150, 210, 245, 0.35), b: rgba(255, 255, 255, 0.16), c: rgba(160, 230, 170, 0.35),
				warm: rgba(250, 220, 120, 0.4), light: rgba(255, 255, 255, 0.1),
			},
			board: board,
		}
	},
	background: func(dc *gg.Context, r *rand.Rand, pal palette) surface {
		dc.SetColor(rgba(122, 80, 46, 1))
		dc.Clear()
		dc.SetLineWidth(2)
		for i := 0; i < 90; i++ { // wood grain
			dc.SetColor(rgba(80, 50, 25, 0.25+r.Float64()*0.2))
			y := r.Float64() * Height
			amp, per := 2+r.Float64()*4, 200+r.Float64()*300
			dc.MoveTo(0, y)
			for x := 0.0; x <= Width; x += 20 {
				dc.LineTo(x, y+amp*math.Sin(x/per*2*math.Pi))
			}
			dc.Stroke()
		}
		dc.SetColor(pal.board)
		dc.DrawRectangle(40, 40, Width-80, Height-80)
		dc.Fill()
		g := gg.NewRadialGradient(Width*0.45, Height*0.4, 50, Width/2, Height/2, Height*0.75)
		g.AddColorStop(0, rgba(255, 255, 255, 0.07))
		g.AddColorStop(1, rgba(0, 0, 0, 0.22))
		dc.SetFillStyle(g)
		dc.DrawRectangle(40, 40, Width-80, Height-80)
		dc.Fill()
		for i := 0; i < 40; i++ { // chalk dust
			dc.SetColor(rgba(255, 255, 255, 0.015+r.Float64()*0.025))
			dc.DrawEllipse(60+r.Float64()*(Width-120), 60+r.Float64()*(Height-120), 40+r.Float64()*160, 10+r.Float64()*50)
			dc.Fill()
		}
		dc.SetLineWidth(70)
		for i := 0; i < 4; i++ { // old eraser swipes
			dc.SetColor(rgba(255, 255, 255, 0.03))
			x, y := 80+r.Float64()*(Width-500), 120+r.Float64()*(Height-300)
			sketchLine(dc, r, x, y, x+300+r.Float64()*300, y+jit(r, 80), 20)
		}
		dc.SetColor(rgba(100, 64, 36, 1))
		dc.DrawRectangle(40, Height-72, Width-80, 32)
		dc.Fill()
		dc.SetColor(rgba(245, 245, 240, 1))
		dc.DrawRoundedRectangle(820, Height-84, 120, 18, 8)
		dc.Fill()
		return surface{area: rect{X: 100, Y: 96, W: Width - 200, H: Height - 96 - 118}}
	},
	finish: chalkGrain,
}

// chalkGrain knocks random pixels of bright chalk strokes back toward the
// board colour, giving text and doodles the broken texture of real chalk.
func chalkGrain(dc *gg.Context, r *rand.Rand, pal palette) {
	img, ok := dc.Image().(*image.RGBA)
	if !ok {
		return
	}
	b := pal.board
	for i := 0; i < Width*Height/3; i++ {
		x, y := 40+r.Intn(Width-80), 40+r.Intn(Height-80)
		o := img.PixOffset(x, y)
		p := img.Pix[o : o+3 : o+3]
		if int(p[0])+int(p[1])+int(p[2]) < 330 {
			continue
		}
		t := 0.35 + r.Float64()*0.4
		p[0] = uint8(float64(p[0])*(1-t) + float64(b.R)*t)
		p[1] = uint8(float64(p[1])*(1-t) + float64(b.G)*t)
		p[2] = uint8(float64(p[2])*(1-t) + float64(b.B)*t)
	}
}
