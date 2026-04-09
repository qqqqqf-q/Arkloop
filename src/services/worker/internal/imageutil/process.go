package imageutil

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxBytes           = 500 * 1024
	modelInputMaxBytes = 128 * 1024
	minDim             = 512
)

// 每轮尝试的 (长边上限, JPEG质量)
var compressSteps = []struct {
	dim     int
	quality int
}{
	{2048, 85},
	{1536, 75},
	{1024, 70},
	{768, 65},
}

// ProcessImage 对超过阈值的图片执行缩放+JPEG 压缩。
// GIF 和解码失败的图片原样返回。
func ProcessImage(data []byte, mimeType string) ([]byte, string) {
	return processImageWithLimit(data, mimeType, maxBytes)
}

// ProcessModelInputImage keeps images under a tighter budget before they are
// embedded into model requests, where base64 inflation is expensive.
func ProcessModelInputImage(data []byte, mimeType string) ([]byte, string) {
	return processImageWithLimit(data, mimeType, modelInputMaxBytes)
}

func processImageWithLimit(data []byte, mimeType string, limit int) ([]byte, string) {
	if len(data) <= limit {
		return data, mimeType
	}
	if mimeType == "image/gif" {
		return data, mimeType
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mimeType
	}

	var out []byte
	for _, step := range compressSteps {
		scaled := scaleToFit(img, step.dim)
		out = encodeJPEG(scaled, step.quality)
		if len(out) <= limit {
			return out, "image/jpeg"
		}
	}
	return out, "image/jpeg"
}

func scaleToFit(img image.Image, maxDim int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	longer := w
	if h > w {
		longer = h
	}
	if longer <= maxDim {
		return img
	}

	ratio := float64(maxDim) / float64(longer)
	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

func encodeJPEG(img image.Image, quality int) []byte {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes()
}
