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
    _audioCtx: null,

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

    // A native OS notification — used when the tab is hidden/backgrounded
    // (see App._bindChatSocketListeners, which picks between this and
    // showInAppToast based on document focus). Works both in a regular
    // browser tab and inside the Wails desktop shell (WebView2 is
    // Chromium-based, full Notification API support) — no platform-specific
    // code needed either way. tag+renotify means a second message in the
    // same conversation replaces the earlier toast instead of piling up,
    // while still re-alerting. Clicking it focuses the window and jumps
    // straight to that conversation.
    notify({ title, body, conversationId }) {
        if (typeof Notification === 'undefined') return;
        const fire = () => {
            const n = new Notification(title, {
                body,
                icon: '/img/icon.png',
                tag: conversationId ? 'chat-conv-' + conversationId : undefined,
                renotify: !!conversationId,
            });
            n.onclick = () => {
                window.focus();
                if (typeof App !== 'undefined') App.navigate('chat');
                if (typeof ChatPage !== 'undefined' && conversationId) ChatPage.selectConversation(conversationId);
                n.close();
            };
        };
        if (Notification.permission === 'granted') {
            fire();
        } else if (Notification.permission !== 'denied') {
            Notification.requestPermission().then(p => { if (p === 'granted') fire(); });
        }
    },

    // A short, synthesized two-note chime (Web Audio API, no sound file) —
    // instant, license-free, and works fully offline. Gated by the caller on
    // App.user.chat_notify_sound.
    playChime() {
        try {
            const Ctx = window.AudioContext || window.webkitAudioContext;
            if (!Ctx) return;
            const ctx = this._audioCtx || (this._audioCtx = new Ctx());
            const now = ctx.currentTime;
            [[880, 0], [1174.66, 0.09]].forEach(([freq, delay]) => {
                const osc = ctx.createOscillator();
                const gain = ctx.createGain();
                osc.type = 'sine';
                osc.frequency.value = freq;
                gain.gain.setValueAtTime(0, now + delay);
                gain.gain.linearRampToValueAtTime(0.18, now + delay + 0.012);
                gain.gain.exponentialRampToValueAtTime(0.001, now + delay + 0.22);
                osc.connect(gain).connect(ctx.destination);
                osc.start(now + delay);
                osc.stop(now + delay + 0.24);
            });
        } catch { /* best-effort — a missing/blocked AudioContext just means no sound */ }
    },

    // A rich in-app toast (avatar, sender/text per the caller's privacy
    // choice, click-to-jump) — used instead of the native notify() while the
    // tab itself is focused, where a native OS toast reads as out of place.
    showInAppToast({ avatarUserId, avatarName, title, body, conversationId }) {
        const stack = document.getElementById('chat-toast-stack');
        if (!stack) return;
        const el = document.createElement('div');
        el.className = 'chat-toast';
        el.innerHTML = `
            ${UI.avatarHTML(avatarUserId, avatarName, 'chat-toast-avatar')}
            <div class="chat-toast-body">
                <div class="chat-toast-title">${UI.esc(title)}</div>
                <div class="chat-toast-text">${UI.esc(body)}</div>
            </div>`;
        const dismiss = () => {
            el.classList.add('chat-toast-out');
            setTimeout(() => el.remove(), 220);
        };
        el.onclick = () => {
            if (typeof App !== 'undefined') App.navigate('chat');
            if (typeof ChatPage !== 'undefined' && conversationId) ChatPage.selectConversation(conversationId);
            dismiss();
        };
        stack.appendChild(el);
        setTimeout(dismiss, 5000);
    },
};
