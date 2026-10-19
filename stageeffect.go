package background

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/imgutil"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/unity"
)

// Special background bundles carry a stage-effect prefab (the GameObject
// with InGameStageBgEffector) that the original instantiates on the stage
// root next to the bgImage widget: an NGUI hierarchy whose local
// coordinates are the bgImage widget's UI units (top edge at y = 550).
//
// Its Animator only plays StartAnimation / ClearAnimation around the intro
// and the clear; every state writes defaults and both return to the empty
// None state, and the intro-only tweens they trigger are reversed by the
// same clips. During play the prefab therefore shows its serialized state,
// which is what is rendered here: active widgets with their serialized
// alpha (AnimatedAlpha and tweens enabled at instantiation applied), panel
// alpha and clipping, depth cutouts and the shaders' blending. Looping
// sprite animations show their serialized frame; particle systems are not
// part of a static image.

// stageDraw is one widget of the stage effect to draw.
type stageDraw struct {
	name    string
	tex     *image.NRGBA
	corners [4][2]float64 // UI units after the stage root scale: bl, tl, tr, br
	z       float64
	uv      [4]float64 // x, y, w, h (bottom-left origin)
	color   [4]float64 // RGB tint and final alpha
	blend   string     // alpha | additive | cutout
	clip    *stageClip
	order   [3]int // render queue, panel depth, widget depth
	seq     int
}

type stageClip struct {
	// clip rectangle centre and half size in UI units, softness in UI units
	cx, cy, hw, hh, sx, sy float64
}

// shader names by blend mode
var stageShaders = map[string]string{
	"Unlit/Transparent Colored": "alpha",
	"Unlit/Depth Cutout":        "cutout",
	// Resources/unity_builtin_extra#10720 is Mobile/Particles/Additive.
	"builtin:10720": "additive",
}

type stageObject struct {
	classID int32
	v       map[string]any
}

// stageEffect returns the visible widgets of a bundle's stage-effect prefab
// (none for bundles without one).
func stageEffect(files map[string][]byte) ([]stageDraw, error) {
	var draws []stageDraw
	for _, raw := range files {
		f, err := unity.Parse(raw)
		if err != nil {
			continue // resource streams
		}
		d, err := renderStage(f, files)
		if err != nil {
			return nil, err
		}
		draws = append(draws, d...)
	}
	return draws, nil
}

