package main

import (
	"fmt"
	"net"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var pGetPrinter = winspool.NewProc("GetPrinterW")

type PrinterStatus struct {
	Printer string `json:"printer"`
	State   string `json:"state"`   // ok | offline | paper_out | cover_open | paper_jam | paused | error | not_found | unreachable
	Online  bool   `json:"online"`  // true when ok
	Warning string `json:"warning"` // paper_low, ...
	Detail  string `json:"detail"`
	Raw     uint32 `json:"raw,omitempty"`
}

type printerInfo2 struct {
	serverName, printerName, shareName, portName, driverName, comment, location *uint16
	devMode                                                                     uintptr
	sepFile, printProcessor, datatype, parameters                               *uint16
	secDesc                                                                     uintptr
	attributes, priority, defaultPriority, startTime, untilTime                 uint32
	status, cJobs, averagePPM                                                   uint32
}

func windowsStatus(name string) PrinterStatus {
	st := PrinterStatus{Printer: name, State: "ok", Online: true}
	pn, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return PrinterStatus{Printer: name, State: "not_found", Detail: err.Error()}
	}
	var h windows.Handle
	r, _, e := pOpenPrinter.Call(uintptr(unsafe.Pointer(pn)), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return PrinterStatus{Printer: name, State: "not_found", Detail: fmt.Sprintf("impresora no encontrada: %v", e)}
	}
	defer pClosePrinter.Call(uintptr(h))

	var needed uint32
	pGetPrinter.Call(uintptr(h), 2, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return st
	}
	buf := make([]byte, needed)
	r, _, e = pGetPrinter.Call(uintptr(h), 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)))
	if r == 0 {
		st.Detail = fmt.Sprintf("GetPrinter: %v", e)
		return st
	}
	info := (*printerInfo2)(unsafe.Pointer(&buf[0]))
	s, attr := info.status, info.attributes
	st.Raw = s
	switch {
	case s&0x80 != 0 || attr&0x400 != 0 || s&0x1000 != 0:
		st.State, st.Detail = "offline", "impresora desconectada o apagada"
	case s&0x10 != 0 || s&0x40 != 0:
		st.State, st.Detail = "paper_out", "sin papel"
	case s&0x400000 != 0:
		st.State, st.Detail = "cover_open", "tapa abierta"
	case s&0x8 != 0:
		st.State, st.Detail = "paper_jam", "atasco de papel"
	case s&0x1 != 0:
		st.State, st.Detail = "paused", "impresora en pausa"
	case s&0x2 != 0 || s&0x100000 != 0:
		st.State, st.Detail = "error", "la impresora indica un error"
	}
	if s&0x20000 != 0 {
		st.Warning = "paper_low"
	}
	st.Online = st.State == "ok"
	return st
}

// networkStatus talks to the printer through the real-time ESC/POS status
// commands (DLE EOT n) on top of the TCP connection.
func networkStatus(host string, port int) PrinterStatus {
	if port == 0 {
		port = 9100
	}
	label := net.JoinHostPort(host, fmt.Sprint(port))
	st := PrinterStatus{Printer: label, State: "ok", Online: true}
	conn, err := net.DialTimeout("tcp", label, 2*time.Second)
	if err != nil {
		return PrinterStatus{Printer: label, State: "unreachable", Detail: "no responde en la red"}
	}
	defer conn.Close()
	query := func(n byte) (byte, bool) {
		_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
		if _, err := conn.Write([]byte{0x10, 0x04, n}); err != nil {
			return 0, false
		}
		b := make([]byte, 1)
		if _, err := conn.Read(b); err != nil {
			return 0, false
		}
		return b[0], true
	}
	v1, ok := query(1)
	if !ok {
		st.Detail = "conectada (sin información de estado)"
		return st
	}
	if v1&0x08 != 0 {
		st.State, st.Detail = "offline", "impresora fuera de línea"
	}
	if v2, ok := query(2); ok {
		switch {
		case v2&0x04 != 0:
			st.State, st.Detail = "cover_open", "tapa abierta"
		case v2&0x20 != 0:
			st.State, st.Detail = "paper_out", "sin papel"
		case v2&0x40 != 0:
			st.State, st.Detail = "error", "error de la impresora"
		}
	}
	if v4, ok := query(4); ok {
		if v4&0x60 != 0 {
			st.State, st.Detail = "paper_out", "sin papel"
		} else if v4&0x0C != 0 {
			st.Warning = "paper_low"
		}
	}
	st.Online = st.State == "ok"
	return st
}

func printerStatus(t printTarget) PrinterStatus {
	if t.Host != "" {
		return networkStatus(t.Host, t.Port)
	}
	name := t.Name
	if name == "" {
		var err error
		if name, err = defaultPrinter(); err != nil {
			return PrinterStatus{State: "not_found", Detail: err.Error()}
		}
	}
	return windowsStatus(name)
}

func statusText(st PrinterStatus) string {
	if st.Detail != "" {
		return st.Detail
	}
	return strings.ReplaceAll(st.State, "_", " ")
}

// blocking states must not receive a job; the queue will retry later.
func blocking(st PrinterStatus) bool {
	switch st.State {
	case "ok":
		return false
	}
	return true
}
