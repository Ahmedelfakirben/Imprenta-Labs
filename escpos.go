package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"strings"
	"unicode/utf8"
)

// cp858 maps Unicode runes to the PC858 (Latin-1 + Euro) code page,
// the one used by almost every ESC/POS thermal printer in Europe.
var cp858 = map[rune]byte{
	'Ç': 0x80, 'ü': 0x81, 'é': 0x82, 'â': 0x83, 'ä': 0x84, 'à': 0x85, 'å': 0x86, 'ç': 0x87,
	'ê': 0x88, 'ë': 0x89, 'è': 0x8A, 'ï': 0x8B, 'î': 0x8C, 'ì': 0x8D, 'Ä': 0x8E, 'Å': 0x8F,
	'É': 0x90, 'æ': 0x91, 'Æ': 0x92, 'ô': 0x93, 'ö': 0x94, 'ò': 0x95, 'û': 0x96, 'ù': 0x97,
	'ÿ': 0x98, 'Ö': 0x99, 'Ü': 0x9A, 'ø': 0x9B, '£': 0x9C, 'Ø': 0x9D, '×': 0x9E, 'ƒ': 0x9F,
	'á': 0xA0, 'í': 0xA1, 'ó': 0xA2, 'ú': 0xA3, 'ñ': 0xA4, 'Ñ': 0xA5, 'ª': 0xA6, 'º': 0xA7,
	'¿': 0xA8, '®': 0xA9, '¬': 0xAA, '½': 0xAB, '¼': 0xAC, '¡': 0xAD, '«': 0xAE, '»': 0xAF,
	'Á': 0xB5, 'Â': 0xB6, 'À': 0xB7, '©': 0xB8, 'ã': 0xC6, 'Ã': 0xC7, 'Ê': 0xD2, 'Ë': 0xD3,
	'È': 0xD4, '€': 0xD5, 'Í': 0xD6, 'Î': 0xD7, 'Ï': 0xD8, 'Ó': 0xE0, 'ß': 0xE1, 'Ô': 0xE2,
	'Ò': 0xE3, 'õ': 0xE4, 'Õ': 0xE5, 'µ': 0xE6, 'Ú': 0xE9, 'Û': 0xEA, 'Ù': 0xEB, 'ý': 0xEC,
	'Ý': 0xED, '═': '=', '─': '-', '–': '-', '—': '-', '’': '\'', '‘': '\'', '“': '"', '”': '"',
}

