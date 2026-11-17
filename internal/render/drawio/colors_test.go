package drawio

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

func subnetNode(id string, public bool) *model.Node {
	n := &model.Node{ID: id, Type: aws.TypeSubnet}
	if public {
		n.Tier = inventory.TierPublic
	}
	return n
}

func TestPaletteSubnet(t *testing.T) {
	p := newPalette(config.Colors{
		Public:    []string{"#111111", "#222222"},
		Resources: map[string]string{"subnet-pinned": "#999999"},
	})
	got := []string{
		p.subnet(subnetNode("pub-1", true)),
		p.subnet(subnetNode("pub-2", true)),
		p.subnet(subnetNode("pub-3", true)),         // wraps around
		p.subnet(subnetNode("priv-1", false)),       // default private list
		p.subnet(subnetNode("subnet-pinned", true)), // override, and does not advance the rotation
		p.subnet(subnetNode("pub-4", true)),
	}
	want := []string{"#111111", "#222222", "#111111", defaultColors[inventory.TierPrivate][0], "#999999", "#222222"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("colors = %v, want %v", got, want)
	}
	if p.container("pub-2") != "#222222" || p.container("subnet-pinned") != "#999999" || p.container("vpc-x") != "" {
		t.Errorf(
			"container colors wrong: %q %q %q",
			p.container("pub-2"),
			p.container("subnet-pinned"),
			p.container("vpc-x"),
		)
	}
}

func TestMix(t *testing.T) {
	// A 90% tint of the AWS public subnet green is the group's own fill.
	if got := mix("#7AA116", "#FFFFFF", 0.9); got != "#F2F6E8" {
		t.Errorf("tint = %s, want #F2F6E8", got)
	}
	if got := mix("#808080", "#000000", 0.5); got != "#404040" {
		t.Errorf("shade = %s, want #404040", got)
	}
}

// TestRenderColors checks that subnets, overridden VPCs and groups, and rule
// edges take their colors: an edge takes the color of the subnet it starts in.
func TestRenderColors(t *testing.T) {
	colors := config.Colors{
		Public:    []string{"#111111"},
		Private:   []string{"#222222"},
		Resources: map[string]string{"vpc-1": "#333333", "sg-db": "#444444"},
	}
	out, err := Render(context.Background(), "colors", testModel(), layout.ELK{}, colors)
	if err != nil {
		t.Fatal(err)
	}
	var p parsed
	if err := xml.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	style := map[string]string{}
	for _, o := range p.Objects {
		style[o.ID] = o.Cell.Style
	}
	for id, want := range map[string]string{
		"subnet-pub":           "strokeColor=#111111;",
		"subnet-priv":          "strokeColor=#222222;",
		"vpc-1":                "strokeColor=#333333;",
		"sg-db-in-subnet-priv": "strokeColor=#444444;",
		"sg-web-in-subnet-pub": "strokeColor=#DD3522;", // no override: the palette red
		// The web group's box sits in the public subnet, so its rule edge is
		// drawn in that subnet's color.
		"rule-sg-web-in-subnet-pub-sg-db-in-subnet-priv": "strokeColor=#111111;",
		// A prefix list sits in the region, which has no color.
		// The default rule color is too light for a line, so its shade is drawn.
		"rule-pl-1-sg-web-in-subnet-pub": "strokeColor=" + textShades[ruleColor] + ";",
	} {
		if !strings.Contains(style[id], want) {
			t.Errorf("%s style %q, want %s", id, style[id], want)
		}
	}
}

// defaultSwatches are the default colors: each tier's, and the rule color.
func defaultSwatches() map[string]string {
	out := map[string]string{"rule": ruleColor}
	for tier, colors := range defaultColors {
		for i, c := range colors {
			out[fmt.Sprintf("%s-%d", tier, i)] = c
		}
	}
	return out
}

// TestDefaultColorsReadable checks the default colors against the
// readability principle: lines reach 3:1 on the canvas, and labels 4.5:1 on
// the canvas and on both subnet tints.
func TestDefaultColorsReadable(t *testing.T) {
	for name, c := range defaultSwatches() {
		if got := contrast(lineColor(c), canvas); got < minLineContrast {
			t.Errorf("%s line %s: contrast %.2f, want at least %v", name, lineColor(c), got, minLineContrast)
		}
		for _, bg := range []string{canvas, subnetFill(c, false), subnetFill(c, true)} {
			if got := contrast(textOn(c, bg), bg); got < minTextContrast {
				t.Errorf("%s text on %s: contrast %.2f, want at least %v", name, bg, got, minTextContrast)
			}
		}
	}
}

