package geometry

import (
	"math"
	"strconv"
	"strings"
)

// RGBA is a straight (not premultiplied) sRGB colour, channels in [0, 1].
type RGBA struct {
	R, G, B, A float64
}

// paint reads an SVG paint and its opacity; "none", absent or unparseable is
// transparent.
func paint(color string, opacity string) (c RGBA) {
	color = strings.TrimSpace(color)
	if len(color) != 7 || color[0] != '#' {
		return RGBA{}
	}
	v, err := strconv.ParseUint(color[1:], 16, 32)
	if err != nil {
		return RGBA{}
	}
	c = RGBA{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255, 1}
	if opacity != "" {
		c.A = num(opacity)
	}
	return c
}

// over composites src over an opaque dst.
func over(src RGBA, dst RGBA) RGBA {
	a := min(max(src.A, 0), 1)
	return RGBA{src.R*a + dst.R*(1-a), src.G*a + dst.G*(1-a), src.B*a + dst.B*(1-a), 1}
}

func linear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// Luminance is WCAG 2 relative luminance.
func (inst RGBA) Luminance() float64 {
	return 0.2126*linear(inst.R) + 0.7152*linear(inst.G) + 0.0722*linear(inst.B)
}

// Contrast is the WCAG 2 contrast ratio of two opaque colours, in [1, 21].
func Contrast(a, b RGBA) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Lab is CIE L*a*b* under D65.
type Lab struct{ L, A, B float64 }

func (inst RGBA) Lab() Lab {
	r, g, b := linear(inst.R), linear(inst.G), linear(inst.B)
	x := (0.4124564*r + 0.3575761*g + 0.1804375*b) / 0.95047
	y := 0.2126729*r + 0.7151522*g + 0.0721750*b
	z := (0.0193339*r + 0.1191920*g + 0.9503041*b) / 1.08883
	f := func(t float64) float64 {
		if t > 216.0/24389 {
			return math.Cbrt(t)
		}
		return (24389.0/27*t + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return Lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// Chroma is C*ab, how far from grey.
func (inst Lab) Chroma() float64 { return math.Hypot(inst.A, inst.B) }

// DeltaE2000 is the CIEDE2000 colour difference (Sharma, Wu, Dalal 2005).
func DeltaE2000(c1, c2 Lab) float64 {
	const kL, kC, kH = 1.0, 1.0, 1.0
	rad := math.Pi / 180
	cb := (math.Hypot(c1.A, c1.B) + math.Hypot(c2.A, c2.B)) / 2
	g := 0.5 * (1 - math.Sqrt(math.Pow(cb, 7)/(math.Pow(cb, 7)+math.Pow(25, 7))))
	a1, a2 := (1+g)*c1.A, (1+g)*c2.A
	cp1, cp2 := math.Hypot(a1, c1.B), math.Hypot(a2, c2.B)
	hp := func(b, a float64) float64 {
		if a == 0 && b == 0 {
			return 0
		}
		h := math.Atan2(b, a) / rad
		if h < 0 {
			h += 360
		}
		return h
	}
	h1, h2 := hp(c1.B, a1), hp(c2.B, a2)
	dL := c2.L - c1.L
	dC := cp2 - cp1
	var dh float64
	switch {
	case cp1*cp2 == 0:
		dh = 0
	case math.Abs(h2-h1) <= 180:
		dh = h2 - h1
	case h2-h1 > 180:
		dh = h2 - h1 - 360
	default:
		dh = h2 - h1 + 360
	}
	dH := 2 * math.Sqrt(cp1*cp2) * math.Sin(dh/2*rad)
	lb := (c1.L + c2.L) / 2
	cpb := (cp1 + cp2) / 2
	var hb float64
	switch {
	case cp1*cp2 == 0:
		hb = h1 + h2
	case math.Abs(h1-h2) <= 180:
		hb = (h1 + h2) / 2
	case h1+h2 < 360:
		hb = (h1 + h2 + 360) / 2
	default:
		hb = (h1 + h2 - 360) / 2
	}
	t := 1 - 0.17*math.Cos((hb-30)*rad) + 0.24*math.Cos(2*hb*rad) +
		0.32*math.Cos((3*hb+6)*rad) - 0.20*math.Cos((4*hb-63)*rad)
	dTheta := 30 * math.Exp(-math.Pow((hb-275)/25, 2))
	rc := 2 * math.Sqrt(math.Pow(cpb, 7)/(math.Pow(cpb, 7)+math.Pow(25, 7)))
	sl := 1 + 0.015*math.Pow(lb-50, 2)/math.Sqrt(20+math.Pow(lb-50, 2))
	sc := 1 + 0.045*cpb
	sh := 1 + 0.015*cpb*t
	rt := -math.Sin(2*dTheta*rad) * rc
	l, c, h := dL/(kL*sl), dC/(kC*sc), dH/(kH*sh)
	return math.Sqrt(l*l + c*c + h*h + rt*c*h)
}
