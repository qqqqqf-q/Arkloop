package imageutil

import (
	"bytes"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

const (
	modelInputBannerHeight = 100
	bannerPaddingX         = 12
	bannerPaddingY         = 8
	bannerLineGap          = 2
)

var (
	bannerBackground = color.White
	bannerForeground = color.Black
)

// PrepareModelInputImage returns a temporary, model-only image variant.
// When an attachment key is present, it adds a top banner for the model.
// The final bytes are always squeezed back under the model input budget.
func PrepareModelInputImage(data []byte, mimeType, attachmentKey string) ([]byte, string) {
	key := strings.TrimSpace(attachmentKey)
	if len(data) == 0 {
		return data, mimeType
	}

	cleanedMime := normalizeMimeType(mimeType)
	if cleanedMime == "image/gif" {
		return data, mimeType
	}
	if key == "" {
		return ProcessModelInputImage(data, mimeType)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ProcessModelInputImage(data, mimeType)
	}

	rendered, err := renderAttachmentKeyBanner(img, "attachment_key: "+key)
	if err != nil {
		return ProcessModelInputImage(data, mimeType)
	}
	return ProcessModelInputImage(rendered, "image/jpeg")
}

func renderAttachmentKeyBanner(img image.Image, bannerText string) ([]byte, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	if width <= 0 {
		return nil, image.ErrFormat
	}

	height := bounds.Dy() + modelInputBannerHeight
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(dst, dst.Bounds(), &image.Uniform{bannerBackground}, image.Point{}, stddraw.Src)
	stddraw.Draw(dst, image.Rect(0, modelInputBannerHeight, width, height), img, bounds.Min, stddraw.Src)

	face, lines := selectBannerFace(width, bannerText)
	drawBannerText(dst, face, lines)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 95}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func selectBannerFace(width int, text string) (font.Face, []string) {
	faces := []font.Face{
		inconsolata.Bold8x16,
		basicfont.Face7x13,
	}
	maxWidth := width - bannerPaddingX*2
	maxHeight := modelInputBannerHeight - bannerPaddingY*2

	for _, face := range faces {
		lines := wrapBannerText(face, text, maxWidth)
		lineHeight := face.Metrics().Height.Ceil() + bannerLineGap
		if len(lines)*lineHeight <= maxHeight {
			return face, lines
		}
	}

	last := faces[len(faces)-1]
	return last, wrapBannerText(last, text, maxWidth)
}

func wrapBannerText(face font.Face, text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	lines := []string{}
	var current strings.Builder

	flush := func() {
		lines = append(lines, current.String())
		current.Reset()
	}

	for _, r := range text {
		candidate := current.String() + string(r)
		if current.Len() > 0 && font.MeasureString(face, candidate).Ceil() > maxWidth {
			flush()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 || len(lines) == 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func drawBannerText(dst *image.RGBA, face font.Face, lines []string) {
	drawer := &font.Drawer{
		Dst:  dst,
		Src:  &image.Uniform{bannerForeground},
		Face: face,
	}

	y := bannerPaddingY + face.Metrics().Ascent.Ceil()
	lineHeight := face.Metrics().Height.Ceil() + bannerLineGap
	for _, line := range lines {
		drawer.Dot = fixed.P(bannerPaddingX, y)
		drawer.DrawString(line)
		y += lineHeight
		if y > modelInputBannerHeight {
			return
		}
	}
}

func normalizeMimeType(mimeType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
}
