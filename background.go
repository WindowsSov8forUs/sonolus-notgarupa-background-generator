// Package background builds the NotGarupa background pack: the live
// background texture laid out as the original Standard-live stage shows it
// during play (bgImage widget 1920×1440 UI units centred at y = −170 under a
// stage root scaled 0.92), with the DarkCover reverse-subtraction baked in.
// Stage lights, speakers and psyllium are animated and therefore drawn by the
// engine from the skin pack.
//
// Generate returns the background items in memory (for embedding in a
// server); WriteSource writes them as a sonolus-pack source tree. The command
// in cmd/sonolus-notgarupa-background-generator wraps both.
package background

import (
	"context"
	"fmt"
	"image"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-core-go/codec"
	"github.com/WindowsSov8forUs/sonolus-core-go/core/resource"
	"github.com/WindowsSov8forUs/sonolus-core-go/database"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/bestdori"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/imgutil"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/packout"

	xdraw "golang.org/x/image/draw"
)

// Original stage constants (UI units, y up, origin at the screen centre).
const (
	bgWidgetW      = 1920.0
	bgWidgetH      = 1440.0
	bgWidgetY      = -170.0
	stageRootScale = 0.92 // steady play (intro animates 0.7 → 0.92)
	// The canvas spans the whole bgImage widget at 0.92 (wider than the 1334
	// UI unit screen), so the engine's clear outro (Stage TRS back to scale
	// 0.7) still covers the screen.
	canvasWidth = bgWidgetW * stageRootScale
	// DarkCover: 1334×1500 quad at (0, 190), texture grey 121, CE/RevSubtraction
	// with widget RGB 0.25 (converted to linear by UIDrawCall); linear colour space.
	darkCoverW, darkCoverH, darkCoverY = 1334.0, 1500.0, 190.0
	darkCoverColor                     = 0.25
)

// Source configures where downloadable assets come from.
type Source struct {
	// CacheDir caches downloads (Bestdori files under <CacheDir>/bestdori);
	// empty disables the disk cache.
	CacheDir string
	// Regions is the Bestdori server priority order (nil:
	// jp, en, tw, cn, kr). Each bundle comes from the first server that has
	// it, so server-exclusive assets are found too.
	Regions []string
	// Offline fails every download that is not already cached.
	Offline bool
	// HTTPClient is used for downloads (nil: a client with a 90 s timeout).
	HTTPClient *http.Client
	// Log receives one line per download (nil: silent).
	Log func(format string, args ...any)
}

func (s Source) bestdori(ctx context.Context) *bestdori.Client {
	cache := ""
	if s.CacheDir != "" {
		cache = filepath.Join(s.CacheDir, "bestdori")
	}
	b := bestdori.New(cache)
	b.Regions = s.Regions
	if s.HTTPClient != nil {
		b.HTTP = s.HTTPClient
	}
	b.Offline, b.Log, b.Ctx = s.Offline, s.Log, ctx
	return b
}

// DefaultBackgrounds are the ordinary backgrounds built when
// Options.Backgrounds is empty.
var DefaultBackgrounds = []string{"skin00"}

// HabahiroBackground is the background the original forces for #HABAHIRO
// lives (MV and practice aside); it is always built.
const HabahiroBackground = "habahiro"

// Options selects what to build. The zero value builds the default pack.
type Options struct {
	// Backgrounds are the ordinary background bundles
	// ingameskin/bgskin/<id> (nil: DefaultBackgrounds). HabahiroBackground is
	// built in addition.
	Backgrounds []string
	// NoFever skips the liveBG_fever variants.
	NoFever bool
	// NoDarkCover skips baking the DarkCover subtraction.
	NoDarkCover bool
	// PxPerUnit is the output density in pixels per UI unit (0: the
	// texture's native density).
	PxPerUnit float64
	// Height is the canvas height in UI units, symmetric around the screen
	// centre (0: 1640, which covers the whole widget).
	Height float64
	Source Source
}

// Result is one generated background item.
type Result struct {
	// Item is the database item; its resource fields (Thumbnail, Data,
	// Image, Configuration) are left empty for the caller to fill, e.g. with
	// the server's repository URLs of Files().
	Item database.DatabaseBackgroundItem
	// Habahiro marks the items of #HABAHIRO levels, which the server forces
	// for them as the original does.
	Habahiro      bool
	Data          resource.BackgroundData
	Configuration resource.BackgroundConfiguration
	Image         []byte // PNG
	Thumbnail     []byte // PNG
	// Bundle, File and Variant ("normal" or "fever") name the source texture.
	Bundle, File, Variant string
	// PxPerUnit is the image density in pixels per UI unit.
	PxPerUnit float64
}

