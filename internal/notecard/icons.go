package notecard

import (
	"image/color"
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/fogleman/gg"
)

// IconNames lists the doodles the renderer can draw (offered to the LLM).
var IconNames = []string{
	"robot", "bulb", "rocket", "gear", "shield", "key", "lock", "database",
	"cloud", "server", "bug", "chart", "clock", "magnifier", "eye", "code",
	"users", "chip", "warning", "monitor", "target", "book", "coffee", "check",
}

// pen draws doodles: a fill + ink outline, then a faint offset second stroke
// so shapes look sketched by hand rather than vector-perfect.
type pen struct {
	dc    *gg.Context
	r     *rand.Rand
	ink   color.NRGBA
	width float64
	fills iconFills
}

type iconFills struct {
	a, b, c, warm, light color.NRGBA // pastel fills; light = "white" parts
}

func (p pen) shape(path func(dx, dy float64), fill *color.NRGBA) {
	path(0, 0)
	if fill != nil {
		p.dc.SetColor(*fill)
		p.dc.FillPreserve()
	}
	p.dc.SetColor(p.ink)
	p.dc.SetLineWidth(p.width)
	p.dc.Stroke()
	ox, oy := jit(p.r, p.width*0.5), jit(p.r, p.width*0.5)
	path(ox, oy)
	p.dc.SetColor(withAlpha(p.ink, 0.3))
	p.dc.SetLineWidth(p.width * 0.55)
	p.dc.Stroke()
}

func (p pen) line(x1, y1, x2, y2 float64) {
	p.dc.SetColor(p.ink)
	p.dc.SetLineWidth(p.width)
	sketchLine(p.dc, p.r, x1, y1, x2, y2, p.width*0.25)
}

func (p pen) dot(x, y, rad float64, c color.NRGBA) {
	p.dc.SetColor(c)
	p.dc.DrawCircle(x, y, rad)
	p.dc.Fill()
}

func (p pen) circle(cx, cy, rad float64, fill *color.NRGBA) {
	p.shape(func(dx, dy float64) {
		p.dc.NewSubPath()
		p.dc.DrawCircle(cx+dx, cy+dy, rad)
	}, fill)
}

func (p pen) rrect(x, y, w, h, rad float64, fill *color.NRGBA) {
	p.shape(func(dx, dy float64) {
		p.dc.NewSubPath()
		p.dc.DrawRoundedRectangle(x+dx, y+dy, w, h, rad)
	}, fill)
}

func (p pen) poly(pts [][2]float64, fill *color.NRGBA) {
	p.shape(func(dx, dy float64) {
		p.dc.NewSubPath()
		for i, q := range pts {
			if i == 0 {
				p.dc.MoveTo(q[0]+dx, q[1]+dy)
			} else {
				p.dc.LineTo(q[0]+dx, q[1]+dy)
			}
		}
		p.dc.ClosePath()
	}, fill)
}