func encodeCP858(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		default:
			if b, ok := cp858[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// TicketOp is one instruction of a structured ticket.
type TicketOp struct {
	T         string `json:"t"` // text, cols, line, feed, qr, barcode, cut, drawer
	Text      string `json:"text,omitempty"`
	Left      string `json:"left,omitempty"`
	Right     string `json:"right,omitempty"`
	Align     string `json:"align,omitempty"` // left, center, right
	Bold      bool   `json:"bold,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Size      int    `json:"size,omitempty"`   // 1 (normal), 2 (double), 3 (triple)...
	N         int    `json:"n,omitempty"`      // feed lines / qr module size
	Data      string `json:"data,omitempty"`   // qr / barcode payload
	Char      string `json:"char,omitempty"`   // line char
	Dither    bool   `json:"dither,omitempty"` // image: dither instead of threshold
}

type Builder struct {
	buf   bytes.Buffer
	width int // characters per line
	dots  int // paper width in dots (576 = 80 mm, 384 = 58 mm)
}

func NewBuilder(width, dots int) *Builder {
	if dots <= 0 {
		dots = 576
	}
	if width <= 0 {
		width = dots / 12
	}
	b := &Builder{width: width, dots: dots}
	b.buf.Write([]byte{0x1B, 0x40})     // ESC @  init
	b.buf.Write([]byte{0x1B, 0x74, 19}) // ESC t 19 -> PC858
	return b
}

func (b *Builder) Bytes() []byte { return b.buf.Bytes() }

func (b *Builder) align(a string) {
	n := byte(0)
	switch a {
	case "center":
		n = 1
	case "right":
		n = 2
	}
	b.buf.Write([]byte{0x1B, 0x61, n})
}

func (b *Builder) style(bold, underline bool, size int) {
	if size < 1 {
		size = 1
	}
	if size > 8 {
		size = 8
	}
	s := byte((size-1)<<4 | (size - 1))
	b.buf.Write([]byte{0x1D, 0x21, s})
	b.buf.Write([]byte{0x1B, 0x45, boolByte(bold)})
	b.buf.Write([]byte{0x1B, 0x2D, boolByte(underline)})
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func (b *Builder) reset() {
	b.style(false, false, 1)
	b.align("left")
}

func (b *Builder) Text(s string) {
	b.buf.Write(encodeCP858(s))
	b.buf.WriteByte('\n')
}

func (b *Builder) Feed(n int) {
	if n < 1 {
		n = 1
	}
	b.buf.Write([]byte{0x1B, 0x64, byte(n)})
}

func (b *Builder) Cut() {
	b.buf.Write([]byte{0x1D, 0x56, 0x42, 0x03}) // GS V 66 3 : feed + partial cut
}

func (b *Builder) Drawer() {
	b.buf.Write([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) // ESC p 0 25 250
}

func (b *Builder) QR(data string, size int) {
	if size < 1 || size > 16 {
		size = 6
	}
	d := []byte(data)
	n := len(d) + 3
	b.buf.Write([]byte{0x1D, 0x28, 0x6B, 0x04, 0x00, 0x31, 0x41, 0x32, 0x00}) // model 2
	b.buf.Write([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x43, byte(size)}) // module size
	b.buf.Write([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x45, 0x31})       // error correction M
	b.buf.Write([]byte{0x1D, 0x28, 0x6B, byte(n & 0xFF), byte(n >> 8), 0x31, 0x50, 0x30})
	b.buf.Write(d)
	b.buf.Write([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x51, 0x30}) // print
}

func (b *Builder) Barcode(data string) {
	b.buf.Write([]byte{0x1D, 0x68, 80})                                  // height
	b.buf.Write([]byte{0x1D, 0x77, 2})                                   // width
	b.buf.Write([]byte{0x1D, 0x48, 2})                                   // HRI below
	b.buf.Write([]byte{0x1D, 0x6B, 73, byte(len(data) + 2), 0x7B, 0x42}) // CODE128 set B
	b.buf.WriteString(data)
}

// Image prints a bitmap, never wider than the paper and never upscaled.
func (b *Builder) Image(img image.Image, dots int, dither bool) {
	natural := img.Bounds().Dx()
	if dots <= 0 || dots > b.dots {
		dots = b.dots
	}
	if natural < dots {
		dots = natural
	}
	b.buf.Write(rasterBytes(toGray(img, dots), dither))
}

func (b *Builder) Apply(op TicketOp) {
	size := op.Size
	if size == 0 {
		size = 1
	}
	switch op.T {
	case "text", "":
		b.align(op.Align)
		b.style(op.Bold, op.Underline, size)
		for _, ln := range strings.Split(op.Text, "\n") {
			b.Text(ln)
		}
		b.reset()
	case "cols":
		b.align("left")
		b.style(op.Bold, op.Underline, size)
		w := b.width / size
		left, right := op.Left, op.Right
		rl := utf8.RuneCountInString(right)
		ll := utf8.RuneCountInString(left)
		if ll+rl+1 > w {
			keep := w - rl - 1
			if keep < 1 {
				keep = 1
			}
			left = string([]rune(left)[:min(keep, ll)])
			ll = utf8.RuneCountInString(left)
		}
		b.Text(left + strings.Repeat(" ", max(1, w-ll-rl)) + right)
		b.reset()
	case "line":
		ch := op.Char
		if ch == "" {
			ch = "-"
		}
		b.align("left")
		b.Text(strings.Repeat(string([]rune(ch)[0]), b.width))
	case "feed":
		b.Feed(op.N)
	case "qr":
		b.align(op.Align)
		if op.Align == "" {
			b.align("center")
		}
		b.QR(op.Data, op.N)
		b.reset()
	case "barcode":
		b.align("center")
		b.Barcode(op.Data)
		b.Text("")
		b.reset()
	case "image":
		data := op.Data
		if i := strings.Index(data, ","); i >= 0 && strings.HasPrefix(data, "data:") {
			data = data[i+1:]
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return
		}
		img, err := decodeImage(raw)
		if err != nil {
			return
		}
		if op.Align == "" {
			b.align("center")
		} else {
			b.align(op.Align)
		}
		b.Image(img, op.N, op.Dither)
		b.reset()
	case "cut":
		b.Cut()
	case "drawer":
		b.Drawer()
	}
}
