package imageutil

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeJPEG(w, h, quality int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// 用伪随机填充以生成不可压缩的图片
	for i := range img.Pix {
		img.Pix[i] = byte((i*7 + 13) % 256)
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes()
}

func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// 更高熵：xorshift 伪随机
	s := uint32(0xDEADBEEF)
	for i := range img.Pix {
		s ^= s << 13
		s ^= s >> 17
		s ^= s << 5
		img.Pix[i] = byte(s)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestProcessImage_SmallImagePassthrough(t *testing.T) {
	data := makeJPEG(100, 100, 90)
	if len(data) > maxBytes {
		t.Fatal("test setup: small image exceeds threshold")
	}
	out, mime := ProcessImage(data, "image/jpeg")
	if !bytes.Equal(out, data) {
		t.Error("small image should be returned unchanged")
	}
	if mime != "image/jpeg" {
		t.Errorf("mime should be unchanged, got %s", mime)
	}
}

func TestProcessImage_GIFPassthrough(t *testing.T) {
	// 构造一个超过阈值的假 GIF 数据
	data := make([]byte, maxBytes+1)
	copy(data, []byte("GIF89a"))
	out, mime := ProcessImage(data, "image/gif")
	if !bytes.Equal(out, data) {
		t.Error("GIF should be returned unchanged")
	}
	if mime != "image/gif" {
		t.Errorf("GIF mime should be unchanged, got %s", mime)
	}
}

func TestProcessImage_DecodeFallback(t *testing.T) {
	data := make([]byte, maxBytes+1)
	data[0] = 0xFF // 非法图片数据
	out, mime := ProcessImage(data, "image/jpeg")
	if !bytes.Equal(out, data) {
		t.Error("decode failure should return original data")
	}
	if mime != "image/jpeg" {
		t.Errorf("mime should be unchanged on decode failure, got %s", mime)
	}
}

func TestProcessImage_LargeJPEGCompressed(t *testing.T) {
	// 3000x3000 应该触发缩放 + 压缩
	data := makeJPEG(3000, 3000, 100)
	if len(data) <= maxBytes {
		t.Skip("test image not large enough to trigger compression")
	}
	out, mime := ProcessImage(data, "image/jpeg")
	if mime != "image/jpeg" {
		t.Errorf("output should be JPEG, got %s", mime)
	}
	if len(out) > maxBytes {
		t.Errorf("output should be <= %d bytes, got %d", maxBytes, len(out))
	}
	if len(out) >= len(data) {
		t.Errorf("output should be smaller than input: %d >= %d", len(out), len(data))
	}
}

func TestProcessImage_LargePNGCompressed(t *testing.T) {
	data := makePNG(4000, 3000)
	if len(data) <= maxBytes {
		t.Skip("test PNG not large enough")
	}
	out, mime := ProcessImage(data, "image/png")
	if mime != "image/jpeg" {
		t.Errorf("output should be JPEG, got %s", mime)
	}
	if len(out) >= len(data) {
		t.Errorf("output should be smaller than input")
	}
}

func TestProcessModelInputImage_UsesTighterBudget(t *testing.T) {
	data := makePNG(4000, 3000)
	if len(data) <= modelInputMaxBytes {
		t.Skip("test PNG not large enough")
	}
	out, mime := ProcessModelInputImage(data, "image/png")
	if mime != "image/jpeg" {
		t.Errorf("output should be JPEG, got %s", mime)
	}
	if len(out) > modelInputMaxBytes {
		t.Errorf("output should be <= %d bytes, got %d", modelInputMaxBytes, len(out))
	}
}

func TestScaleToFit_NoOpWhenSmall(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 200))
	result := scaleToFit(img, 2048)
	b := result.Bounds()
	if b.Dx() != 100 || b.Dy() != 200 {
		t.Errorf("small image should not be scaled, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestScaleToFit_ScalesLargeImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	result := scaleToFit(img, 2048)
	b := result.Bounds()
	if b.Dx() > 2048 || b.Dy() > 2048 {
		t.Errorf("scaled image should fit within 2048, got %dx%d", b.Dx(), b.Dy())
	}
	if b.Dx() != 2048 {
		t.Errorf("longer side should be 2048, got %d", b.Dx())
	}
}