// Files returns the item's resource files by item.json field name:
// "thumbnail", "data" and "configuration" (gzip-compressed) and "image".
func (r *Result) Files() (map[string][]byte, error) {
	data, err := codec.Compress(r.Data)
	if err != nil {
		return nil, fmt.Errorf("compress background data: %w", err)
	}
	conf, err := codec.Compress(r.Configuration)
	if err != nil {
		return nil, fmt.Errorf("compress background configuration: %w", err)
	}
	return map[string][]byte{"thumbnail": r.Thumbnail, "data": data, "configuration": conf, "image": r.Image}, nil
}

// WriteSource (re)creates root as a sonolus-pack source tree holding the
// given background items.
func WriteSource(root string, results ...*Result) error {
	src, err := packout.Init(root, packout.T("NotGarupa Backgrounds", "NotGarupa 背景", "NotGarupa 背景"))
	if err != nil {
		return err
	}
	for _, r := range results {
		rel, err := src.ItemDir("backgrounds", r.Item.Name, packout.Item{
			Version: r.Item.Version, Title: r.Item.Title, Subtitle: r.Item.Subtitle, Author: r.Item.Author,
			Tags: r.Item.Tags, Description: r.Item.Description,
		})
		if err != nil {
			return err
		}
		files, err := r.Files()
		if err != nil {
			return err
		}
		if err := src.WriteFiles(rel, files); err != nil {
			return err
		}
	}
	return nil
}

// Generate builds one item per background bundle and variant, the ordinary
// backgrounds first and HabahiroBackground last: names are
// notgarupa-<bg>-normal and notgarupa-<bg>-fever.
func Generate(ctx context.Context, o Options) ([]*Result, error) {
	ordinary := o.Backgrounds
	if len(ordinary) == 0 {
		ordinary = DefaultBackgrounds
	}
	var backgrounds []string
	for _, id := range append(slices.Clone(ordinary), HabahiroBackground) {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(backgrounds, id) {
			backgrounds = append(backgrounds, id)
		}
	}
	heightUnits := o.Height
	if heightUnits <= 0 {
		heightUnits = 1640
	}
	dark := !o.NoDarkCover
	bd := o.Source.bestdori(ctx)
	coverTex, err := darkCover()
	if err != nil {
		return nil, err
	}

	var results []*Result
	for _, id := range backgrounds {
		bundle := "ingameskin/bgskin/" + id
		files, err := bd.Manifest(bundle)
		if err != nil {
			return nil, err
		}
		variants := map[string]string{}
		for _, f := range files {
			switch strings.ToLower(f) {
			case "livebg_normal.png", "livebg.png":
				variants["normal"] = f
			case "livebg_fever.png":
				if !o.NoFever {
					variants["fever"] = f
				}
			}
		}
		if variants["normal"] == "" {
			return nil, fmt.Errorf("%s: no liveBG_normal.png / liveBG.png in %v", bundle, files)
		}
		for _, v := range []string{"normal", "fever"} {
			file, ok := variants[v]
			if !ok {
				continue
			}
			data, err := bd.File(bundle, file)
			if err != nil {
				return nil, err
			}
			tex, err := imgutil.Decode(data)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", bundle, file, err)
			}
			scale := o.PxPerUnit
			if scale <= 0 {
				scale = float64(tex.Rect.Dx()) / (bgWidgetW * stageRootScale)
			}
			img := compose(tex, coverTex, scale, heightUnits, dark)
			png, err := imgutil.EncodePNG(img)
			if err != nil {
				return nil, err
			}
			w, h := img.Rect.Dx(), img.Rect.Dy()
			thumb, err := imgutil.EncodePNG(imgutil.Scale(img, 256, int(math.Round(256*float64(h)/float64(w)))))
			if err != nil {
				return nil, err
			}
			name := fmt.Sprintf("notgarupa-%s-%s", strings.ToLower(id), v)
			title := fmt.Sprintf("NotGarupa %s (%s)", id, v)
			desc := fmt.Sprintf("%s/%s laid out as the Standard-live stage (scale %.2f, DarkCover %v). Canvas: %.1f × %.0f UI units around the screen centre (the engine places it).", bundle, file, stageRootScale, dark, canvasWidth, heightUnits)
			results = append(results, &Result{
				Item: database.DatabaseBackgroundItem{
					Name:        name,
					Version:     packout.BackgroundVersion,
					Title:       packout.T(title, title, title),
					Subtitle:    packout.T("BanG Dream! Girls Band Party! (original art)", "BanG Dream! 少女乐团派对！（原作素材）", "バンドリ！ ガールズバンドパーティ！（原作素材）"),
					Author:      packout.T("sonolus-notgarupa-background-generator", "sonolus-notgarupa-background-generator", "sonolus-notgarupa-background-generator"),
					Tags:        []database.DatabaseTag{},
					Description: packout.T(desc, desc, desc),
				},
				Data:          resource.BackgroundData{AspectRatio: float64(w) / float64(h), Fit: "width", Color: "#000000"},
				Configuration: resource.BackgroundConfiguration{Scope: "notgarupa", Blur: 0, Mask: "#00000000"},
				Image:         png,
				Thumbnail:     thumb,
				Habahiro:      id == HabahiroBackground,
				Bundle:        bundle, File: file, Variant: v,
				PxPerUnit: scale,
			})
		}
	}
	return results, nil
}

