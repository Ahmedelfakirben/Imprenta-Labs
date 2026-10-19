package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed assets/icon.ico
var iconICO []byte

var (
	user32       = windows.NewLazySystemDLL("user32.dll")
	kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	pMessageBox  = user32.NewProc("MessageBoxW")
	pCreateMutex = kernel32.NewProc("CreateMutexW")
)

const (
	mbYesNo       = 0x4
	mbIconQuest   = 0x20
	mbSystemModal = 0x1000
	mbTopmost     = 0x40000
	idYes         = 6
)

func askYesNo(title, text string) bool {
	r, _, _ := pMessageBox.Call(0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
		mbYesNo|mbIconQuest|mbSystemModal|mbTopmost)
	return r == idYes
}

func info(title, text string) {
	pMessageBox.Call(0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
		0x40|mbTopmost)
}

func alreadyRunning() bool {
	_, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Global\\ImprentaLabsAgent"))))
	return err == syscall.Errno(183) // ERROR_ALREADY_EXISTS
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("ImprentaLabs")
	return err == nil
}

func setAutostart(on bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if on {
		exe, _ := os.Executable()
		_ = k.SetStringValue("ImprentaLabs", `"`+exe+`"`)
	} else {
		_ = k.DeleteValue("ImprentaLabs")
	}
}

func testTicket() map[string]any {
	ops := []TicketOp{
		{T: "text", Text: "IMPRENTA LABS", Align: "center", Bold: true, Size: 2},
		{T: "text", Text: "Prueba de impresión", Align: "center"},
		{T: "line"},
		{T: "cols", Left: "1x Café con leche", Right: "1,80 €"},
		{T: "cols", Left: "2x Ñoquis a la crema", Right: "9,50 €"},
		{T: "line"},
		{T: "cols", Left: "TOTAL", Right: "11,30 €", Bold: true, Size: 2},
		{T: "feed", N: 1},
		{T: "qr", Data: "https://vektorlabs.com", N: 6},
		{T: "text", Text: "¡Gracias por su visita!", Align: "center"},
	}
	data := buildTicket(ticketParams{Lines: ops, Cut: true})
	return queue.Submit(printTarget{}, "Imprenta Labs test", data)
}

func statusReport() string {
	names, err := listPrinters()
	if err != nil {
		return "No se pudo leer la lista de impresoras: " + err.Error()
	}
	var sb strings.Builder
	for _, n := range names {
		st := windowsStatus(n)
		icon := "✔"
		if st.State != "ok" {
			icon = "✖"
		}
		line := fmt.Sprintf("%s  %s — %s", icon, n, statusText(st))
		if st.State == "ok" {
			line = fmt.Sprintf("%s  %s", icon, n)
		}
		if st.Warning == "paper_low" {
			line += "  (poco papel)"
		}
		sb.WriteString(line + "\n")
	}
	if sb.Len() == 0 {
		return "No hay impresoras instaladas."
	}
	return sb.String()
}

func onReady() {
	systray.SetIcon(iconICO)
	systray.SetTitle(appName)
	systray.SetTooltip(fmt.Sprintf("%s %s — puerto %d", appName, appVersion, activePort))

	head := systray.AddMenuItem(fmt.Sprintf("%s v%s · puerto %d", appName, appVersion, activePort), "")
	head.Disable()
	mQueue := systray.AddMenuItem("Cola de impresión: vacía", "Trabajos pendientes de imprimir")
	mQueue.Disable()
	systray.AddSeparator()
	mTest := systray.AddMenuItem("Imprimir ticket de prueba", "")
	mStatus := systray.AddMenuItem("Estado de las impresoras", "")
	mSites := systray.AddMenuItem("Sitios permitidos", "Clic en un sitio para quitar su permiso")
	none := mSites.AddSubMenuItem("(ninguno todavía)", "")
	none.Disable()
	mAuto := systray.AddMenuItemCheckbox("Iniciar con Windows", "", autostartEnabled())
	mFolder := systray.AddMenuItem("Abrir carpeta de datos", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Salir", "")

	addSite := func(origin string) {
		none.Hide()
		mark := "✖ "
		if origins.get(origin) {
			mark = "✔ "
		}
		it := mSites.AddSubMenuItem(mark+origin, "Clic para quitar este sitio")
		go func() {
			for range it.ClickedCh {
				if askYesNo(appName, "¿Quitar el permiso de:\n\n"+origin+"\n\nVolverá a preguntar la próxima vez que intente imprimir.") {
					origins.remove(origin)
					it.Hide()
					return
				}
			}
		}()
	}
	for _, o := range origins.list() {
		addSite(o)
	}
	origins.onAdd = addSite

	go func() {
		for range time.Tick(3 * time.Second) {
			if n := queue.Pending(); n == 0 {
				mQueue.SetTitle("Cola de impresión: vacía")
			} else {
				mQueue.SetTitle(fmt.Sprintf("Cola de impresión: %d pendiente(s)", n))
			}
		}
	}()

	go func() {
		for {
			select {
			case <-mTest.ClickedCh:
				res := testTicket()
				if res["state"] != "printed" {
					info(appName, fmt.Sprintf("El ticket de prueba quedó en cola.\nMotivo: %v\n\nSe reintentará automáticamente.", res["error"]))
				}
			case <-mStatus.ClickedCh:
				info(appName, statusReport())
			case <-mAuto.ClickedCh:
				if mAuto.Checked() {
					mAuto.Uncheck()
					setAutostart(false)
				} else {
					mAuto.Check()
					setAutostart(true)
				}
			case <-mFolder.ClickedCh:
				_ = exec.Command("explorer.exe", dataDir()).Start()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--genkey" {
		dir, _ := os.Getwd()
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		id, err := genKeys(dir)
		if err != nil {
			info(appName, "Error generando claves: "+err.Error())
			return
		}
		info(appName, fmt.Sprintf("Claves generadas en:\n%s\n\nID de clave: %s\n\n• imprenta-labs-%s.private.pem  → servidor del ERP (SECRETO)\n• imprenta-labs-%s.trusted_keys.json → copiar como trusted_keys.json junto a ImprentaLabs.exe", dir, id, id, id))
		return
	}

	if f, err := os.OpenFile(filepath.Join(dataDir(), "agent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
	if alreadyRunning() {
		return
	}
	loadConfig()
	origins.load()
	loadKeys()
	queue.load()
	queue.Start()
	if err := startServer(); err != nil {
		info(appName, "No se pudo iniciar: "+err.Error())
		return
	}
	log.Printf("%s %s escuchando en 127.0.0.1:%d", appName, appVersion, activePort)
	systray.Run(onReady, func() {})
}