func renderStage(f *unity.File, files map[string][]byte) ([]stageDraw, error) {
	objs := map[int64]stageObject{}
	for _, id := range f.Order {
		o := f.Objects[id]
		switch o.ClassID {
		case 1, 4, 21, 48, 114, 115, 198:
		default:
			continue
		}
		v, err := f.Read(id)
		if err != nil {
			return nil, err
		}
		objs[id] = stageObject{o.ClassID, v}
	}
	script := func(v map[string]any) string {
		if o, ok := objs[ptr(v["m_Script"])]; ok && o.classID == 115 {
			s, _ := o.v["m_ClassName"].(string)
			return s
		}
		return ""
	}
	// components by GameObject
	type node struct {
		name   string
		active bool
		tr     map[string]any
		comps  map[string]map[string]any // script class or "ParticleSystem"
	}
	nodes := map[int64]*node{} // by transform path ID
	var roots []int64
	for id, o := range objs {
		if o.classID != 4 {
			continue
		}
		g, ok := objs[ptr(o.v["m_GameObject"])]
		if !ok {
			return nil, fmt.Errorf("transform %d without GameObject", id)
		}
		n := &node{tr: o.v, comps: map[string]map[string]any{}}
		n.name, _ = g.v["m_Name"].(string)
		n.active = num(g.v["m_IsActive"]) != 0
		comps, _ := g.v["m_Component"].([]any)
		for _, c := range comps {
			cm, _ := c.(map[string]any)
			co, ok := objs[ptr(cm["component"])]
			if !ok {
				continue
			}
			switch co.classID {
			case 114:
				n.comps[script(co.v)] = co.v
			case 198:
				n.comps["ParticleSystem"] = co.v
			}
		}
		nodes[id] = n
		if ptr(o.v["m_Father"]) == 0 {
			if _, ok := n.comps["InGameStageBgEffector"]; ok {
				roots = append(roots, id)
			}
		}
	}
	if len(roots) == 0 {
		return nil, nil
	}
	if len(roots) > 1 {
		return nil, fmt.Errorf("%d stage-effect roots", len(roots))
	}
	shaderName := func(p any) (string, error) {
		pm, _ := p.(map[string]any)
		file := num(pm["m_FileID"])
		id := ptr(p)
		if file == 1 && len(f.Externals) > 0 && f.Externals[0] == "Resources/unity_builtin_extra" {
			return fmt.Sprintf("builtin:%d", id), nil
		}
		if file != 0 {
			return "", fmt.Errorf("shader in external file %v", file)
		}
		o, ok := objs[id]
		if !ok || o.classID != 48 {
			return "", fmt.Errorf("shader %d not in bundle", id)
		}
		if form, ok := o.v["m_ParsedForm"].(map[string]any); ok {
			if s, ok := form["m_Name"].(string); ok {
				return s, nil
			}
		}
		s, _ := o.v["m_Name"].(string)
		return s, nil
	}
	texCache := map[int64]*image.NRGBA{}
	texture := func(p any) (*image.NRGBA, error) {
		id := ptr(p)
		if t, ok := texCache[id]; ok {
			return t, nil
		}
		t, err := unity.DecodeTexture(f, files, id)
		if err != nil {
			return nil, err
		}
		texCache[id] = t
		return t, nil
	}

	type panelState struct {
		queue, depth int
		clip         *stageClip
	}
	outer := &panelState{queue: 3000} // the stage root's own panel
	var draws []stageDraw
	seq := 0
	// m is the 2D affine (a b c d e f: x' = a x + c y + e, y' = b x + d y + f)
	// from local to stage-root UI units; z accumulates the local z.
	// offPlane marks a subtree rotated out of the screen plane (only particle
	// systems use that; a visible widget there is an error).
	var walk func(id int64, m [6]float64, z, alpha float64, panel *panelState, offPlane bool) error
	walk = func(id int64, m [6]float64, z, alpha float64, panel *panelState, offPlane bool) error {
		n := nodes[id]
		if !n.active {
			return nil
		}
		pos, rot, scl := vec(n.tr["m_LocalPosition"]), n.tr["m_LocalRotation"].(map[string]any), vec(n.tr["m_LocalScale"])
		qx, qy, qz, qw := num(rot["x"]), num(rot["y"]), num(rot["z"]), num(rot["w"])
		offPlane = offPlane || math.Abs(qx) > 1e-6 || math.Abs(qy) > 1e-6
		ang := 2 * math.Atan2(qz, qw)
		cos, sin := math.Cos(ang), math.Sin(ang)
		local := [6]float64{cos * scl[0], sin * scl[0], -sin * scl[1], cos * scl[1], pos[0], pos[1]}
		m = mul(m, local)
		z += pos[2]
		// alpha of this node's UIRect (panel or widget); AnimatedAlpha and
		// tweens enabled at instantiation override the serialized value
		own := 1.0
		rect := false
		if p, ok := n.comps["UIPanel"]; ok && enabled(p) {
			rect = true
			own = num(p["mAlpha"])
			ps := &panelState{queue: int(num(p["startingRenderQueue"])), depth: int(num(p["mDepth"])), clip: panel.clip}
			if num(p["renderQueue"]) == 0 { // Automatic
				ps.queue = 3000
			}
			if c := int(num(p["mClipping"])); c != 0 {
				r := vec4(p["mClipRange"])
				off := vec(p["mClipOffset"])
				soft := vec(p["mClipSoftness"])
				cx, cy := r[0]+off[0], r[1]+off[1]
				wx, wy := apply(m, cx, cy)
				ps.clip = &stageClip{cx: wx, cy: wy, hw: r[2] / 2 * math.Hypot(m[0], m[1]), hh: r[3] / 2 * math.Hypot(m[2], m[3]),
					sx: soft[0] * math.Hypot(m[0], m[1]), sy: soft[1] * math.Hypot(m[2], m[3])}
			}
			panel = ps
		}
		var widget map[string]any
		var kind string
		for _, k := range []string{"UITexture", "UISprite", "UIWidget", "UILabel", "UI2DSprite"} {
			if w, ok := n.comps[k]; ok && enabled(w) {
				widget, kind = w, k
			}
		}
		if widget != nil {
			rect = true
			own = num(widget["mColor"].(map[string]any)["a"])
		}
		if a, ok := n.comps["AnimatedAlpha"]; ok && enabled(a) {
			own = num(a["alpha"])
		}
		if t, ok := n.comps["TweenAlpha"]; ok && enabled(t) {
			own = num(t["from"])
		}
		for _, k := range []string{"TweenPosition", "TweenRotation", "TweenScale", "TweenColor", "TweenWidth", "TweenHeight"} {
			if t, ok := n.comps[k]; ok && enabled(t) {
				return fmt.Errorf("%s: %s enabled at instantiation is not supported", n.name, k)
			}
		}
		if rect {
			alpha *= own
		}
		if _, ok := n.comps["ParticleSystem"]; ok {
			// a particle system is animation, not part of the static frame
		}
		clipped := panel.clip != nil && (panel.clip.hw <= 0 || panel.clip.hh <= 0)
		if widget != nil && alpha > 0 && !clipped {
			switch kind {
			case "UITexture":
				if offPlane {
					return fmt.Errorf("%s: visible widget rotated outside the screen plane", n.name)
				}
				if num(widget["mType"]) != 0 || num(widget["mFlip"]) != 0 { // fill amount only applies to the Filled type
					return fmt.Errorf("%s: only simple unflipped UITexture is supported", n.name)
				}
				shader := widget["mShader"]
				if mat := ptr(widget["mMat"]); mat != 0 {
					mo, ok := objs[mat]
					if !ok {
						return fmt.Errorf("%s: material %d not in bundle", n.name, mat)
					}
					shader = mo.v["m_Shader"]
				}
				sn, err := shaderName(shader)
				if err != nil {
					return fmt.Errorf("%s: %w", n.name, err)
				}
				blend, ok := stageShaders[sn]
				if !ok {
					return fmt.Errorf("%s: unsupported shader %s", n.name, sn)
				}
				tex, err := texture(widget["mTexture"])
				if err != nil {
					return fmt.Errorf("%s: %w", n.name, err)
				}
				w, h := num(widget["mWidth"]), num(widget["mHeight"])
				px, py := pivot(int(num(widget["mPivot"])))
				x0, y0 := -px*w, -py*h
				var corners [4][2]float64
				for i, c := range [4][2]float64{{x0, y0}, {x0, y0 + h}, {x0 + w, y0 + h}, {x0 + w, y0}} {
					corners[i][0], corners[i][1] = apply(m, c[0], c[1])
				}
				col := widget["mColor"].(map[string]any)
				uv := widget["mRect"].(map[string]any)
				draws = append(draws, stageDraw{
					name: n.name, tex: tex, corners: corners, z: z,
					uv:    [4]float64{num(uv["x"]), num(uv["y"]), num(uv["width"]), num(uv["height"])},
					color: [4]float64{num(col["r"]), num(col["g"]), num(col["b"]), alpha},
					blend: blend, clip: panel.clip,
					order: [3]int{panel.queue, panel.depth, int(num(widget["mDepth"]))}, seq: seq,
				})
				seq++
			case "UIWidget":
				// container only
			default:
				return fmt.Errorf("%s: visible %s is not supported", n.name, kind)
			}
		}
		children, _ := n.tr["m_Children"].([]any)
		for _, c := range children {
			if err := walk(ptr(c), m, z, alpha, panel, offPlane); err != nil {
				return err
			}
		}
		return nil
	}
	// The prefab root sits at the stage root origin, scaled like the
	// bgImage widget (stageRootScale).
	root := [6]float64{stageRootScale, 0, 0, stageRootScale, 0, 0}
	if err := walk(roots[0], root, 0, 1, outer, false); err != nil {
		return nil, err
	}
	sort.SliceStable(draws, func(i, j int) bool {
		a, b := draws[i], draws[j]
		for k := range a.order {
			if a.order[k] != b.order[k] {
				return a.order[k] < b.order[k]
			}
		}
		return a.seq < b.seq
	})
	return draws, nil
}

