package notecard

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/fogleman/gg"
)

type rect struct{ X, Y, W, H float64 }

func rgba(r, g, b uint8, a float64) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: uint8(math.Round(a * 255))}
}

func withAlpha(c color.Color, a float64) color.NRGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = uint8(math.Round(float64(n.A) * a))
	return n
}

// jit returns a uniform random value in [-amt, amt].
func jit(r *rand.Rand, amt float64) float64 { return (r.Float64()*2 - 1) * amt }

// sketchLine strokes a slightly bowed, wobbly line, like one drawn by hand.
func sketchLine(dc *gg.Context, r *rand.Rand, x1, y1, x2, y2, wobble float64) {
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(dx, dy)
	if length < 1 {
		return
	}
	nx, ny := -dy/length, dx/length
	bow := jit(r, wobble*1.6)
	n := int(math.Max(3, length/26))
	dc.MoveTo(x1+jit(r, wobble*0.5), y1+jit(r, wobble*0.5))
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		off := bow*math.Sin(math.Pi*t) + jit(r, wobble*0.35)
		dc.LineTo(x1+dx*t+nx*off, y1+dy*t+ny*off)
	}
	dc.Stroke()
}

// sketchCircle strokes an imperfect, not-quite-closed loop.
func sketchCircle(dc *gg.Context, r *rand.Rand, cx, cy, rad, wobble float64) {
	start := r.Float64() * 2 * math.Pi
	sweep := 2*math.Pi + 0.35 + r.Float64()*0.35
	phase := r.Float64() * 2 * math.Pi
	squash := 1 + jit(r, 0.08)
	const n = 44
	for i := 0; i <= n; i++ {
		t := float64(i) / n
		a := start + sweep*t
		rr := rad*(1+0.05*math.Sin(2*a+phase)) + rad*0.08*t + jit(r, wobble*0.25)
		x, y := cx+rr*math.Cos(a)*squash, cy+rr*math.Sin(a)/squash
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	dc.Stroke()
}

// sketchRect strokes a box whose corners overshoot a little.
func sketchRect(dc *gg.Context, r *rand.Rand, b rect, wobble float64) {
	o := wobble * 2.5
	sketchLine(dc, r, b.X-o, b.Y, b.X+b.W+o, b.Y+jit(r, 2), wobble)
	sketchLine(dc, r, b.X+b.W, b.Y-o, b.X+b.W+jit(r, 2), b.Y+b.H+o, wobble)
	sketchLine(dc, r, b.X+b.W+o, b.Y+b.H, b.X-o, b.Y+b.H+jit(r, 2), wobble)
	sketchLine(dc, r, b.X, b.Y+b.H+o, b.X+jit(r, 2), b.Y-o, wobble)
}

// wavyUnderline strokes a loose hand-drawn wave from x1 to x2.
func wavyUnderline(dc *gg.Context, r *rand.Rand, x1, x2, y, amp float64) {
	period := 60 + r.Float64()*30
	phase := r.Float64() * 2 * math.Pi
	dc.MoveTo(x1, y)
	for x := x1; x <= x2; x += 8 {
		dc.LineTo(x, y+amp*math.Sin((x-x1)/period*2*math.Pi+phase)+jit(r, 0.6))
	}
	dc.Stroke()
}

// highlight paints a translucent highlighter stroke behind a line of text.
func highlight(dc *gg.Context, r *rand.Rand, x, baseline, w, size float64, c color.Color) {
	top := baseline - size*0.52
	bot := baseline + size*0.12
	s := size * 0.12
	dc.MoveTo(x-14+jit(r, 4), top+jit(r, 3))
	dc.LineTo(x+w+14+jit(r, 4), top+jit(r, 4))
	dc.LineTo(x+w+10+s+jit(r, 4), bot+jit(r, 3))
	dc.LineTo(x-10-s+jit(r, 4), bot+jit(r, 3))
	dc.ClosePath()
	dc.SetColor(c)
	dc.Fill()
}

func checkMark(dc *gg.Context, r *rand.Rand, cx, cy, s float64) {
	dc.MoveTo(cx-s*0.5+jit(r, 1), cy+jit(r, 1))
	dc.LineTo(cx-s*0.12+jit(r, 1), cy+s*0.42+jit(r, 1))
	dc.LineTo(cx+s*0.62+jit(r, 1), cy-s*0.55+jit(r, 1))
	dc.Stroke()
}

func starMark(dc *gg.Context, r *rand.Rand, cx, cy, s float64) {
	rot := -math.Pi/2 + jit(r, 0.2)
	for i := 0; i <= 10; i++ {
		rad := s * 0.55
		if i%2 == 1 {
			rad = s * 0.23
		}
		a := rot + float64(i)*math.Pi/5
		x, y := cx+rad*math.Cos(a)+jit(r, 1), cy+rad*math.Sin(a)+jit(r, 1)
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	dc.Stroke()
}
