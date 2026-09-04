package web

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestCombineBookmarkImages(t *testing.T) {
	dir := t.TempDir()
	makeImage := func(name string, width, height int, c color.Color) string {
		t.Helper()
		path := filepath.Join(dir, name)
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.Set(x, y, c)
			}
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}
	red := makeImage("first.png", 4, 4, color.RGBA{R: 255, A: 255})
	blue := makeImage("second.png", 6, 2, color.RGBA{B: 255, A: 255})
	video := filepath.Join(dir, "clip.mp4")
	output := filepath.Join(dir, "combined.png")
	remaining, combined, err := combineBookmarkImages(context.Background(), []string{red, video, blue}, output)
	if err != nil {
		t.Fatal(err)
	}
	if !combined || len(remaining) != 1 || remaining[0] != video {
		t.Fatalf("remaining=%v combined=%v", remaining, combined)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	if closeErr := f.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 8, 2) {
		t.Fatalf("bounds=%v", img.Bounds())
	}
	if r, _, _, _ := img.At(1, 1).RGBA(); r != 65535 {
		t.Fatal("first image is not on the left")
	}
	if _, _, b, _ := img.At(7, 1).RGBA(); b != 65535 {
		t.Fatal("second image is not on the right")
	}
	if _, err := os.Stat(red); err != nil {
		t.Fatal("source was removed", err)
	}
	if _, err := os.Stat(blue); err != nil {
		t.Fatal("source was removed", err)
	}
	unchanged, combined, err := combineBookmarkImages(context.Background(), []string{red, video}, filepath.Join(dir, "single.png"))
	if err != nil || combined || len(unchanged) != 2 {
		t.Fatalf("single image: %v %v %v", unchanged, combined, err)
	}
}

func TestCombineBookmarkImagesFailureDoesNotPublishPartialImage(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "combined.png")
	if _, _, err := combineBookmarkImages(context.Background(), []string{bad, bad}, output); err == nil {
		t.Fatal("expected decode failure")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("partial output exists: %v", err)
	}
}
