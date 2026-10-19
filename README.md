# 🖨️ Imprenta Labs

<p align="center">
  <img src="assets/logo.png" width="160" alt="Imprenta Labs Logo" />
</p>

<p align="center">
  <strong>Agente nativo ultraligero de impresión directa y silenciosa para ERPs, TPVs y aplicaciones Web.</strong><br>
  <em>Desarrollado por Vektor Labs</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-2.0.0-orange.svg" alt="Version 2.0.0" />
  <img src="https://img.shields.io/badge/go-1.27-blue.svg" alt="Go Version" />
  <img src="https://img.shields.io/badge/platform-Windows_x64-0078D6.svg" alt="Windows" />
  <img src="https://img.shields.io/badge/RAM-~14MB-green.svg" alt="Low RAM" />
  <img src="https://img.shields.io/badge/license-MIT-lightgrey.svg" alt="License" />
</p>

---

## 🌟 ¿Qué es Imprenta Labs?

**Imprenta Labs** es un puente (bridge) de comunicación local entre cualquier aplicación web (ERP, TPV, ecommerce, sistema de comandas) y el hardware físico de impresión.

Permite **imprimir silenciosamente en fracciones de segundo**, abrir el cajón portamonedas y cortar papel sin cuadros de diálogo del navegador ni dependencias pesadas como Java o runtimes externos.

### 📊 Comparativa frente a QZ Tray

| Característica | QZ Tray | Imprenta Labs |
| :--- | :--- | :--- |
| **Consumo de Memoria RAM** | ~150 MB - 250 MB | **~14 MB** *(90% menos)* |
| **Tamaño del Instalador** | ~104 MB | **4.59 MB** |
| **Dependencias externas** | OpenJDK + JavaFX (~300 MB en disco) | **Cero dependencias** (binario nativo independiente) |
| **Tiempo de arranque** | 5 a 8 segundos | **< 50 milisegundos** |
| **Motor de renderizado HTML** | JavaFX WebKit (DLLs de 67 MB) | **Motor nativo Edge headless** (incluido en Windows) |
| **Cola de impresión** | Se pierde si la impresora está apagada | **Cola persistente en disco con reintentos** |
| **Detección de estado** | Limitada | **Tiempo real** (sin papel, tapa abierta, offline) |
| **Avisos / Licenciamiento** | Diálogos de advertencia o pago | **Totalmente libre y personalizable** |

---

## 🚀 Características Principales

1. **Impresión RAW Ultrarrápida (ESC/POS & ZPL):** Comunicación directa con el spooler de Windows (`winspool.drv`) o impresoras de red Ethernet/Wi-Fi (puerto 9100).
2. **Renderizado de HTML a Ticket Térmico:** Envía fragmentos de HTML/CSS y el agente los renderiza vectorialmente a 58mm o 80mm preservando fuentes, negritas, bordes punteados y tablas.
3. **Corte y Cajón Portamonedas:** Comandos automáticos de corte total/parcial y apertura de cajón (`ESC p`).
4. **Cola de Impresión Persistente con Reintentos:** Si una impresora se queda sin papel o se desconecta, los tickets se guardan en disco (`%APPDATA%\ImprentaLabs\queue`) y se imprimen solos tan pronto como se repone el papel.
5. **Detección de Hardware en Tiempo Real:** Reporta el estado de cada impresora (`ok`, `paper_out`, `cover_open`, `paper_low`, `offline`, `paused`).
6. **Bandeja de Sistema Inteligente:**
   - Impresión de ticket de prueba.
   - Monitor de impresoras conectadas.
   - Gestión visual de sitios web autorizados (clic para revocar acceso sin editar archivos).
   - Arranque automático con Windows.
7. **Seguridad Robusta:** Soporta autorización mediante whitelist de dominios y autenticación criptográfica silenciosa con firmas Ed25519.

---

## 💻 Guía de Integración para Desarrolladores

### 1. Incluir el conector en tu Frontend

Puedes importar el conector oficial en **JavaScript / TypeScript**:

```typescript
import { imprentaLabs } from './client/imprenta-labs.js';
// o en TypeScript:
import { imprentaLabs } from './lib/imprentaLabs';
```

### 2. Conexión y consulta de impresoras

```javascript
// Conectar con el agente local
await imprentaLabs.connect();

// Obtener lista de impresoras instaladas en Windows
const printers = await imprentaLabs.printers();
console.log('Impresoras disponibles:', printers);
// -> ["printer WD8260", "POS-90", "Microsoft Print to PDF"]

// Obtener la impresora predeterminada
const defaultPrinter = await imprentaLabs.defaultPrinter();

// Comprobar estado de la impresora (papel, encendido)
const status = await imprentaLabs.printerStatus('printer WD8260');
if (status.state === 'paper_out') {
  alert('¡La impresora se ha quedado sin papel!');
}
```

### 3. Imprimir Tickets Estructurados (ESC/POS)

Es el formato más rápido y nítido para impresoras de tickets térmicos:

```javascript
await imprentaLabs.printTicket({
  printer: 'printer WD8260', // O dejar vacío para usar la predeterminada
  cut: true,                 // Cortar papel al terminar
  openDrawer: true,          // Abrir cajón portamonedas
  lines: [
    { t: 'text', text: 'MI COMERCIO S.L.', align: 'center', bold: true, size: 2 },
    { t: 'text', text: 'Calle Mayor 12 - Madrid', align: 'center' },
    { t: 'line', char: '=' },
    { t: 'cols', left: '1x Café con leche', right: '1.80 €' },
    { t: 'cols', left: '1x Croissant artesano', right: '2.20 €' },
    { t: 'line', char: '-' },
    { t: 'cols', left: 'TOTAL:', right: '4.00 €', bold: true, size: 2 },
    { t: 'feed', n: 1 },
    { t: 'qr', data: 'https://miempresa.com/factura/1234', n: 6, align: 'center' },
    { t: 'text', text: '¡Gracias por su visita!', align: 'center' }
  ]
});
```

