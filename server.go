package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	appName    = "Imprenta Labs"
	appVersion = "2.0.0"
)

var ports = []int{8765, 8766, 8767, 8768, 8769}

var activePort int

func dataDir() string {
	dir := filepath.Join(os.Getenv("APPDATA"), "ImprentaLabs")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// ---------- connections ----------

type wsConn struct {
	c      *websocket.Conn
	wmu    sync.Mutex
	origin string
	nonce  string
	authed bool

	smu  sync.Mutex
	subs map[string]printTarget
}

func (w *wsConn) write(v any) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.c.WriteJSON(v)
}

var conns = struct {
	sync.Mutex
	m map[*wsConn]bool
}{m: map[*wsConn]bool{}}

func broadcast(v map[string]any) {
	conns.Lock()
	list := make([]*wsConn, 0, len(conns.m))
	for c := range conns.m {
		list = append(list, c)
	}
	conns.Unlock()
	for _, c := range list {
		_ = c.write(v)
	}
}

// authorize: signed connections pass; others must be an allowed site.
func (w *wsConn) authorize() error {
	if w.authed {
		return nil
	}
	if originAllowed(w.origin, true) {
		w.authed = true
		return nil
	}
	return fmt.Errorf("sitio no autorizado: %s", w.origin)
}

// ---------- protocol types ----------

type request struct {
	ID     json.RawMessage `json:"id"`
	Call   string          `json:"call"`
	Params json.RawMessage `json:"params"`
}

type rawData struct {
	Type   string `json:"type"`
	Format string `json:"format"` // plain | base64 | hex
	Data   string `json:"data"`
}

type printParams struct {
	Printer printTarget `json:"printer"`
	Data    []rawData   `json:"data"`
	Copies  int         `json:"copies"`
	Job     string      `json:"job"`
}

type ticketParams struct {
	Printer    printTarget `json:"printer"`
	Width      int         `json:"width"`
	WidthDots  int         `json:"widthDots"`
	Lines      []TicketOp  `json:"lines"`
	Logo       bool        `json:"logo"`
	Cut        bool        `json:"cut"`
	OpenDrawer bool        `json:"openDrawer"`
	Copies     int         `json:"copies"`
	Job        string      `json:"job"`
}

type htmlParams struct {
	Printer    printTarget `json:"printer"`
	HTML       string      `json:"html"`
	WidthDots  int         `json:"widthDots"`
	CSSWidth   int         `json:"cssWidth"`
	Dither     bool        `json:"dither"`
	Cut        *bool       `json:"cut"`
	OpenDrawer bool        `json:"openDrawer"`
	Copies     int         `json:"copies"`
	Job        string      `json:"job"`
}

func decode(d rawData) ([]byte, error) {
	switch strings.ToLower(d.Format) {
	case "base64":
		return base64.StdEncoding.DecodeString(d.Data)
	case "hex":
		return hex.DecodeString(strings.ReplaceAll(d.Data, " ", ""))
	default:
		return encodeCP858(d.Data), nil
	}
}

func repeat(data []byte, copies int) []byte {
	if copies <= 1 {
		return data
	}
	out := make([]byte, 0, len(data)*copies)
	for i := 0; i < copies; i++ {
		out = append(out, data...)
	}
	return out
}

func buildTicket(p ticketParams) []byte {
	dots := p.WidthDots
	if dots <= 0 {
		dots = cfg.WidthDots
	}
	b := NewBuilder(p.Width, dots)
	if p.Logo && cfg.Logo != "" {
		if raw, err := os.ReadFile(cfg.Logo); err == nil {
			if img, err := decodeImage(raw); err == nil {
				b.align("center")
				b.Image(img, min(dots, 320), true)
				b.reset()
				b.Feed(1)
			}
		}
	}
	for _, op := range p.Lines {
		b.Apply(op)
	}
	if p.OpenDrawer {
		b.Drawer()
	}
	if p.Cut {
		b.Feed(3)
		b.Cut()
	}
	return b.Bytes()
}

func buildHTML(p htmlParams) ([]byte, error) {
	dots := p.WidthDots
	if dots <= 0 {
		dots = cfg.WidthDots
	}
	img, err := renderHTML(p.HTML, dots, p.CSSWidth)
	if err != nil {
		return nil, err
	}
	img = trimBottom(img)
	b := NewBuilder(0, dots)
	b.Image(img, dots, p.Dither)
	if p.OpenDrawer {
		b.Drawer()
	}
	if p.Cut == nil || *p.Cut {
		b.Feed(3)
		b.Cut()
	}
	return b.Bytes(), nil
}

