package main

import (
	"errors"
	"fmt"
	"net"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winspool      = windows.NewLazySystemDLL("winspool.drv")
	pEnumPrinters = winspool.NewProc("EnumPrintersW")
	pGetDefault   = winspool.NewProc("GetDefaultPrinterW")
	pOpenPrinter  = winspool.NewProc("OpenPrinterW")
	pClosePrinter = winspool.NewProc("ClosePrinter")
	pStartDoc     = winspool.NewProc("StartDocPrinterW")
	pEndDoc       = winspool.NewProc("EndDocPrinter")
	pStartPage    = winspool.NewProc("StartPagePrinter")
	pEndPage      = winspool.NewProc("EndPagePrinter")
	pWritePrinter = winspool.NewProc("WritePrinter")
)

const (
	printerEnumLocal       = 0x2
	printerEnumConnections = 0x4
)

type printerInfo4 struct {
	name       *uint16
	serverName *uint16
	attributes uint32
}

type docInfo1 struct {
	docName    *uint16
	outputFile *uint16
	datatype   *uint16
}

func listPrinters() ([]string, error) {
	var needed, returned uint32
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	pEnumPrinters.Call(flags, 0, 4, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return []string{}, nil
	}
	buf := make([]byte, needed)
	r, _, err := pEnumPrinters.Call(flags, 0, 4,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r == 0 {
		return nil, fmt.Errorf("EnumPrinters: %w", err)
	}
	size := unsafe.Sizeof(printerInfo4{})
	names := make([]string, 0, returned)
	for i := uintptr(0); i < uintptr(returned); i++ {
		p := (*printerInfo4)(unsafe.Pointer(&buf[i*size]))
		names = append(names, windows.UTF16PtrToString(p.name))
	}
	return names, nil
}

func defaultPrinter() (string, error) {
	var size uint32
	pGetDefault.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return "", errors.New("no hay impresora predeterminada")
	}
	buf := make([]uint16, size)
	r, _, err := pGetDefault.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", fmt.Errorf("GetDefaultPrinter: %w", err)
	}
	return windows.UTF16ToString(buf), nil
}

// rawPrintWindows sends raw bytes to an installed Windows printer through the spooler.
func rawPrintWindows(printer, jobName string, data []byte) error {
	if printer == "" {
		var err error
		if printer, err = defaultPrinter(); err != nil {
			return err
		}
	}
	name, err := windows.UTF16PtrFromString(printer)
	if err != nil {
		return err
	}
	var h windows.Handle
	r, _, e := pOpenPrinter.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return fmt.Errorf("no se puede abrir la impresora %q: %w", printer, e)
	}
	defer pClosePrinter.Call(uintptr(h))

	doc := docInfo1{
		docName:  windows.StringToUTF16Ptr(jobName),
		datatype: windows.StringToUTF16Ptr("RAW"),
	}
	r, _, e = pStartDoc.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&doc)))
	if r == 0 {
		return fmt.Errorf("StartDocPrinter: %w", e)
	}
	defer pEndDoc.Call(uintptr(h))

	if r, _, e = pStartPage.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("StartPagePrinter: %w", e)
	}
	defer pEndPage.Call(uintptr(h))

	var written uint32
	if len(data) == 0 {
		return nil
	}
	r, _, e = pWritePrinter.Call(uintptr(h), uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	if r == 0 {
		return fmt.Errorf("WritePrinter: %w", e)
	}
	if int(written) != len(data) {
		return fmt.Errorf("escritos %d de %d bytes", written, len(data))
	}
	return nil
}

// rawPrintNetwork sends raw bytes straight to a network printer (JetDirect, port 9100).
func rawPrintNetwork(host string, port int, data []byte) error {
	if port == 0 {
		port = 9100
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return fmt.Errorf("no se puede conectar a %s: %w", addr, err)
	}
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Write(data)
	return err
}
