// Package background builds the NotGarupa background items: every live
// background of the original game's master data (event backgrounds, the
// Collaboration / Limited backgrounds and the HABAHIRO background), laid out
// as the Standard live stage shows it during play (bgImage widget 1920×1440
// UI units centred at y = −170 under a stage root scaled 0.92), with the
// special backgrounds' stage effects in their play state and the DarkCover
// reverse-subtraction baked in. Stage lights, speakers and psyllium are
// animated and therefore drawn by the engine from the skin pack.
//
// Generate returns the items in memory one by one (for embedding in a
// server); CreateSourceTree writes them as a sonolus-pack source tree. The
// command in cmd/sonolus-notgarupa-background-generator wraps both.
package background

import (
	"context"
	"fmt"
	"image"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/WindowsSov8forUs/sonolus-core-go/codec"
	"github.com/WindowsSov8forUs/sonolus-core-go/core/resource"
	"github.com/WindowsSov8forUs/sonolus-core-go/database"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/bestdori"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/cdn"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/imgutil"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/master"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/packout"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/unity"

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

// Output constants: the canvas is composed canvasHeight UI units high
// (symmetric around the screen centre) at the background texture's native
// density, then cut at canvasTop, the bgImage widget's top edge: above it the
// canvas is only the opaque black where the widget does not reach, which the
// background colour (#000000) shows instead. Sonolus places a background
// itself where the engine cannot (Preview), and it would show that band.
const (
	canvasHeight = 1640.0
	canvasTop    = bgWidgetY*stageRootScale + bgWidgetH*stageRootScale/2 // 506
)

// PinnedAssetVersion is the content CDN asset version the generator was
// verified against.
const PinnedAssetVersion = cdn.PinnedVersion

// Source configures where downloadable assets come from: the background
// textures from Bestdori, the stage-effect prefabs from the official CDN.
type Source struct {
	// CacheDir caches downloads (<CacheDir>/bestdori, <CacheDir>/cdn);
	// empty disables the disk cache.
	CacheDir string
	// Regions is the Bestdori server priority order (nil: jp, en, tw, cn,
	// kr). Each bundle comes from the first server that has it.
	Regions []string
	// AssetVersion is the content CDN asset version ("" = PinnedAssetVersion).
	AssetVersion string
	// Offline fails every download that is not already cached.
	Offline bool
	// HTTPClient is used for downloads (nil: the clients' defaults).
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

func (s Source) cdn(ctx context.Context) *cdn.Client {
	return &cdn.Client{Version: s.AssetVersion, CacheDir: s.CacheDir, Offline: s.Offline, HTTP: s.HTTPClient, Log: s.Log, Ctx: ctx}
}

// Options configures a run; the zero value builds everything.
type Options struct {
	Source Source
	// Workers is the number of backgrounds built in parallel (0: the CPU count).
	Workers int
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
	// Bundle and File name the source texture.
	Bundle, File string
	// StageEffect lists the stage-effect widgets visible in the image.
	StageEffect []string
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

// SourceTree is a sonolus-pack source tree being written.
type SourceTree struct{ src *packout.Source }

// CreateSourceTree (re)creates root as a sonolus-pack source tree for
// background items.
func CreateSourceTree(root string) (*SourceTree, error) {
	src, err := packout.Init(root, packout.T("NotGarupa Backgrounds", "NotGarupa 背景", "NotGarupa 背景", "NotGarupa 背景"))
	if err != nil {
		return nil, err
	}
	return &SourceTree{src}, nil
}

// Add writes one item.
func (t *SourceTree) Add(r *Result) error {
	rel, err := t.src.ItemDir("backgrounds", r.Item.Name, packout.Item{
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
	return t.src.WriteFiles(rel, files)
}

func plan() ([]master.Background, error) {
	m, err := master.Load()
	if err != nil {
		return nil, err
	}
	return m.Backgrounds(), nil
}

// Items returns the names of every item Generate builds, in order. It needs
// no network. Only the normal texture is generated: the original loads
// liveBG_fever only for the multi live modes with Fever (README, "Fever 背景").
func Items() ([]string, error) {
	p, err := plan()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(p))
	for i, bg := range p {
		out[i] = bg.Name()
	}
	return out, nil
}

// Generate builds every background item and hands them to emit in Items
// order; emit is called from one goroutine at a time. It returns the first
// error.
func Generate(ctx context.Context, opts Options, emit func(*Result) error) error {
	bgs, err := plan()
	if err != nil {
		return err
	}
	coverTex, err := darkCover()
	if err != nil {
		return err
	}
	bd := opts.Source.bestdori(ctx)
	bundles := opts.Source.cdn(ctx)
	type job struct {
		bg   master.Background
		res  *Result
		err  error
		done chan struct{}
	}
	jobs := make([]*job, len(bgs))
	for i, bg := range bgs {
		jobs[i] = &job{bg: bg, done: make(chan struct{})}
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan *job)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				j.res, j.err = buildBundle(ctx, bd, bundles, coverTex, j.bg)
				close(j.done)
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, j := range jobs {
			select {
			case queue <- j:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer wg.Wait()
	for _, j := range jobs {
		select {
		case <-j.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if j.err != nil {
			return fmt.Errorf("%s: %w", j.bg.Bundle, j.err)
		}
		if err := emit(j.res); err != nil {
			return err
		}
	}
	return nil
}

func buildBundle(ctx context.Context, bd *bestdori.Client, bundles *cdn.Client, coverTex *image.NRGBA, bg master.Background) (*Result, error) {
	bundle := "ingameskin/bgskin/" + bg.Bundle
	files, err := bd.Manifest(bundle)
	if err != nil {
		return nil, err
	}
	file := ""
	for _, f := range files {
		switch strings.ToLower(f) {
		case "livebg_normal.png", "livebg.png":
			file = f
		}
	}
	if file == "" {
		return nil, fmt.Errorf("%s: no liveBG_normal.png / liveBG.png in %v", bundle, files)
	}
	raw, err := bundles.Bundle(bundle)
	if err != nil {
		return nil, err
	}
	unityFiles, err := unity.ReadBundle(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", bundle, err)
	}
	stage, err := stageEffect(unityFiles)
	if err != nil {
		return nil, fmt.Errorf("%s stage effect: %w", bundle, err)
	}
	data, err := bd.File(bundle, file)
	if err != nil {
		return nil, err
	}
	tex, err := imgutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", bundle, file, err)
	}
	scale := float64(tex.Rect.Dx()) / (bgWidgetW * stageRootScale)
	img, names := compose(tex, coverTex, stage, scale)
	if img, err = cutTop(img, scale); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", bundle, file, err)
	}
	png, err := imgutil.EncodePNG(img)
	if err != nil {
		return nil, err
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	thumb, err := imgutil.EncodePNG(imgutil.Scale(img, 256, int(math.Round(256*float64(h)/float64(w)))))
	if err != nil {
		return nil, err
	}
	title := bg.Title
	desc := fmt.Sprintf("%s/%s laid out as the Standard-live stage (scale %.2f, DarkCover %v). Canvas: %.1f UI units wide around the screen centre, from y %.0f to %.0f (the engine places it).", bundle, file, stageRootScale, true, canvasWidth, canvasTop, -canvasHeight/2)
	if len(names) > 0 {
		desc += fmt.Sprintf(" Stage effect in its play state: %s.", strings.Join(names, ", "))
	}
	return &Result{
		Item: database.DatabaseBackgroundItem{
			Name:        bg.Name(),
			Version:     packout.BackgroundVersion,
			Title:       packout.L(title),
			Subtitle:    packout.T("BanG Dream! Girls Band Party!", "BanG Dream! 少女乐团派对！", "BanG Dream! 少女樂團派對！", "バンドリ！ ガールズバンドパーティ！"),
			Author:      packout.T("GirlsBandParty", "GirlsBandParty", "GirlsBandParty", "GirlsBandParty"),
			Tags:        []database.DatabaseTag{},
			Description: packout.T(desc, desc, desc, desc),
		},
		Habahiro:      bg.Habahiro,
		Data:          resource.BackgroundData{AspectRatio: float64(w) / float64(h), Fit: "cover", Color: "#000000"},
		Configuration: resource.BackgroundConfiguration{Scope: "notgarupa", Blur: 0, Mask: "#00000000"},
		Image:         png,
		Thumbnail:     thumb,
		Bundle:        bundle, File: file,
		StageEffect: names,
		PxPerUnit:   scale,
	}, nil
}

// darkCover decodes the stage's DarkCover texture (built into the APK).
func darkCover() (*image.NRGBA, error) {
	data, err := apkFile("prefabs/bms/stage/stage-dark-cover.png")
	if err != nil {
		return nil, err
	}
	return imgutil.Decode(data)
}

// cutTop removes the canvas above canvasTop, which must be opaque black.
func cutTop(img *image.NRGBA, scale float64) (*image.NRGBA, error) {
	rows := int(math.Round((canvasHeight/2 - canvasTop) * scale))
	for y := 0; y < rows; y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			if p := img.Pix[img.PixOffset(x, y):]; p[0]|p[1]|p[2] != 0 || p[3] != 255 {
				return nil, fmt.Errorf("canvas above the bgImage widget is not black at (%d, %d)", x, y)
			}
		}
	}
	return imgutil.Crop(img, 0, rows, img.Rect.Dx(), img.Rect.Dy()-rows)
}

// compose renders the canvas: width canvasWidth UI units, height
// canvasHeight, both centred on the screen centre, at scale px per UI unit.
func compose(tex, cover *image.NRGBA, stage []stageDraw, scale float64) (*image.NRGBA, []string) {
	w := int(math.Round(canvasWidth * scale))
	h := int(math.Round(canvasHeight * scale))
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	toPx := func(x, y float64) (float64, float64) { // UI (y up) → canvas px
		return (x + canvasWidth/2) * scale, (canvasHeight/2 - y) * scale
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
	// Stage-effect widgets (render queues 3100–3250) lie between the
	// bgImage and DarkCover (queue 3490).
	var visible []string
	if len(stage) > 0 {
		visible = drawStage(canvas, stage, toPx)
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
	return canvas, visible
}
