package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func decodeImage(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	return img, err
}

// grayAt returns the luminance (0..255) of a pixel composited over white.
func grayAt(img image.Image, x, y int) float32 {
	r, g, b, a := img.At(x, y).RGBA() // premultiplied, 16 bit
	white := 0xffff - a
	rr := float32(r+white) / 257
	gg := float32(g+white) / 257
	bb := float32(b+white) / 257
	return 0.299*rr + 0.587*gg + 0.114*bb
}

type grayImg struct {
	w, h int
	pix  []float32
}

func toGray(img image.Image, dstW int) *grayImg {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 || sh < 1 {
		return &grayImg{w: 1, h: 1, pix: []float32{255}}
	}
	if dstW <= 0 {
		dstW = sw
	}
	dstH := sh * dstW / sw
	if dstH < 1 {
		dstH = 1
	}
	g := &grayImg{w: dstW, h: dstH, pix: make([]float32, dstW*dstH)}
	for y := 0; y < dstH; y++ {
		y0 := b.Min.Y + y*sh/dstH
		y1 := b.Min.Y + (y+1)*sh/dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dstW; x++ {
			x0 := b.Min.X + x*sw/dstW
			x1 := b.Min.X + (x+1)*sw/dstW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum float32
			n := 0
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					sum += grayAt(img, xx, yy)
					n++
				}
			}
			g.pix[y*dstW+x] = sum / float32(n)
		}
	}
	return g
}

// rasterBytes converts a gray image to ESC/POS "GS v 0" raster bands.
func rasterBytes(g *grayImg, dither bool) []byte {
	w, h := g.w, g.h
	p := g.pix
	black := make([]bool, w*h)
	if dither {
		buf := make([]float32, len(p))
		copy(buf, p)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := y*w + x
				old := buf[i]
				var nv float32 = 255
				if old < 128 {
					nv = 0
					black[i] = true
				}
				e := old - nv
				if x+1 < w {
					buf[i+1] += e * 7 / 16
				}
				if y+1 < h {
					if x > 0 {
						buf[i+w-1] += e * 3 / 16
					}
					buf[i+w] += e * 5 / 16
					if x+1 < w {
						buf[i+w+1] += e * 1 / 16
					}
				}
			}
		}
	} else {
		for i, v := range p {
			black[i] = v < 160
		}
	}
	bpr := (w + 7) / 8
	var out bytes.Buffer
	const band = 128
	for y0 := 0; y0 < h; y0 += band {
		bh := min(band, h-y0)
		out.Write([]byte{0x1D, 0x76, 0x30, 0x00, byte(bpr & 0xFF), byte(bpr >> 8), byte(bh & 0xFF), byte(bh >> 8)})
		for y := y0; y < y0+bh; y++ {
			for bx := 0; bx < bpr; bx++ {
				var v byte
				for bit := 0; bit < 8; bit++ {
					x := bx*8 + bit
					if x < w && black[y*w+x] {
						v |= 0x80 >> bit
					}
				}
				out.WriteByte(v)
			}
		}
	}
	return out.Bytes()
}

// trimBottom removes the empty white area at the bottom of a page render.
func trimBottom(img image.Image) image.Image {
	b := img.Bounds()
	last := b.Min.Y
	for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
		found := false
		for x := b.Min.X; x < b.Max.X; x++ {
			if grayAt(img, x, y) < 250 {
				found = true
				break
			}
		}
		if found {
			last = y
			break
		}
	}
	bottom := min(b.Max.Y, last+10)
	if s, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		return s.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, bottom))
	}
	return img
}

// ---------- HTML -> raster through the Edge/Chrome already on the PC ----------

func findBrowser() string {
	cands := []string{cfg.BrowserPath}
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(base, `Google\Chrome\Application\chrome.exe`))
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func renderHTML(html string, dots, cssWidth int) (image.Image, error) {
	br := findBrowser()
	if br == "" {
		return nil, errors.New("no se encontró Microsoft Edge ni Chrome para dibujar el HTML")
	}
	if cssWidth <= 0 {
		cssWidth = dots / 2
	}
	zoom := float64(dots) / float64(cssWidth)

	tmp := filepath.Join(dataDir(), "tmp")
	_ = os.MkdirAll(tmp, 0o755)
	id := newNonce()[:10]
	htmlPath := filepath.Join(tmp, id+".html")
	pngPath := filepath.Join(tmp, id+".png")
	profile := filepath.Join(tmp, id+"-profile")
	defer func() {
		_ = os.Remove(htmlPath)
		_ = os.Remove(pngPath)
		_ = os.RemoveAll(profile)
	}()

	zoomCSS := fmt.Sprintf("<style>html{zoom:%g}</style>", zoom)
	doc := html
	if !strings.Contains(strings.ToLower(html), "<html") {
		doc = `<html><head><meta charset="utf-8">` + zoomCSS + `<style>html,body{margin:0;padding:0;background:#fff}</style></head><body>` + html + `</body></html>`
	} else {
		doc = `<meta charset="utf-8">` + zoomCSS + html
	}
	if err := os.WriteFile(htmlPath, []byte(doc), 0o644); err != nil {
		return nil, err
	}
	fileURL := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(htmlPath)}).String()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, br,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run",
		"--disable-extensions", "--allow-file-access-from-files",
		// Edge refuses windows narrower than ~500 px, so the page is rendered
		// at the full paper width and its layout is zoomed (see zoomCSS).
		"--force-device-scale-factor=1",
		fmt.Sprintf("--window-size=%d,%d", dots, 6000),
		"--virtual-time-budget=3000",
		"--user-data-dir="+profile,
		"--screenshot="+pngPath,
		fileURL)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		if _, serr := os.Stat(pngPath); serr != nil {
			return nil, fmt.Errorf("error dibujando HTML: %v %s", err, strings.TrimSpace(string(out)))
		}
	}
	b, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, fmt.Errorf("el navegador no generó la imagen: %w", err)
	}
	return decodeImage(b)
}
