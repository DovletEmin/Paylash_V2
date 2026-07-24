/* Paylash — Chat WebSocket client.
   Chat is the one feature in this app where the existing polling-only
   approach (see App.startNotifPolling) genuinely isn't good enough — people
   expect near-instant delivery in a conversation. This owns the live
   connection at app-shell lifetime (started/stopped alongside notif
   polling) so the sidebar badge stays current even off the Chat page, and
   ChatPage itself subscribes to the same events for live message append. */
const ChatSocket = {
    _ws: null,
    _reconnectAttempts: 0,
    _reconnectTimer: null,
    _manuallyClosed: false,
    _listeners: {},

    connect() {
        if (this._ws && (this._ws.readyState === WebSocket.OPEN || this._ws.readyState === WebSocket.CONNECTING)) return;
        this._manuallyClosed = false;
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${proto}//${location.host}/api/chat/ws`);
        this._ws = ws;
        ws.onopen = () => { this._reconnectAttempts = 0; };
        ws.onmessage = (ev) => {
            let data;
            try { data = JSON.parse(ev.data); } catch { return; }
            if (data && data.type) this._emit(data.type, data);
        };
        ws.onclose = () => { if (ws === this._ws && !this._manuallyClosed) this._scheduleReconnect(); };
        ws.onerror = () => {};
    },

    disconnect() {
        this._manuallyClosed = true;
        if (this._reconnectTimer) { clearTimeout(this._reconnectTimer); this._reconnectTimer = null; }
        if (this._ws) { this._ws.close(); this._ws = null; }
        this._reconnectAttempts = 0;
    },

    _scheduleReconnect() {
        this._reconnectAttempts++;
        const delay = Math.min(1000 * 2 ** this._reconnectAttempts, 30000);
        this._reconnectTimer = setTimeout(() => this.connect(), delay);
    },

    on(type, fn) {
        (this._listeners[type] = this._listeners[type] || []).push(fn);
    },

    _emit(type, data) {
        (this._listeners[type] || []).forEach(fn => fn(data));
    },

    // Works both in a regular browser tab and inside the Wails desktop
    // shell (WebView2 is Chromium-based, full Notification API support) —
    // no platform-specific code needed either way.
    notify(title, body) {
        if (typeof Notification === 'undefined') return;
        if (Notification.permission === 'granted') {
            new Notification(title, { body });
        } else if (Notification.permission !== 'denied') {
            Notification.requestPermission().then(p => {
                if (p === 'granted') new Notification(title, { body });
            });
        }
    },
};
