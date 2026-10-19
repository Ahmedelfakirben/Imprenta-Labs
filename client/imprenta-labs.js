/**
 * Imprenta Labs — Cliente JavaScript para el ERP / POS
 * Vektor Labs (C) 2026
 *
 * Soporta:
 *  - Impresión silenciosa RAW / ESC-POS
 *  - Impresión de HTML directo renderizado con motor nativo
 *  - Detección de estado de impresora en tiempo real (offline, sin papel, tapa abierta)
 *  - Cola persistente con reintentos
 *  - Autenticación por firma Ed25519 o autorización de origen
 */

const PORTS = [8765, 8766, 8767, 8768, 8769];

export class ImprentaLabs {
  constructor() {
    this.ws = null;
    this.pending = new Map();
    this.seq = 0;
    this.connecting = null;
    this.nonce = null;
    this.versionInfo = null;
    this.listeners = new Set();
  }

  get connected() {
    return !!this.ws && this.ws.readyState === WebSocket.OPEN;
  }

  _open(port) {
    return new Promise((resolve, reject) => {
      let ws;
      try {
        ws = new WebSocket(`ws://127.0.0.1:${port}`);
      } catch (e) {
        return reject(e);
      }
      const timer = setTimeout(() => {
        try { ws.close(); } catch (_) {}
        reject(new Error('timeout'));
      }, 4000);

      ws.onopen = () => {
        clearTimeout(timer);
        resolve(ws);
      };
      ws.onerror = (err) => {
        clearTimeout(timer);
        reject(err || new Error('no disponible'));
      };
    });
  }

  async connect() {
    if (this.connected) return this;
    if (this.connecting) return this.connecting;

    this.connecting = (async () => {
      for (const port of PORTS) {
        try {
          const ws = await this._open(port);
          ws.onmessage = (ev) => {
            try {
              const msg = JSON.parse(ev.data);
              if (msg.event === 'challenge') {
                this.nonce = msg.nonce;
                this.versionInfo = { version: msg.version };
                return;
              }
              if (msg.event === 'printer.status' || msg.event === 'job') {
                this.listeners.forEach((fn) => {
                  try { fn(msg); } catch (err) { console.error(err); }
                });
                return;
              }
              const p = this.pending.get(msg.id);
              if (!p) return;
              this.pending.delete(msg.id);
              if (msg.error) {
                p.reject(new Error(msg.error));
              } else {
                p.resolve(msg.result);
              }
            } catch (err) {
              console.error('Error procesando respuesta WS:', err);
            }
          };

          ws.onclose = () => {
            this.ws = null;
            this.pending.forEach((p) => p.reject(new Error('Conexión cerrada')));
            this.pending.clear();
          };

          this.ws = ws;
          return this;
        } catch (_) {
          // probar siguiente puerto
        }
      }
      throw new Error('Imprenta Labs no está en ejecución en este equipo.');
    })();

    try {
      return await this.connecting;
    } finally {
      this.connecting = null;
    }
  }

  async call(call, params = {}) {
    await this.connect();
    const id = ++this.seq;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      try {
        this.ws.send(JSON.stringify({ id, call, params }));
      } catch (err) {
        this.pending.delete(id);
        reject(err);
      }
    });
  }

  /**
   * Suscribe un listener a eventos en tiempo real (cambios de estado de impresora, progreso de cola)
   */
  onEvent(callback) {
    this.listeners.add(callback);
    return () => this.listeners.delete(callback);
  }

  // --- Información y Pruebas ---
  version() {
    return this.call('getVersion');
  }

  printers() {
    return this.call('printers.find');
  }

  defaultPrinter() {
    return this.call('printers.getDefault');
  }

  /**
   * Consulta el estado de una impresora: 'ok', 'paper_out', 'offline', 'cover_open', etc.
   * printer: "Nombre en Windows" o { host: "192.168.1.50", port: 9100 }
   */
  printerStatus(printer) {
    return this.call('printers.status', { printer });
  }

  /**
   * Suscribe el agente a monitorizar activamente una o más impresoras cada 5s
   */
  subscribeStatus(printers) {
    const list = Array.isArray(printers) ? printers : [printers];
    return this.call('printers.subscribe', { printers: list.map((p) => (typeof p === 'string' ? { name: p } : p)) });
  }

  // --- Impresión ---

  /**
   * Imprime un ticket estructurado en ESC/POS directo (rápido, nítido y con corte)
   */
  printTicket({
    printer,
    lines,
    logo = false,
    width = 48,
    widthDots = 576,
    cut = true,
    openDrawer = false,
    copies = 1,
    job = 'Ticket POS',
  }) {
    return this.call('ticket', {
      printer,
      lines,
      logo,
      width,
      widthDots,
      cut,
      openDrawer,
      copies,
      job,
    });
  }

  /**
   * Imprime HTML renderizado directamente al ancho de papel de la impresora térmica
   * (Sin abrir diálogos del navegador y con corte de papel automático)
   */
  printHTML({
    printer,
    html,
    widthDots = 576,
    cssWidth = 288,
    dither = false,
    cut = true,
    openDrawer = false,
    copies = 1,
    job = 'Ticket HTML',
  }) {
    return this.call('html', {
      printer,
      html,
      widthDots,
      cssWidth,
      dither,
      cut,
      openDrawer,
      copies,
      job,
    });
  }

  /**
   * Envía bytes puros a la impresora (ESC/POS, ZPL para etiquetas, etc.)
   */
  printRaw({ printer, data, format = 'plain', copies = 1, job = 'RAW Job' }) {
    return this.call('print', {
      printer,
      data: [{ type: 'raw', format, data }],
      copies,
      job,
    });
  }

  openDrawer(printer) {
    return this.call('openDrawer', { printer });
  }

  // --- Cola de Impresión ---
  queueList() {
    return this.call('queue.list');
  }

  queueCancel(id) {
    return this.call('queue.cancel', { id });
  }

  queueRetry(id) {
    return this.call('queue.retry', { id });
  }
}

export const imprentaLabs = new ImprentaLabs();
