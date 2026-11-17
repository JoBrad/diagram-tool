package drawio

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/model"
)

// defaultColors are the tier colors when the config sets none: one hue per
// tier, from a palette tuned from Okabe-Ito so the tiers stay apart for
// readers with color vision deficiencies. Neighboring subnets of one tier
// differ by tint instead (subnetFill).
var defaultColors = map[inventory.Tier][]string{
	inventory.TierPublic:   {"#17A07A"},
	inventory.TierPrivate:  {"#3478C8"},
	inventory.TierIsolated: {"#CC74AE"},
}

// ruleColor is a rule edge's color when where it starts has no color of its
// own, such as a resource sitting in the region. It is the palette's flow
// color; a red here would read as risk.
const ruleColor = "#68BDEA"

// textShades are the palette's text shades of its colors, darker than the
// color itself so labels in it stay readable. Other colors are darkened by
// textOn.
var textShades = map[string]string{
	"#17A07A": "#0C7558",
	"#3478C8": "#215EA3",
	"#CC74AE": "#A04E85",
	"#68BDEA": "#2B6F98",
}

// textColor is the default text color, a dark charcoal, for labels that
// carry no color of their own.
const textColor = "#374151"

// Minimum contrast ratios: lines against the canvas, and text against what
// it sits on.
const (
	minLineContrast = 3.0
	minTextContrast = 4.5
	canvas          = "#FFFFFF"
)

// Subnet fills are a tint of the subnet's color. Neighbors alternate between
// a light and a stronger tint, so subnets of one tier stay apart.
var subnetTints = [2]float64{0.9, 0.8}

// palette hands out the diagram's colors: a color per subnet, rotated
// through its tier's list, and per-resource overrides from the config.
type palette struct {
	tiers     map[inventory.Tier][]string
	resources map[string]string
	next      map[inventory.Tier]int
	subnets   map[string]string // subnet ID -> its assigned color
}

func newPalette(c config.Colors) *palette {
	p := &palette{
		tiers:     map[inventory.Tier][]string{},
		resources: c.Resources,
		next:      map[inventory.Tier]int{},
		subnets:   map[string]string{},
	}
	for tier, colors := range map[inventory.Tier][]string{
		inventory.TierPublic: c.Public, inventory.TierPrivate: c.Private, inventory.TierIsolated: c.Isolated,
	} {
		if len(colors) == 0 {
			colors = defaultColors[tier]
		}
		p.tiers[tier] = colors
	}
	return p
}

// subnet assigns and returns a subnet's color: its override, else the next
// color of its tier. Subnets are assigned in drawing order, so neighbors in a
// row take different colors.
func (p *palette) subnet(n *model.Node) string {
	if c, ok := p.resources[n.ID]; ok {
		p.subnets[n.ID] = c
		return c
	}
	tier := subnetTier(n)
	colors := p.tiers[tier]
	c := colors[p.next[tier]%len(colors)]
	p.next[tier]++
	p.subnets[n.ID] = c
	return c
}

// container returns the color of a drawn container: a subnet's assigned
// color, or a VPC's override. It returns "" for a container with no color.
func (p *palette) container(id string) string {
	if c, ok := p.subnets[id]; ok {
		return c
	}
	return p.resources[id]
}

// subnetStyle draws a subnet in its color: a tint of it as fill, as the AWS
// subnet groups are drawn, and its label in a shade readable on that fill.
// alt picks the stronger tint, for every other neighbor.
func subnetStyle(color string, alt bool) string {
	fill := subnetFill(color, alt)
	return groupStyle("mxgraph.aws4.group_security_group", color, fill, textOn(color, fill), false, false)
}

// subnetFill is the tint a subnet in color is filled with.
func subnetFill(color string, alt bool) string {
	tint := subnetTints[0]
	if alt {
		tint = subnetTints[1]
	}
	return mix(color, "#FFFFFF", tint)
}

// ruleStyleFor is a rule edge drawn in color, with its ports in the mono
// font and a shade of color readable on the canvas.
func ruleStyleFor(color string) string {
	return edgeRouting + "html=1;rounded=0;dashed=1;strokeColor=" + lineColor(color) + ";endArrow=block;" +
		"fontColor=" + textOn(color, canvas) + ";fontSize=11;fontFamily=" + monoFont + ";"
}

// lineColor is color, or its text shade when color is too light to draw a
// line on the canvas.
func lineColor(color string) string {
	if contrast(color, canvas) >= minLineContrast {
		return color
	}
	return textOn(color, canvas)
}

// textOn returns a shade of color for text on bg: the palette's text shade,
// else color darkened by a third, darkened further until it is readable.
func textOn(color, bg string) string {
	text, ok := textShades[strings.ToUpper(color)]
	if !ok {
		text = mix(color, "#000000", 0.3)
	}
	shade := text
	for weight := 0.1; contrast(shade, bg) < minTextContrast && weight <= 1; weight += 0.1 {
		shade = mix(text, "#000000", min(weight, 1))
	}
	return shade
}

// contrast is the WCAG contrast ratio between two #RRGGBB colors, from 1 to 21.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// luminance is a #RRGGBB color's relative luminance, as WCAG defines it.
func luminance(color string) float64 {
	var l float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		v, _ := strconv.ParseUint(color[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		l += weight * c
	}
	return l
}

// mix blends color toward with by weight (0 keeps color, 1 gives with).
// Both are #RRGGBB, which config.Read checks.
func mix(color, with string, weight float64) string {
	out := "#"
	for i := 1; i < 7; i += 2 {
		a, _ := strconv.ParseUint(color[i:i+2], 16, 8)
		b, _ := strconv.ParseUint(with[i:i+2], 16, 8)
		out += fmt.Sprintf("%02X", int(float64(a)*(1-weight)+float64(b)*weight+0.5))
	}
	return out
}
