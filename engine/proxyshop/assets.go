package proxyshop

import (
	"embed"
	"image"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// assetFiles holds the few icon images some Proxyshop PSDs do not carry
// themselves, such as a planeswalker transform frame whose icon group never
// got the plain triangle every card with no named frame effect defaults to.
// They are Proxyshop's own art, already converted and used as is elsewhere in
// this template set, not drawn by this package
//
//go:embed assets/*.png
var assetFiles embed.FS

// transformIconAssets maps a transform icon's key to the embedded art a PSD
// missing that layer falls back to. Keyed by the same names
// engine/frame.TransformIconNames lists
var transformIconAssets = map[string]string{
	"convertdfc":    "assets/transform_icon_convertdfc.png",
	"upsidedowndfc": "assets/transform_icon_upsidedowndfc.png",
}

// loadAsset decodes an embedded PNG by the path transformIconAssets names
func loadAsset(path string) (image.Image, error) {
	f, err := assetFiles.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// loadAssetFitted decodes the embedded PNG at path and resamples it to fill
// crop's size, the way a part's own PSD-rendered variants are cropped to the
// part's shared canvas
func loadAssetFitted(path string, crop image.Rectangle) (*image.NRGBA, error) {
	src, err := loadAsset(path)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	xdraw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return out, nil
}