// drawIcon draws the named doodle centred at (cx, cy) within an s×s box.
// Unknown names fall back to a light bulb.
func drawIcon(p pen, name string, cx, cy, s float64) {
	f := p.fills
	switch name {
	case "robot":
		p.line(cx, cy-s*0.36, cx, cy-s*0.46)
		p.circle(cx, cy-s*0.47, s*0.035, &f.warm)
		p.rrect(cx-s*0.3, cy-s*0.27, s*0.06, s*0.12, s*0.02, &f.b)
		p.rrect(cx+s*0.24, cy-s*0.27, s*0.06, s*0.12, s*0.02, &f.b)
		p.rrect(cx-s*0.25, cy-s*0.37, s*0.5, s*0.32, s*0.08, &f.light)
		p.dot(cx-s*0.1, cy-s*0.23, s*0.045, p.ink)
		p.dot(cx+s*0.1, cy-s*0.23, s*0.045, p.ink)
		p.dot(cx-s*0.09, cy-s*0.245, s*0.014, f.light)
		p.dot(cx+s*0.11, cy-s*0.245, s*0.014, f.light)
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(p.width * 0.8)
		p.dc.DrawArc(cx, cy-s*0.17, s*0.06, 0.2*math.Pi, 0.8*math.Pi)
		p.dc.Stroke()
		p.rrect(cx-s*0.05, cy-s*0.05, s*0.1, s*0.05, s*0.01, &f.b)
		p.rrect(cx-s*0.2, cy, s*0.4, s*0.3, s*0.07, &f.light)
		p.rrect(cx-s*0.1, cy+s*0.06, s*0.2, s*0.12, s*0.03, &f.a)
		p.dot(cx-s*0.04, cy+s*0.12, s*0.018, f.warm)
		p.dot(cx+s*0.04, cy+s*0.12, s*0.018, p.ink)
		p.line(cx-s*0.2, cy+s*0.06, cx-s*0.36, cy-s*0.06) // waving arm
		p.circle(cx-s*0.39, cy-s*0.09, s*0.045, &f.light)
		p.line(cx+s*0.2, cy+s*0.06, cx+s*0.35, cy+s*0.18)
		p.circle(cx+s*0.37, cy+s*0.2, s*0.045, &f.light)
		p.rrect(cx-s*0.14, cy+s*0.3, s*0.08, s*0.14, s*0.02, &f.b)
		p.rrect(cx+s*0.06, cy+s*0.3, s*0.08, s*0.14, s*0.02, &f.b)
	case "rocket":
		p.dc.Push()
		p.dc.RotateAbout(gg.Radians(35), cx, cy)
		p.poly([][2]float64{{cx - s*0.15, cy + s*0.2}, {cx - s*0.27, cy + s*0.34}, {cx - s*0.14, cy + s*0.34}}, &f.warm)
		p.poly([][2]float64{{cx + s*0.15, cy + s*0.2}, {cx + s*0.27, cy + s*0.34}, {cx + s*0.14, cy + s*0.34}}, &f.warm)
		flame := rgba(255, 170, 60, 0.9)
		p.poly([][2]float64{{cx - s*0.09, cy + s*0.3}, {cx, cy + s*0.5}, {cx + s*0.09, cy + s*0.3}}, &flame)
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.MoveTo(cx+dx, cy-s*0.48+dy)
			p.dc.QuadraticTo(cx+s*0.22+dx, cy-s*0.2+dy, cx+s*0.15+dx, cy+s*0.32+dy)
			p.dc.LineTo(cx-s*0.15+dx, cy+s*0.32+dy)
			p.dc.QuadraticTo(cx-s*0.22+dx, cy-s*0.2+dy, cx+dx, cy-s*0.48+dy)
			p.dc.ClosePath()
		}, &f.light)
		p.circle(cx, cy-s*0.1, s*0.08, &f.a)
		p.dc.Pop()
		for i := 0; i < 3; i++ {
			x := cx - s*0.38 + float64(i)*s*0.1
			p.line(x, cy+s*0.36+float64(i)*s*0.03, x-s*0.08, cy+s*0.46+float64(i)*s*0.03)
		}
	case "gear":
		var pts [][2]float64
		for i := 0; i < 32; i++ {
			a := float64(i) * 2 * math.Pi / 32
			rad := s * 0.36
			if (i/2)%2 == 0 {
				rad = s * 0.44
			}
			pts = append(pts, [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
		}
		p.poly(pts, &f.b)
		p.circle(cx, cy, s*0.2, &f.light)
		p.circle(cx, cy, s*0.08, &f.a)
	case "shield":
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.MoveTo(cx-s*0.32+dx, cy-s*0.32+dy)
			p.dc.QuadraticTo(cx+dx, cy-s*0.42+dy, cx+s*0.32+dx, cy-s*0.32+dy)
			p.dc.QuadraticTo(cx+s*0.34+dx, cy+s*0.18+dy, cx+dx, cy+s*0.44+dy)
			p.dc.QuadraticTo(cx-s*0.34+dx, cy+s*0.18+dy, cx-s*0.32+dx, cy-s*0.32+dy)
			p.dc.ClosePath()
		}, &f.a)
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(p.width * 1.6)
		checkMark(p.dc, p.r, cx, cy, s*0.32)
	case "key":
		p.circle(cx-s*0.22, cy, s*0.16, &f.warm)
		p.circle(cx-s*0.22, cy, s*0.05, &f.light)
		p.rrect(cx-s*0.07, cy-s*0.035, s*0.45, s*0.07, s*0.02, &f.warm)
		p.rrect(cx+s*0.22, cy+s*0.03, s*0.05, s*0.1, s*0.01, &f.warm)
		p.rrect(cx+s*0.31, cy+s*0.03, s*0.05, s*0.14, s*0.01, &f.warm)
	case "lock":
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.MoveTo(cx-s*0.17+dx, cy-s*0.06+dy)
			p.dc.LineTo(cx-s*0.17+dx, cy-s*0.18+dy)
			p.dc.DrawArc(cx+dx, cy-s*0.18+dy, s*0.17, math.Pi, 2*math.Pi)
			p.dc.LineTo(cx+s*0.17+dx, cy-s*0.06+dy)
		}, nil)
		p.rrect(cx-s*0.27, cy-s*0.08, s*0.54, s*0.44, s*0.06, &f.warm)
		p.dot(cx, cy+s*0.08, s*0.05, p.ink)
		p.line(cx, cy+s*0.1, cx, cy+s*0.22)
	case "database":
		top, bot, rx, ry := cy-s*0.3, cy+s*0.32, s*0.3, s*0.09
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.MoveTo(cx-rx+dx, top+dy)
			p.dc.LineTo(cx-rx+dx, bot+dy)
			p.dc.DrawEllipticalArc(cx+dx, bot+dy, rx, ry, math.Pi, 0)
			p.dc.LineTo(cx+rx+dx, top+dy)
		}, &f.b)
		for _, y := range []float64{cy - s*0.1, cy + s*0.11} {
			p.dc.SetColor(p.ink)
			p.dc.SetLineWidth(p.width * 0.8)
			p.dc.NewSubPath()
			p.dc.DrawEllipticalArc(cx, y, rx, ry, math.Pi, 0)
			p.dc.Stroke()
		}
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.DrawEllipse(cx+dx, top+dy, rx, ry)
		}, &f.light)
	case "cloud":
		circles := [][3]float64{{-0.22, 0.06, 0.17}, {0, -0.08, 0.24}, {0.22, 0.05, 0.18}, {0.08, 0.14, 0.15}, {-0.08, 0.14, 0.15}}
		p.poly(unionOutline(cx, cy, s, circles), &f.a)
	case "server":
		for i := 0; i < 3; i++ {
			y := cy - s*0.36 + float64(i)*s*0.25
			p.rrect(cx-s*0.32, y, s*0.64, s*0.2, s*0.04, &f.b)
			p.dot(cx-s*0.22, y+s*0.1, s*0.03, rgba(80, 200, 120, 1))
			p.dot(cx-s*0.13, y+s*0.1, s*0.03, f.warm)
			p.line(cx, y+s*0.1, cx+s*0.22, y+s*0.1)
		}
	case "bug":
		for _, sy := range []float64{-0.08, 0.06, 0.2} {
			p.line(cx-s*0.16, cy+s*sy, cx-s*0.34, cy+s*(sy-0.06))
			p.line(cx+s*0.16, cy+s*sy, cx+s*0.34, cy+s*(sy-0.06))
		}
		p.line(cx-s*0.05, cy-s*0.3, cx-s*0.14, cy-s*0.44)
		p.line(cx+s*0.05, cy-s*0.3, cx+s*0.14, cy-s*0.44)
		p.circle(cx, cy-s*0.24, s*0.1, &f.b)
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.DrawEllipse(cx+dx, cy+s*0.08+dy, s*0.19, s*0.26)
		}, &f.c)
		p.line(cx, cy-s*0.14, cx, cy+s*0.32)
		p.dot(cx-s*0.08, cy, s*0.03, p.ink)
		p.dot(cx+s*0.08, cy+s*0.12, s*0.03, p.ink)
	case "chart":
		p.line(cx-s*0.38, cy-s*0.4, cx-s*0.38, cy+s*0.36)
		p.line(cx-s*0.38, cy+s*0.36, cx+s*0.42, cy+s*0.36)
		for i, hgt := range []float64{0.22, 0.38, 0.3, 0.56} {
			fill := []color.NRGBA{f.a, f.c, f.warm, f.a}[i]
			x := cx - s*0.3 + float64(i)*s*0.18
			p.rrect(x, cy+s*0.36-s*hgt, s*0.12, s*hgt, s*0.01, &fill)
		}
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(p.width)
		p.dc.MoveTo(cx-s*0.3, cy-s*0.02)
		p.dc.LineTo(cx-s*0.1, cy-s*0.16)
		p.dc.LineTo(cx+s*0.06, cy-s*0.08)
		p.dc.LineTo(cx+s*0.34, cy-s*0.38)
		p.dc.Stroke()
		arrowHead(p.dc, cx+s*0.34, cy-s*0.38, -0.8, s*0.1)
	case "clock":
		p.circle(cx, cy, s*0.4, &f.light)
		for i := 0; i < 12; i++ {
			a := float64(i) * math.Pi / 6
			l := 0.05
			if i%3 == 0 {
				l = 0.09
			}
			p.line(cx+math.Cos(a)*s*(0.38-l), cy+math.Sin(a)*s*(0.38-l), cx+math.Cos(a)*s*0.38, cy+math.Sin(a)*s*0.38)
		}
		p.line(cx, cy, cx, cy-s*0.26)
		p.line(cx, cy, cx+s*0.18, cy+s*0.08)
		p.dot(cx, cy, s*0.03, f.warm)
	case "magnifier":
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(s * 0.09)
		p.dc.DrawLine(cx+s*0.14, cy+s*0.14, cx+s*0.4, cy+s*0.4)
		p.dc.Stroke()
		lens := withAlpha(f.a, 0.8)
		p.circle(cx-s*0.08, cy-s*0.08, s*0.28, &lens)
		p.dc.SetColor(f.light)
		p.dc.SetLineWidth(p.width)
		p.dc.DrawArc(cx-s*0.08, cy-s*0.08, s*0.19, 1.1*math.Pi, 1.5*math.Pi)
		p.dc.Stroke()
	case "eye":
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.MoveTo(cx-s*0.42+dx, cy+dy)
			p.dc.QuadraticTo(cx+dx, cy-s*0.38+dy, cx+s*0.42+dx, cy+dy)
			p.dc.QuadraticTo(cx+dx, cy+s*0.38+dy, cx-s*0.42+dx, cy+dy)
			p.dc.ClosePath()
		}, &f.light)
		p.circle(cx, cy, s*0.14, &f.a)
		p.dot(cx, cy, s*0.07, p.ink)
		p.dot(cx+s*0.03, cy-s*0.03, s*0.02, f.light)
		for _, sx := range []float64{-1, 1} { // scanner brackets
			for _, sy := range []float64{-1, 1} {
				x, y := cx+sx*s*0.48, cy+sy*s*0.34
				p.line(x, y, x-sx*s*0.12, y)
				p.line(x, y, x, y-sy*s*0.1)
			}
		}
	case "code":
		p.rrect(cx-s*0.42, cy-s*0.32, s*0.84, s*0.64, s*0.06, &f.light)
		p.line(cx-s*0.42, cy-s*0.18, cx+s*0.42, cy-s*0.18)
		for i, c := range []color.NRGBA{rgba(240, 100, 90, 1), f.warm, rgba(80, 200, 120, 1)} {
			p.dot(cx-s*0.33+float64(i)*s*0.08, cy-s*0.25, s*0.025, c)
		}
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(p.width * 1.3)
		p.dc.MoveTo(cx-s*0.12, cy-s*0.06)
		p.dc.LineTo(cx-s*0.26, cy+s*0.07)
		p.dc.LineTo(cx-s*0.12, cy+s*0.2)
		p.dc.Stroke()
		p.dc.MoveTo(cx+s*0.12, cy-s*0.06)
		p.dc.LineTo(cx+s*0.26, cy+s*0.07)
		p.dc.LineTo(cx+s*0.12, cy+s*0.2)
		p.dc.Stroke()
		p.line(cx+s*0.05, cy-s*0.08, cx-s*0.05, cy+s*0.22)
	case "users":
		for _, o := range [][2]float64{{0.16, -0.04}, {-0.12, 0}} {
			x, y := cx+s*o[0], cy+s*o[1]
			fill := f.a
			if o[0] < 0 {
				fill = f.c
			}
			p.shape(func(dx, dy float64) {
				p.dc.NewSubPath()
				p.dc.MoveTo(x-s*0.24+dx, y+s*0.38+dy)
				p.dc.QuadraticTo(x-s*0.24+dx, y+s*0.06+dy, x+dx, y+s*0.06+dy)
				p.dc.QuadraticTo(x+s*0.24+dx, y+s*0.06+dy, x+s*0.24+dx, y+s*0.38+dy)
				p.dc.ClosePath()
			}, &fill)
			p.circle(x, y-s*0.12, s*0.13, &f.light)
		}
	case "chip":
		for i := 0; i < 4; i++ {
			o := -s*0.18 + float64(i)*s*0.12
			p.line(cx+o, cy-s*0.42, cx+o, cy-s*0.3)
			p.line(cx+o, cy+s*0.3, cx+o, cy+s*0.42)
			p.line(cx-s*0.42, cy+o, cx-s*0.3, cy+o)
			p.line(cx+s*0.3, cy+o, cx+s*0.42, cy+o)
		}
		p.rrect(cx-s*0.3, cy-s*0.3, s*0.6, s*0.6, s*0.05, &f.b)
		p.rrect(cx-s*0.15, cy-s*0.15, s*0.3, s*0.3, s*0.03, &f.a)
	case "warning":
		p.poly([][2]float64{{cx, cy - s*0.4}, {cx + s*0.44, cy + s*0.36}, {cx - s*0.44, cy + s*0.36}}, &f.warm)
		p.line(cx, cy-s*0.12, cx, cy+s*0.12)
		p.dot(cx, cy+s*0.23, s*0.04, p.ink)
	case "monitor":
		p.rrect(cx-s*0.09, cy+s*0.2, s*0.18, s*0.14, s*0.02, &f.b)
		p.rrect(cx-s*0.24, cy+s*0.33, s*0.48, s*0.07, s*0.03, &f.b)
		p.rrect(cx-s*0.44, cy-s*0.36, s*0.88, s*0.58, s*0.05, &f.light)
		inner := withAlpha(f.a, 0.7)
		p.rrect(cx-s*0.37, cy-s*0.29, s*0.74, s*0.44, s*0.03, &inner)
	case "target":
		p.circle(cx, cy, s*0.4, &f.warm)
		p.circle(cx, cy, s*0.27, &f.light)
		p.circle(cx, cy, s*0.13, &f.warm)
		p.line(cx, cy, cx+s*0.42, cy-s*0.42)
		arrowHead(p.dc, cx, cy, math.Pi*0.75, s*0.12)
	case "book":
		for _, side := range []float64{-1, 1} {
			sd := side
			p.shape(func(dx, dy float64) {
				p.dc.NewSubPath()
				p.dc.MoveTo(cx+dx, cy-s*0.26+dy)
				p.dc.QuadraticTo(cx+sd*s*0.2+dx, cy-s*0.36+dy, cx+sd*s*0.44+dx, cy-s*0.3+dy)
				p.dc.LineTo(cx+sd*s*0.44+dx, cy+s*0.3+dy)
				p.dc.QuadraticTo(cx+sd*s*0.2+dx, cy+s*0.24+dy, cx+dx, cy+s*0.34+dy)
				p.dc.ClosePath()
			}, &f.light)
			for i := 0; i < 3; i++ {
				y := cy - s*0.14 + float64(i)*s*0.13
				p.line(cx+sd*s*0.08, y, cx+sd*s*0.34, y)
			}
		}
	case "coffee":
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.DrawArc(cx+s*0.26+dx, cy+s*0.06+dy, s*0.11, -math.Pi/2, math.Pi/2)
		}, nil)
		p.rrect(cx-s*0.3, cy-s*0.12, s*0.52, s*0.48, s*0.08, &f.warm)
		for i := 0; i < 3; i++ {
			x := cx - s*0.16 + float64(i)*s*0.12
			p.dc.SetColor(withAlpha(p.ink, 0.7))
			p.dc.SetLineWidth(p.width * 0.8)
			p.dc.MoveTo(x, cy-s*0.18)
			p.dc.CubicTo(x-s*0.06, cy-s*0.26, x+s*0.06, cy-s*0.32, x, cy-s*0.42)
			p.dc.Stroke()
		}
	case "check":
		fill := rgba(150, 220, 160, 0.9)
		p.circle(cx, cy, s*0.4, &fill)
		p.dc.SetColor(p.ink)
		p.dc.SetLineWidth(p.width * 1.6)
		checkMark(p.dc, p.r, cx, cy, s*0.42)
	case "bubble":
		p.shape(func(dx, dy float64) {
			p.dc.NewSubPath()
			p.dc.DrawRoundedRectangle(cx-s*0.44+dx, cy-s*0.34+dy, s*0.88, s*0.56, s*0.16)
			p.dc.NewSubPath()
			p.dc.MoveTo(cx-s*0.2+dx, cy+s*0.2+dy)
			p.dc.LineTo(cx-s*0.3+dx, cy+s*0.42+dy)
			p.dc.LineTo(cx-s*0.02+dx, cy+s*0.21+dy)
		}, &f.a)
		for i := -1; i <= 1; i++ {
			p.dot(cx+float64(i)*s*0.18, cy-s*0.06, s*0.045, p.ink)
		}
	default: // bulb
		p.rrect(cx-s*0.13, cy+s*0.2, s*0.26, s*0.16, s*0.04, &f.b)
		p.line(cx-s*0.13, cy+s*0.27, cx+s*0.13, cy+s*0.27)
		glow := rgba(255, 236, 120, 0.95)
		p.circle(cx, cy-s*0.08, s*0.29, &glow)
		p.dc.SetColor(withAlpha(p.ink, 0.8))
		p.dc.SetLineWidth(p.width * 0.8)
		p.dc.MoveTo(cx-s*0.08, cy+s*0.18)
		p.dc.LineTo(cx-s*0.06, cy-s*0.04)
		p.dc.LineTo(cx, cy+s*0.02)
		p.dc.LineTo(cx+s*0.06, cy-s*0.04)
		p.dc.LineTo(cx+s*0.08, cy+s*0.18)
		p.dc.Stroke()
		for _, a := range []float64{-2.6, -2.0, -1.57, -1.14, -0.54} {
			p.line(cx+math.Cos(a)*s*0.37, cy-s*0.08+math.Sin(a)*s*0.37, cx+math.Cos(a)*s*0.47, cy-s*0.08+math.Sin(a)*s*0.47)
		}
	}
}