// drawStage renders the widgets onto the canvas (UI units → pixels through
// toPx), blending in linear colour space like the original (linear project),
// and returns the widgets that changed pixels.
func drawStage(canvas *image.NRGBA, draws []stageDraw, toPx func(x, y float64) (float64, float64)) []string {
	var visible []string
	w, h := canvas.Rect.Dx(), canvas.Rect.Dy()
	depth := make([]float64, w*h)
	for i := range depth {
		depth[i] = math.Inf(1)
	}
	for _, d := range draws {
		changed := false
		var pc [4][2]float64
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for i, c := range d.corners {
			pc[i][0], pc[i][1] = toPx(c[0], c[1])
			minX, maxX = math.Min(minX, pc[i][0]), math.Max(maxX, pc[i][0])
			minY, maxY = math.Min(minY, pc[i][1]), math.Max(maxY, pc[i][1])
		}
		// inverse of the parallelogram bl + u·(br−bl) + v·(tl−bl)
		ox, oy := pc[0][0], pc[0][1]
		ux, uy := pc[3][0]-ox, pc[3][1]-oy
		vx, vy := pc[1][0]-ox, pc[1][1]-oy
		det := ux*vy - uy*vx
		if det == 0 {
			continue
		}
		tw, th := d.tex.Rect.Dx(), d.tex.Rect.Dy()
		for py := max(0, int(math.Floor(minY))); py < min(h, int(math.Ceil(maxY))); py++ {
			for px := max(0, int(math.Floor(minX))); px < min(w, int(math.Ceil(maxX))); px++ {
				fx, fy := float64(px)+0.5-ox, float64(py)+0.5-oy
				u := (fx*vy - fy*vx) / det
				v := (ux*fy - uy*fx) / det
				if u < 0 || u >= 1 || v < 0 || v >= 1 {
					continue
				}
				a := d.color[3]
				if c := d.clip; c != nil {
					wx, wy := uiFromPx(toPx, float64(px)+0.5, float64(py)+0.5)
					a *= clipFactor(c, wx, wy)
					if a <= 0 {
						continue
					}
				}
				// texture sample (bilinear), UV bottom-left origin
				s := d.uv[0] + u*d.uv[2]
				t := d.uv[1] + v*d.uv[3]
				r, g, b, ta := bilinear(d.tex, s*float64(tw)-0.5, (1-t)*float64(th)-0.5)
				o := canvas.PixOffset(px, py)
				i := py*w + px
				if d.blend == "cutout" {
					if ta*a > 0.5 && d.z <= depth[i] {
						depth[i] = d.z
					}
					continue
				}
				if d.z > depth[i] {
					continue // behind a depth cutout
				}
				sa := ta * a
				for k, sc := range [3]float64{r * d.color[0], g * d.color[1], b * d.color[2]} {
					dst := imgutil.SRGBToLinear(float64(canvas.Pix[o+k]) / 255)
					src := imgutil.SRGBToLinear(math.Min(1, sc))
					var out float64
					if d.blend == "additive" {
						out = dst + src*sa
					} else {
						out = src*sa + dst*(1-sa)
					}
					v := uint8(math.Round(imgutil.LinearToSRGB(math.Min(1, out)) * 255))
					changed = changed || v != canvas.Pix[o+k]
					canvas.Pix[o+k] = v
				}
			}
		}
		if changed {
			visible = append(visible, d.name)
		}
	}
	return visible
}

