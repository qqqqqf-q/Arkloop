package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func solidPNG(w, h int, fill color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func decodeTestImage(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	return img
}

func TestPrepareModelInputImageAddsTopBanner(t *testing.T) {
	source := solidPNG(320, 180, color.RGBA{R: 240, G: 40, B: 40, A: 255})

	out, mime := PrepareModelInputImage(source, "image/png", "attachments/account/thread/image.jpg")
	if mime != "image/jpeg" {
		t.Fatalf("unexpected mime: %q", mime)
	}

	img := decodeTestImage(t, out)
	if got := img.Bounds().Dy(); got != 180+modelInputBannerHeight {
		t.Fatalf("unexpected height: %d", got)
	}

	top := color.RGBAModel.Convert(img.At(4, 4)).(color.RGBA)
	if top.R < 240 || top.G < 240 || top.B < 240 {
		t.Fatalf("expected white banner, got %#v", top)
	}

	body := color.RGBAModel.Convert(img.At(10, modelInputBannerHeight+10)).(color.RGBA)
	if body.R < 180 || body.G > 120 || body.B > 120 {
		t.Fatalf("expected original image body to remain visible, got %#v", body)
	}

	nonWhite := 0
	for y := 0; y < modelInputBannerHeight; y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.R < 220 || px.G < 220 || px.B < 220 {
				nonWhite++
				break
			}
		}
	}
	if nonWhite == 0 {
		t.Fatal("expected banner text pixels in top area")
	}
}

func TestPrepareModelInputImageWithoutKeyPassthrough(t *testing.T) {
	source := solidPNG(64, 64, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	out, mime := PrepareModelInputImage(source, "image/png", "")
	if !bytes.Equal(out, source) {
		t.Fatal("expected original bytes when key is empty")
	}
	if mime != "image/png" {
		t.Fatalf("unexpected mime: %q", mime)
	}
}

func TestPrepareModelInputImageDecodeFailureFallsBack(t *testing.T) {
	source := []byte("not-an-image")

	out, mime := PrepareModelInputImage(source, "image/jpeg", "attachments/a/b/c.jpg")
	if !bytes.Equal(out, source) {
		t.Fatal("expected original bytes on decode failure")
	}
	if mime != "image/jpeg" {
		t.Fatalf("unexpected mime: %q", mime)
	}
}

func TestPrepareModelInputImageBannerUsesOriginalImageOffset(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	img.Set(5, 6, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	out, _ := PrepareModelInputImage(buf.Bytes(), "image/jpeg", "attachments/a/b/c.jpg")
	rendered := decodeTestImage(t, out)
	got := color.RGBAModel.Convert(rendered.At(5, modelInputBannerHeight+6)).(color.RGBA)
	if got.R > 30 || got.G > 30 || got.B > 30 {
		t.Fatalf("expected dark original pixel below banner, got %#v", got)
	}
}