// unionOutline traces the outer boundary of overlapping circles (relative
// centre/radius in units of s) — used for the puffy cloud.
func unionOutline(cx, cy, s float64, circles [][3]float64) [][2]float64 {
	type pt struct{ x, y, a float64 }
	var pts []pt
	for i, c := range circles {
		x0, y0, rad := cx+c[0]*s, cy+c[1]*s, c[2]*s
		for k := 0; k < 60; k++ {
			a := float64(k) * 2 * math.Pi / 60
			x, y := x0+rad*math.Cos(a), y0+rad*math.Sin(a)
			inside := false
			for j, o := range circles {
				if j != i && math.Hypot(x-(cx+o[0]*s), y-(cy+o[1]*s)) < o[2]*s-0.5 {
					inside = true
					break
				}
			}
			if !inside {
				pts = append(pts, pt{x, y, math.Atan2(y-cy, x-cx)})
			}
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].a < pts[j].a })
	out := make([][2]float64, len(pts))
	for i, q := range pts {
		out[i] = [2]float64{q.x, q.y}
	}
	return out
}

// arrowHead draws a two-stroke arrow head at (x, y) pointing along angle.
func arrowHead(dc *gg.Context, x, y, angle, size float64) {
	for _, d := range []float64{-0.5, 0.5} {
		a := angle + math.Pi + d
		dc.MoveTo(x, y)
		dc.LineTo(x+math.Cos(a)*size, y+math.Sin(a)*size)
	}
	dc.Stroke()
}