// uiFromPx inverts toPx (an axis-aligned scale and offset).
func uiFromPx(toPx func(x, y float64) (float64, float64), px, py float64) (float64, float64) {
	x0, y0 := toPx(0, 0)
	x1, y1 := toPx(1, 1)
	return (px - x0) / (x1 - x0), (py - y0) / (y1 - y0)
}

// clipFactor is NGUI's soft clip: 1 inside, falling to 0 over the softness
// at the edges; a zero-size clip region shows nothing.
func clipFactor(c *stageClip, x, y float64) float64 {
	if c.hw <= 0 || c.hh <= 0 {
		return 0
	}
	f := 1.0
	for _, e := range [][3]float64{{x - c.cx, c.hw, c.sx}, {y - c.cy, c.hh, c.sy}} {
		d := e[1] - math.Abs(e[0])
		if d < 0 {
			return 0
		}
		if e[2] > 0 {
			f = math.Min(f, d/e[2])
		}
	}
	return math.Min(1, f)
}

func bilinear(img *image.NRGBA, x, y float64) (r, g, b, a float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(xi, yi int) [4]float64 {
		xi, yi = min(max(xi, 0), w-1), min(max(yi, 0), h-1)
		p := img.Pix[img.PixOffset(xi, yi):]
		return [4]float64{float64(p[0]) / 255, float64(p[1]) / 255, float64(p[2]) / 255, float64(p[3]) / 255}
	}
	c00, c10, c01, c11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	var out [4]float64
	for k := range out {
		out[k] = (c00[k]*(1-fx)+c10[k]*fx)*(1-fy) + (c01[k]*(1-fx)+c11[k]*fx)*fy
	}
	return out[0], out[1], out[2], out[3]
}

// pivot returns NGUI's pivot as a fraction of the widget size from its
// bottom-left corner.
func pivot(p int) (float64, float64) {
	x := [3]float64{0, 0.5, 1}[p%3]
	y := [3]float64{1, 0.5, 0}[p/3]
	return x, y
}

func mul(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[2]*b[1], a[1]*b[0] + a[3]*b[1],
		a[0]*b[2] + a[2]*b[3], a[1]*b[2] + a[3]*b[3],
		a[0]*b[4] + a[2]*b[5] + a[4], a[1]*b[4] + a[3]*b[5] + a[5],
	}
}

func apply(m [6]float64, x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func enabled(v map[string]any) bool { return num(v["m_Enabled"]) != 0 }

func ptr(v any) int64 {
	m, _ := v.(map[string]any)
	switch p := m["m_PathID"].(type) {
	case int64:
		return p
	case uint64:
		return int64(p)
	case float64:
		return int64(p)
	}
	return 0
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func vec(v any) [3]float64 {
	m, _ := v.(map[string]any)
	return [3]float64{num(m["x"]), num(m["y"]), num(m["z"])}
}

func vec4(v any) [4]float64 {
	m, _ := v.(map[string]any)
	return [4]float64{num(m["x"]), num(m["y"]), num(m["z"]), num(m["w"])}
}