### 4. Imprimir HTML Directamente

Ideal para reutilizar diseños existentes sin convertir a comandos térmicos manuales:

```javascript
const htmlTicket = `
  <div style="font-family:'Courier New', monospace; width:76mm; padding:5px;">
    <h2 style="text-align:center;">COMANDA COCINA</h2>
    <hr style="border-top:1px dashed #000;">
    <p><strong>Mesa 4</strong> - Pedido #042</p>
    <p>1x Hamburguesa Especial (Sin cebolla)</p>
    <p>1x Patatas Rústicas</p>
  </div>
`;

await imprentaLabs.printHTML({
  printer: 'printer WD8260',
  html: htmlTicket,
  widthDots: 576, // 576 para 80mm, 384 para 58mm
  cut: true
});
```

### 5. Impresión a Impresoras de Red (Ethernet / Wi-Fi)

Puedes imprimir en impresoras remotas (por ejemplo en cocina o barra) sin instalar drivers en Windows:

```javascript
await imprentaLabs.printTicket({
  printer: { host: '192.168.1.150', port: 9100 },
  cut: true,
  lines: [
    { t: 'text', text: 'COMANDA BARRA', align: 'center', bold: true, size: 2 },
    { t: 'cols', left: '2x Cerveza Mahou', right: '5.00 €' }
  ]
});
```

### 6. Monitorizar eventos en tiempo real

El agente emite eventos cuando cambia el estado de una impresora o progresa un trabajo de la cola:

```javascript
// Suscribirse a cambios de estado de impresoras
await imprentaLabs.subscribeStatus(['printer WD8260', 'POS-90']);

imprentaLabs.onEvent((event) => {
  if (event.event === 'printer.status') {
    console.log(`Estado de ${event.status.printer}:`, event.status.state);
  }
  if (event.event === 'job') {
    console.log(`Trabajo ${event.id}:`, event.state); // 'queued', 'printed', 'failed'
  }
});
```

---

## 📡 Protocolo WebSocket (JSON-RPC)

El agente escucha en `ws://127.0.0.1:8765` (con fallback dinámico a `8766-8769`).

### Estructura de peticiones
```json
{
  "id": 1,
  "call": "ticket",
  "params": { ... }
}
```

### Métodos disponibles

| Método | Parámetros | Descripción |
| :--- | :--- | :--- |
| `getVersion` | `{}` | Información de versión y capacidades activas |
| `printers.find` | `{}` | Lista todas las impresoras instaladas en Windows |
| `printers.getDefault`| `{}` | Devuelve el nombre de la impresora predeterminada |
| `printers.status` | `{ "printer": "Nombre" }` | Estado físico detallado de la impresora |
| `printers.subscribe` | `{ "printers": [...] }` | Activa la monitorización periódica de impresoras |
| `ticket` | `{ "printer": ..., "lines": [...] }` | Imprime ticket formateado ESC/POS |
| `html` | `{ "printer": ..., "html": "..." }` | Renderiza e imprime código HTML |
| `print` | `{ "printer": ..., "data": [...] }` | Envía bytes RAW (plain, base64 o hex) |
| `openDrawer` | `{ "printer": ... }` | Envía pulso de apertura al cajón portamonedas |
| `queue.list` | `{}` | Lista los trabajos actuales e históricos de la cola |
| `queue.cancel` | `{ "id": "..." }` | Cancela un trabajo en cola |
| `queue.retry` | `{ "id": "..." }` | Fuerza el reintento inmediato de un trabajo |

---

## 🛠️ Compilación y Creación del Instalador

### Requisitos previos
- **Go 1.22+**: `winget install GoLang.Go`
- **Inno Setup 6**: `winget install JRSoftware.InnoSetup`
- **Node.js** (opcional, para ejecutar tests)

### 1. Compilar el ejecutable (.exe)
```powershell
# Instalar generador de recursos de icono
go run github.com/akavel/rsrc@latest -ico assets\icon.ico -o rsrc_windows_amd64.syso

# Compilar sin consola negra (-H=windowsgui) y optimizado (-s -w)
go build -ldflags "-s -w -H=windowsgui" -o ImprentaLabs.exe .
```

### 2. Generar el instalador profesional
```powershell
& "C:\Users\<Usuario>\AppData\Local\Programs\Inno Setup 6\ISCC.exe" installer\ImprentaLabs.iss
```
El instalador generado se guardará en `installer\Output\ImprentaLabs-Setup-2.0.0.exe`.

---

## 🔒 Seguridad y Permisos

1. **Permiso visual en el primer uso:** Cuando un nuevo sitio web intenta conectarse vía WebSocket, Windows muestra un diálogo interactivo pidiendo autorización. Al aceptar, el sitio se guarda en `%APPDATA%\ImprentaLabs\origins.json` y ya no vuelve a preguntar.
2. **Pre-autorización en el instalador:** Al instalar en los puestos de clientes, el asistente permite indicar el dominio de tu ERP (ej. `https://erp.tuempresa.com` o comodín `https://*.tuempresa.com`).
3. **Firma criptográfica con claves Ed25519:** Para entornos de alta seguridad, puedes generar claves con `ImprentaLabs.exe --genkey`. Tu servidor ERP firma un nonce de conexión y el agente valida la firma sin necesidad de interacción del usuario.

---

## 📄 Licencia

Este proyecto está bajo la licencia **MIT**. Desarrollado con ❤️ para el ecosistema **Vektor Labs**.
