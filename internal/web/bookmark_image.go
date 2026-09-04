package web

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Combine only still images, keeping selected order and leaving videos as files.
// Use the shortest source height so combining never enlarges a source image.
func combineBookmarkImages(ctx context.Context, paths []string, destination string) ([]string, bool, error) {
	var images, remaining []string
	var sizes []image.Config
	height := 0
	for _, path := range paths {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg", ".png", ".webp":
		default:
			remaining = append(remaining, path)
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, false, err
		}
		size, _, decodeErr := image.DecodeConfig(file)
		if err := errors.Join(decodeErr, file.Close()); err != nil {
			return nil, false, err
		}
		if size.Width <= 0 || size.Height <= 0 || int64(size.Width)*int64(size.Height) > 64_000_000 {
			return nil, false, fmt.Errorf("source image exceeds combination size limit")
		}
		images = append(images, path)
		sizes = append(sizes, size)
		if height == 0 || size.Height < height {
			height = size.Height
		}
	}
	if len(images) < 2 {
		return paths, false, nil
	}
	width := 0
	for _, size := range sizes {
		width += max(1, int(int64(size.Width)*int64(height)/int64(size.Height)))
	}
	if int64(width)*int64(height) > 64_000_000 {
		return nil, false, fmt.Errorf("combined image exceeds 64 megapixels")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	x := 0
	for i, path := range images {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, false, err
		}
		source, _, decodeErr := image.Decode(file)
		if err := errors.Join(decodeErr, file.Close()); err != nil {
			return nil, false, err
		}
		w := max(1, int(int64(sizes[i].Width)*int64(height)/int64(sizes[i].Height)))
		draw.CatmullRom.Scale(canvas, image.Rect(x, 0, x+w, height), source, source.Bounds(), draw.Src, nil)
		x += w
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".combined-*.png")
	if err != nil {
		return nil, false, err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	encodeErr := png.Encode(file, canvas)
	if err := errors.Join(encodeErr, file.Sync(), file.Close(), ctx.Err()); err != nil {
		return nil, false, err
	}
	if err := os.Rename(temp, destination); err != nil {
		return nil, false, err
	}
	return remaining, true, nil
}