func handle(w *wsConn, call string, params json.RawMessage) (any, error) {
	switch call {
	case "getVersion":
		return map[string]any{"name": appName, "version": appVersion, "port": activePort,
			"features": []string{"raw", "ticket", "html", "image", "queue", "status", "signed-auth"}}, nil
	case "auth":
		var p struct {
			KeyID     string `json:"keyId"`
			Signature string `json:"signature"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if !verifyAuth(p.KeyID, w.nonce, w.origin, p.Signature) {
			return nil, fmt.Errorf("firma no válida")
		}
		w.authed = true
		return true, nil
	}

	if err := w.authorize(); err != nil {
		return nil, err
	}

	switch call {
	case "printers.find":
		return listPrinters()
	case "printers.getDefault":
		return defaultPrinter()
	case "printers.status":
		var p struct {
			Printer printTarget `json:"printer"`
		}
		_ = json.Unmarshal(params, &p)
		return printerStatus(p.Printer), nil
	case "printers.subscribe":
		var p struct {
			Printers []printTarget `json:"printers"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		w.smu.Lock()
		w.subs = map[string]printTarget{}
		for _, t := range p.Printers {
			w.subs[targetKey(t)] = t
		}
		w.smu.Unlock()
		return true, nil
	case "print":
		var p printParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		var all []byte
		for _, d := range p.Data {
			b, err := decode(d)
			if err != nil {
				return nil, err
			}
			all = append(all, b...)
		}
		return queue.Submit(p.Printer, p.Job, repeat(all, p.Copies)), nil
	case "ticket":
		var p ticketParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return queue.Submit(p.Printer, p.Job, repeat(buildTicket(p), p.Copies)), nil
	case "html":
		var p htmlParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		data, err := buildHTML(p)
		if err != nil {
			return nil, err
		}
		return queue.Submit(p.Printer, p.Job, repeat(data, p.Copies)), nil
	case "openDrawer":
		var p struct {
			Printer printTarget `json:"printer"`
		}
		_ = json.Unmarshal(params, &p)
		b := NewBuilder(0, cfg.WidthDots)
		b.Drawer()
		return queue.Submit(p.Printer, "drawer", b.Bytes()), nil
	case "queue.list":
		return queue.List(), nil
	case "queue.cancel", "queue.retry":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if call == "queue.cancel" {
			return queue.Cancel(p.ID), nil
		}
		return queue.RetryNow(p.ID), nil
	}
	return nil, fmt.Errorf("método desconocido: %s", call)
}

var upgrader = websocket.Upgrader{
	// Authorisation happens per call (signature or allowed site), not here.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func wsHandler(rw http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(rw, r, nil)
	if err != nil {
		return
	}
	w := &wsConn{c: c, origin: r.Header.Get("Origin"), nonce: newNonce()}
	conns.Lock()
	conns.m[w] = true
	conns.Unlock()
	defer func() {
		conns.Lock()
		delete(conns.m, w)
		conns.Unlock()
		c.Close()
	}()
	c.SetReadLimit(32 << 20)
	_ = w.write(map[string]any{"event": "challenge", "nonce": w.nonce, "version": appVersion})
	for {
		var req request
		if err := c.ReadJSON(&req); err != nil {
			return
		}
		res, err := handle(w, req.Call, req.Params)
		out := map[string]any{"id": req.ID}
		if err != nil {
			out["error"] = err.Error()
			log.Printf("%s -> error: %v", req.Call, err)
		} else {
			out["result"] = res
		}
		if w.write(out) != nil {
			return
		}
	}
}

// statusLoop pushes printer.status events to the pages that subscribed.
func statusLoop() {
	last := map[string]string{}
	for range time.Tick(5 * time.Second) {
		conns.Lock()
		type sub struct {
			t     printTarget
			conns []*wsConn
		}
		subs := map[string]*sub{}
		for c := range conns.m {
			c.smu.Lock()
			for k, t := range c.subs {
				if subs[k] == nil {
					subs[k] = &sub{t: t}
				}
				subs[k].conns = append(subs[k].conns, c)
			}
			c.smu.Unlock()
		}
		conns.Unlock()
		for k, s := range subs {
			st := printerStatus(s.t)
			sig := st.State + "|" + st.Warning
			if last[k] == sig {
				continue
			}
			last[k] = sig
			ev := map[string]any{"event": "printer.status", "status": st}
			for _, c := range s.conns {
				_ = c.write(ev)
			}
		}
	}
}

func startServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", wsHandler)
	mux.HandleFunc("/status", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]any{"name": appName, "version": appVersion, "port": activePort})
	})
	queue.emit = broadcast
	for _, p := range ports {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		activePort = p
		go func() { log.Println(http.Serve(ln, mux)) }()
		go statusLoop()
		return nil
	}
	return fmt.Errorf("no hay puertos libres en %v", ports)
}