// curvedArrow draws a loose hand-drawn arrow from (x1,y1) to (x2,y2), bowed
// to one side by bend (a fraction of its length).
func curvedArrow(dc *gg.Context, r *rand.Rand, x1, y1, x2, y2, bend, head float64) {
	mx, my := (x1+x2)/2, (y1+y2)/2
	dx, dy := x2-x1, y2-y1
	qx, qy := mx-dy*bend, my+dx*bend
	dc.MoveTo(x1, y1)
	dc.QuadraticTo(qx+jit(r, 3), qy+jit(r, 3), x2, y2)
	dc.Stroke()
	arrowHead(dc, x2, y2, math.Atan2(y2-qy, x2-qx), head)
}

// mainIconTable maps whole words in a post to its main doodle (first match
// wins, so more specific subjects come first).
var mainIconTable = []struct {
	icon  string
	words []string
}{
	{"shield", []string{"security", "secure", "secret", "secrets", "credential", "credentials", "auth", "vulnerability", "vulnerabilities", "breach", "attack", "privacy"}},
	{"robot", []string{"ai", "llm", "llms", "agent", "agents", "gpt", "chatgpt", "claude", "gemini", "copilot", "model", "models", "ml"}},
	{"database", []string{"database", "databases", "sql", "postgres", "postgresql", "mysql", "mongodb", "query", "queries", "index", "indexes", "schema"}},
	{"cloud", []string{"cloud", "aws", "azure", "gcp", "serverless", "lambda"}},
	{"server", []string{"kubernetes", "k8s", "docker", "container", "containers", "deploy", "deployment", "infrastructure", "devops", "ci", "pipeline"}},
	{"bug", []string{"bug", "bugs", "debug", "debugging", "incident", "incidents", "outage", "error", "errors", "crash"}},
	{"rocket", []string{"performance", "latency", "fast", "faster", "speed", "scale", "scaling", "launch", "ship", "shipping", "startup"}},
	{"check", []string{"test", "tests", "testing", "tdd", "qa", "quality"}},
	{"users", []string{"team", "teams", "hiring", "interview", "interviews", "career", "leadership", "manager", "culture", "mentor", "junior", "senior"}},
	{"code", []string{"code", "review", "reviews", "refactor", "refactoring", "api", "apis", "typescript", "javascript", "python", "golang", "rust", "java"}},
	{"chart", []string{"metrics", "cost", "costs", "growth", "data", "analytics", "revenue"}},
	{"clock", []string{"time", "deadline", "deadlines", "productivity", "meetings", "focus"}},
	{"book", []string{"learn", "learning", "book", "books", "reading", "course", "beginner", "beginners"}},
}