// darkCover decodes the stage's DarkCover texture (built into the APK).
func darkCover() (*image.NRGBA, error) {
	data, err := apkFile("prefabs/bms/stage/stage-dark-cover.png")
	if err != nil {
		return nil, err
	}
	return imgutil.Decode(data)
}

// compose renders the canvas: width canvasWidth UI units, height
// `heightUnits`, both centred on the screen centre, at `scale` px per UI unit.
func compose(tex, cover *image.NRGBA, scale, heightUnits float64, dark bool) *image.NRGBA {
	w := int(math.Round(canvasWidth * scale))
	h := int(math.Round(heightUnits * scale))
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	toPx := func(x, y float64) (float64, float64) { // UI (y up) → canvas px
		return (x + canvasWidth/2) * scale, (heightUnits/2 - y) * scale
	}
	// bgImage: full UV stretched over the widget, under the 0.92 stage root.
	bw, bh := bgWidgetW*stageRootScale, bgWidgetH*stageRootScale
	cx, cy := 0.0, bgWidgetY*stageRootScale
	x0, y0 := toPx(cx-bw/2, cy+bh/2)
	x1, y1 := toPx(cx+bw/2, cy-bh/2)
	dst := image.Rect(int(math.Round(x0)), int(math.Round(y0)), int(math.Round(x1)), int(math.Round(y1)))
	xdraw.CatmullRom.Scale(canvas, dst, tex, tex.Rect, xdraw.Over, nil)
	// Opaque black where the widget does not reach (the original shows the
	// camera clear colour / letterbox there).
	for i := 3; i < len(canvas.Pix); i += 4 {
		if canvas.Pix[i] != 255 {
			a := float64(canvas.Pix[i]) / 255
			canvas.Pix[i-3] = uint8(float64(canvas.Pix[i-3]) * a)
			canvas.Pix[i-2] = uint8(float64(canvas.Pix[i-2]) * a)
			canvas.Pix[i-1] = uint8(float64(canvas.Pix[i-1]) * a)
			canvas.Pix[i] = 255
		}
	}
	if !dark {
		return canvas
	}
	// DarkCover: dst_linear -= tex_linear × linear(0.25), clamped at 0. The
	// cover is screen-fixed and fills every visible position at every Stage
	// pose; its texture is uniform, so it is applied over the whole canvas
	// (sampled at the clamped cover UV), which stays exact while the canvas
	// moves with the Stage.
	k := imgutil.SRGBToLinear(darkCoverColor)
	dx0, dy0 := toPx(-darkCoverW/2, darkCoverY+darkCoverH/2)
	dx1, dy1 := toPx(darkCoverW/2, darkCoverY-darkCoverH/2)
	cw, ch := cover.Rect.Dx(), cover.Rect.Dy()
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			u := math.Max(0, (float64(px)+0.5-dx0)/(dx1-dx0))
			v := math.Max(0, (float64(py)+0.5-dy0)/(dy1-dy0))
			tp := cover.Pix[cover.PixOffset(min(cw-1, int(u*float64(cw))), min(ch-1, int(v*float64(ch)))):]
			ta := float64(tp[3]) / 255
			o := canvas.PixOffset(px, py)
			for c := 0; c < 3; c++ {
				sub := imgutil.SRGBToLinear(float64(tp[c])/255) * ta * k
				lin := math.Max(0, imgutil.SRGBToLinear(float64(canvas.Pix[o+c])/255)-sub)
				canvas.Pix[o+c] = uint8(math.Round(imgutil.LinearToSRGB(lin) * 255))
			}
		}
	}
	return canvas
}
