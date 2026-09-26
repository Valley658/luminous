package imgvalidate

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, dir string, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return p
}

func genuinePNGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func genuineJPEGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func genuineGIFBytes() []byte {
	img := image.NewPaletted(image.Rect(0, 0, 4, 4), []color.Color{color.Black, color.White})
	var buf bytes.Buffer
	_ = gif.Encode(&buf, img, nil)
	return buf.Bytes()
}

// PNG header claiming a huge (10001x10001) image but with no real pixel
// data behind it - image.DecodeConfig only reads the IHDR chunk, so this is
// enough to exercise the dimension-limit rejection without allocating a
// genuinely huge image in the test.
func oversizedPNGHeaderBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	raw := buf.Bytes()
	// PNG: 8-byte signature, then IHDR chunk: 4-byte length(=13) + "IHDR" + width(4) + height(4) + ...
	// Width/height live at byte offset 16 and 20 (big-endian).
	out := make([]byte, len(raw))
	copy(out, raw)
	big := uint32(10001)
	out[16] = byte(big >> 24)
	out[17] = byte(big >> 16)
	out[18] = byte(big >> 8)
	out[19] = byte(big)
	out[20] = byte(big >> 24)
	out[21] = byte(big >> 16)
	out[22] = byte(big >> 8)
	out[23] = byte(big)
	// IHDR chunk data is bytes [12:29) ("IHDR" + 13 bytes of fields); the
	// PNG decoder verifies the CRC over that span, stored at [29:33). Recompute
	// it so DecodeConfig gets past checksum validation and we genuinely
	// exercise the width/height limit check, not just "corrupt file rejected".
	crc := crc32.ChecksumIEEE(out[12:29])
	out[29] = byte(crc >> 24)
	out[30] = byte(crc >> 16)
	out[31] = byte(crc >> 8)
	out[32] = byte(crc)
	return out
}

func TestValidateAndReencode_GenuinePNGAccepted(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", genuinePNGBytes())
	res, err := ValidateAndReencode(src, dir, "out", 85)
	if err != nil {
		t.Fatalf("expected genuine PNG to be accepted, got error: %v", err)
	}
	if res.Format != "png" || res.Ext != "png" {
		t.Fatalf("expected format/ext png, got %+v", res)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("expected re-encoded file to exist: %v", err)
	}
}

func TestValidateAndReencode_GenuineJPEGAccepted(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", genuineJPEGBytes())
	res, err := ValidateAndReencode(src, dir, "out", 85)
	if err != nil {
		t.Fatalf("expected genuine JPEG to be accepted, got error: %v", err)
	}
	if res.Format != "jpeg" || res.Ext != "jpg" {
		t.Fatalf("expected format jpeg/ext jpg, got %+v", res)
	}
}

func TestValidateAndReencode_GenuineGIFAccepted(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", genuineGIFBytes())
	res, err := ValidateAndReencode(src, dir, "out", 85)
	if err != nil {
		t.Fatalf("expected genuine GIF to be accepted, got error: %v", err)
	}
	if res.Format != "gif" || res.Ext != "gif" {
		t.Fatalf("expected format/ext gif, got %+v", res)
	}
}

func TestValidateAndReencode_PlainTextRejected(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", []byte("this is just plain text, not an image at all"))
	if _, err := ValidateAndReencode(src, dir, "out", 85); err == nil {
		t.Fatal("expected plain text to be rejected, got no error")
	}
}

// This is the exact reported vulnerability: plain text content, saved with
// a .png-looking name/content-type by the caller, but with no real PNG
// signature. Content-based validation must reject it regardless of what
// the client claimed the file was.
func TestValidateAndReencode_FakePNGRejected(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "fake.png", []byte("plain text pretending to be fake.png"))
	if _, err := ValidateAndReencode(src, dir, "out", 85); err == nil {
		t.Fatal("expected fake.png (plain text) to be rejected, got no error")
	}
}

func TestValidateAndReencode_TruncatedPNGRejected(t *testing.T) {
	dir := t.TempDir()
	full := genuinePNGBytes()
	truncated := full[:len(full)/2]
	src := writeTemp(t, dir, "in.bin", truncated)
	if _, err := ValidateAndReencode(src, dir, "out", 85); err == nil {
		t.Fatal("expected truncated PNG to be rejected, got no error")
	}
}

func TestValidateAndReencode_EmptyFileRejected(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", []byte{})
	if _, err := ValidateAndReencode(src, dir, "out", 85); err == nil {
		t.Fatal("expected empty file to be rejected, got no error")
	}
}

func TestValidateAndReencode_OversizedDimensionsRejected(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", oversizedPNGHeaderBytes())
	_, err := ValidateAndReencode(src, dir, "out", 85)
	if err == nil {
		t.Fatal("expected oversized-dimension PNG header to be rejected, got no error")
	}
	if err != ErrImageTooLarge {
		t.Fatalf("expected ErrImageTooLarge, got: %v", err)
	}
}

func TestValidateAndReencode_NoFileLeftOnRejection(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "in.bin", []byte("not an image"))
	if _, err := ValidateAndReencode(src, dir, "out", 85); err == nil {
		t.Fatal("expected rejection")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "in.bin" {
			t.Fatalf("expected no output file to be left behind on rejection, found: %s", e.Name())
		}
	}
}

func TestRandomBaseName_UniqueAndPrefixed(t *testing.T) {
	a := RandomBaseName("fanart", 16)
	b := RandomBaseName("fanart", 16)
	if a == b {
		t.Fatal("expected two random base names to differ")
	}
	if len(a) < len("fanart_")+10 {
		t.Fatalf("unexpected short base name: %s", a)
	}
}