// machado simulates full-severity color vision deficiencies in linear RGB
// (Machado, Oliveira, and Fernandes 2009).
var machado = map[string][3][3]float64{
	"normal": {{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
	"deuteranopia": {
		{0.367322, 0.860646, -0.227968}, {0.280085, 0.672501, 0.047413}, {-0.011820, 0.042940, 0.968881},
	},
	"protanopia": {
		{0.152286, 1.052583, -0.204868}, {0.114503, 0.786281, 0.099216}, {-0.003882, -0.048116, 1.051998},
	},
	"tritanopia": {
		{1.255528, -0.076749, -0.178779}, {-0.078411, 0.930809, 0.147602}, {0.004733, 0.691367, 0.303900},
	},
}

// seenLab returns a color in CIELAB as a reader with the deficiency m sees it.
func seenLab(color string, m [3][3]float64) [3]float64 {
	var lin, rgb [3]float64
	for i := range 3 {
		var v uint8
		_, _ = fmt.Sscanf(color[1+2*i:3+2*i], "%02X", &v)
		c := float64(v) / 255
		if c <= 0.04045 {
			lin[i] = c / 12.92
		} else {
			lin[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	for i := range 3 {
		rgb[i] = min(max(m[i][0]*lin[0]+m[i][1]*lin[1]+m[i][2]*lin[2], 0), 1)
	}
	x := 0.4124*rgb[0] + 0.3576*rgb[1] + 0.1805*rgb[2]
	y := 0.2126*rgb[0] + 0.7152*rgb[1] + 0.0722*rgb[2]
	z := 0.0193*rgb[0] + 0.1192*rgb[1] + 0.9505*rgb[2]
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116
	}
	fx, fy, fz := f(x/0.95047), f(y), f(z/1.08883)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// TestDefaultColorsDistinct checks that the default colors stay apart for
// readers with color vision deficiencies: every pair at least 15 apart in
// CIELAB after simulation.
func TestDefaultColorsDistinct(t *testing.T) {
	const minDistance = 15
	swatches := defaultSwatches()
	for vision, m := range machado {
		for a, ca := range swatches {
			for b, cb := range swatches {
				if a >= b {
					continue
				}
				la, lb := seenLab(ca, m), seenLab(cb, m)
				d := math.Sqrt((la[0]-lb[0])*(la[0]-lb[0]) + (la[1]-lb[1])*(la[1]-lb[1]) + (la[2]-lb[2])*(la[2]-lb[2]))
				if d < minDistance {
					t.Errorf(
						"%s: %s %s and %s %s are %.1f apart, want at least %d",
						vision,
						a,
						ca,
						b,
						cb,
						d,
						minDistance,
					)
				}
			}
		}
	}
}

func TestTextOn(t *testing.T) {
	// A configured color with no palette shade is darkened until readable.
	if got := textOn("#FFFF00", canvas); contrast(got, canvas) < minTextContrast {
		t.Errorf("textOn(yellow) = %s, contrast %.2f", got, contrast(got, canvas))
	}
	// A palette color takes its own shade, in either case.
	if got := textOn("#3478c8", canvas); got != "#215EA3" {
		t.Errorf("textOn(palette blue) = %s, want #215EA3", got)
	}
	// Even black cannot reach 4.5:1 on a dark background; it stops there.
	if got := textOn("#333333", "#111111"); got != "#000000" {
		t.Errorf("textOn(dark on dark) = %s, want #000000", got)
	}
	if lineColor("#3478C8") != "#3478C8" {
		t.Error("a dark enough color should draw its own line")
	}
}

func TestSubnetTints(t *testing.T) {
	light, strong := subnetStyle("#17A07A", false), subnetStyle("#17A07A", true)
	if light == strong {
		t.Error("neighboring subnets should differ in tint")
	}
	if !strings.Contains(light, "fillColor="+mix("#17A07A", "#FFFFFF", 0.9)+";") {
		t.Errorf("light subnet style %q", light)
	}
}

func TestFonts(t *testing.T) {
	if got := withText("html=1;"); !strings.Contains(got, "fontFamily=Roboto;") ||
		!strings.Contains(got, "fontSource=https%3A%2F%2Ffonts.googleapis.com") {
		t.Errorf("withText = %q, want Roboto and its source", got)
	}
	if got := withText("html=1;"); !strings.Contains(got, "fontColor="+textColor+";") {
		t.Errorf("withText = %q, want the default text color", got)
	}
	if got := withText("fontColor=#111111;"); strings.Contains(got, textColor) {
		t.Errorf("withText = %q, want the style's own color kept", got)
	}
	if got := contrast(textColor, canvas); got < minTextContrast {
		t.Errorf("default text color contrast %.2f, want at least %v", got, minTextContrast)
	}
	if got := withText("fontFamily=Roboto Mono;"); strings.Contains(got, "fontFamily=Roboto;") {
		t.Errorf("withText = %q, want the style's own font kept", got)
	}
	if got := leafCellLabel("web\n10.0.0.1"); got != "web<br>"+mono("10.0.0.1") {
		t.Errorf("leafCellLabel = %q", got)
	}
	if got := mono("a<b"); got != `<span style="font-family:Roboto Mono">a&lt;b</span>` {
		t.Errorf("mono = %q", got)
	}
}