// GuessIcon picks a main doodle for a post from its words (bulb if unsure).
func GuessIcon(text string) string {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		words[w] = true
	}
	for _, e := range mainIconTable {
		for _, w := range e.words {
			if words[w] {
				return e.icon
			}
		}
	}
	return "bulb"
}

// IsIcon reports whether name is a doodle the renderer can draw.
func IsIcon(name string) bool {
	for _, n := range IconNames {
		if n == name {
			return true
		}
	}
	return false
}

// smallIconTable maps whole words in a diagram item to a tiny doodle.
var smallIconTable = []struct {
	icon  string
	words []string
}{
	{"lock", []string{"key", "keys", "secret", "secrets", "token", "tokens", "password", "passwords", "credential", "credentials", "auth", "env", "ssh", "login", "logins", "permission", "permissions"}},
	{"database", []string{"database", "databases", "db", "sql", "data", "table", "tables", "query", "queries", "cache", "index"}},
	{"cloud", []string{"cloud", "aws", "gcp", "azure", "s3", "cdn"}},
	{"bug", []string{"bug", "bugs", "error", "errors", "crash", "crashes", "fail", "failure", "incident"}},
	{"users", []string{"team", "teams", "user", "users", "people", "customer", "customers", "dev", "devs", "developers"}},
	{"clock", []string{"time", "latency", "slow", "fast", "speed", "deadline", "minutes", "hours", "days"}},
	{"code", []string{"code", "api", "apis", "pr", "prs", "commit", "commits", "git", "function", "functions"}},
	{"chart", []string{"metric", "metrics", "cost", "costs", "growth", "scale", "revenue", "numbers"}},
	{"server", []string{"server", "servers", "deploy", "deploys", "ci", "cd", "pipeline", "cluster", "k8s", "kubernetes", "docker"}},
	{"check", []string{"test", "tests", "review", "reviews", "verify", "done", "checklist"}},
}

// smallIconFor picks a tiny doodle for a diagram item from its wording.
func smallIconFor(text, fallback string) string {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		words[w] = true
	}
	for _, e := range smallIconTable {
		for _, w := range e.words {
			if words[w] {
				return e.icon
			}
		}
	}
	return fallback
}
