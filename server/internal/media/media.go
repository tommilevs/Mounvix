// Package media генерирует превью для изображений (кэш в data/cache/thumbs).
package media

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// Supported — можно ли сделать превью для этого файла.
func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

// CachePath — путь файла превью в кэше (детерминированный хэш от root+rel+ширины).
func CachePath(cacheDir, root, rel string, width int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%d", root, rel, width)))
	name := hex.EncodeToString(sum[:]) + ".jpg"
	return filepath.Join(cacheDir, name)
}

// Generate создаёт JPEG-превью шириной не более maxWidth. Возвращает false, если формат не поддержан.
func Generate(srcPath, outPath string, maxWidth int) (bool, error) {
	if !Supported(srcPath) {
		return false, nil
	}
	if maxWidth <= 0 {
		maxWidth = 512
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return true, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return true, fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return true, fmt.Errorf("empty image")
	}
	nw, nh := maxWidth, h*maxWidth/w
	if w <= maxWidth {
		nw, nh = w, h
	}
	if nh <= 0 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return true, err
	}
	tmp := outPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return true, err
	}
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: 82}); err != nil {
		out.Close()
		os.Remove(tmp)
		return true, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return true, err
	}
	return true, os.Rename(tmp, outPath)
}

// IsVideo — расширение относится к видео (для галереи).
func IsVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
		return true
	}
	return false
}
