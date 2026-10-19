package main

import (
	"image/png"
	"os"
	"testing"
)

func TestRenderHTML(t *testing.T) {
	html := `<div style="font-family:'Courier New';width:76mm;margin:0 auto;padding:6px">
<h2 style="text-align:center;margin:4px 0">CAFETERÍA CENTRAL</h2>
<div style="text-align:center;border-bottom:2px dashed #000;padding-bottom:6px">Ticket #020 · Para llevar</div>
<table style="width:100%;font-size:13px;margin-top:6px"><tr><td>1x Café au lait</td><td align="right">1,80 €</td></tr>
<tr><td>2x Ñoquis</td><td align="right">9,50 €</td></tr></table>
<h3 style="text-align:right;border-top:2px dashed #000">TOTAL: 11,30 €</h3></div>`
	img, err := renderHTML(html, 576, 288)
	if err != nil {
		t.Fatal(err)
	}
	img = trimBottom(img)
	b := img.Bounds()
	t.Logf("render %dx%d", b.Dx(), b.Dy())
	f, _ := os.Create(`build\render-test.png`)
	defer f.Close()
	_ = png.Encode(f, img)
	data := rasterBytes(toGray(img, 576), false)
	t.Logf("escpos bytes: %d", len(data))
}
