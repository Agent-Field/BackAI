// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxTransformSourceBytes = 32 << 20
	maxTransformDimension   = 4096
	defaultThumbnailSize    = 256
)

type storageTransform struct {
	Enabled bool
	Mode    string
	Width   int
	Height  int
	Format  string
}

func parseStorageTransform(q url.Values) (storageTransform, error) {
	mode := strings.TrimSpace(q.Get("transform"))
	if mode == "" {
		mode = strings.TrimSpace(q.Get("fit"))
	}
	width, err := parseTransformDimension(q.Get("width"))
	if err != nil {
		return storageTransform{}, err
	}
	if width == 0 {
		width, err = parseTransformDimension(q.Get("w"))
		if err != nil {
			return storageTransform{}, err
		}
	}
	height, err := parseTransformDimension(q.Get("height"))
	if err != nil {
		return storageTransform{}, err
	}
	if height == 0 {
		height, err = parseTransformDimension(q.Get("h"))
		if err != nil {
			return storageTransform{}, err
		}
	}
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	if format == "jpg" {
		format = "jpeg"
	}
	if format != "" && format != "png" && format != "jpeg" && format != "gif" {
		return storageTransform{}, fmt.Errorf("format must be png, jpeg, or gif")
	}

	enabled := mode != "" || width > 0 || height > 0 || format != ""
	if !enabled {
		return storageTransform{}, nil
	}
	if mode == "" {
		mode = "resize"
	}
	switch mode {
	case "resize", "thumbnail":
	default:
		return storageTransform{}, fmt.Errorf("transform must be resize or thumbnail")
	}
	if mode == "thumbnail" {
		if width == 0 && height == 0 {
			width = defaultThumbnailSize
			height = defaultThumbnailSize
		}
		if width == 0 {
			width = height
		}
		if height == 0 {
			height = width
		}
	}
	if width == 0 && height == 0 {
		return storageTransform{}, fmt.Errorf("width or height is required")
	}
	return storageTransform{
		Enabled: true,
		Mode:    mode,
		Width:   width,
		Height:  height,
		Format:  format,
	}, nil
}

func parseTransformDimension(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 || v > maxTransformDimension {
		return 0, fmt.Errorf("dimensions must be integers between 1 and %d", maxTransformDimension)
	}
	return v, nil
}

func transformStorageImage(body io.Reader, ct string, tf storageTransform) ([]byte, string, error) {
	limited := io.LimitReader(body, maxTransformSourceBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxTransformSourceBytes {
		return nil, "", fmt.Errorf("source object exceeds %d bytes", maxTransformSourceBytes)
	}
	src, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("source object is not a supported image")
	}
	format = normaliseImageFormat(format, ct)
	if tf.Format != "" {
		format = tf.Format
	}
	if format == "" {
		format = "png"
	}
	dst := resizeImage(src, tf)

	var out bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/jpeg", nil
	case "gif":
		if err := gif.Encode(&out, dst, nil); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/gif", nil
	default:
		if err := png.Encode(&out, dst); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/png", nil
	}
}

func normaliseImageFormat(format, ct string) string {
	if format == "jpg" {
		return "jpeg"
	}
	if format != "" {
		return format
	}
	switch strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0])) {
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	default:
		return ""
	}
}

func resizeImage(src image.Image, tf storageTransform) image.Image {
	b := src.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	dstW, dstH := targetDimensions(srcW, srcH, tf)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if tf.Mode == "thumbnail" {
		draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.Transparent}, image.Point{}, draw.Src)
	}
	for y := 0; y < dstH; y++ {
		sy := b.Min.Y + int(float64(y)*float64(srcH)/float64(dstH))
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < dstW; x++ {
			sx := b.Min.X + int(float64(x)*float64(srcW)/float64(dstW))
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func targetDimensions(srcW, srcH int, tf storageTransform) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 1, 1
	}
	w, h := tf.Width, tf.Height
	if w > 0 && h > 0 {
		if tf.Mode == "thumbnail" {
			scale := math.Min(float64(w)/float64(srcW), float64(h)/float64(srcH))
			if scale <= 0 {
				scale = 1
			}
			return maxInt(1, int(math.Round(float64(srcW)*scale))),
				maxInt(1, int(math.Round(float64(srcH)*scale)))
		}
		return w, h
	}
	if w > 0 {
		return w, maxInt(1, int(math.Round(float64(srcH)*float64(w)/float64(srcW))))
	}
	return maxInt(1, int(math.Round(float64(srcW)*float64(h)/float64(srcH)))), h
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func writeStorageTransformError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
}
