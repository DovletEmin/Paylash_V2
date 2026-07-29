/* Paýlaş — Web Push client.
   Registers the service worker and (once notification permission is granted)
   subscribes this browser to push, handing the subscription to the server so
   it can deliver a notification while the app is fully closed. Everything here
   is best-effort and feature-detected: no service worker, no PushManager, no
   VAPID key from the server (push disabled), or a denied permission all just
   result in a quiet no-op — the in-app and native (tab-open) notifications are
   unaffected. */
const Push = {
    _reg: null,

    async init() {
        if (!('serviceWorker' in navigator)) return;
        try {
            this._reg = await navigator.serviceWorker.register('/sw.js');
        } catch (e) {
            return; // registration blocked (e.g. insecure context) — give up silently
        }
        navigator.serviceWorker.addEventListener('message', (e) => {
            if (!e.data || e.data.type !== 'push-navigate') return;
            if (e.data.conversation_id) {
                if (typeof App !== 'undefined') App.navigate('chat');
                if (typeof ChatPage !== 'undefined') ChatPage.selectConversation(e.data.conversation_id);
            } else if (e.data.url && typeof App !== 'undefined') {
                // e.data.url is a full path+hash (e.g. "/#shared", usable
                // directly by the service worker's clients.openWindow for a
                // cold start) — location.hash wants just the fragment, or
                // assigning the leading "/" along with it produces "#/#shared"
                // instead of "#shared".
                const i = e.data.url.indexOf('#');
                location.hash = i >= 0 ? e.data.url.slice(i) : e.data.url;
                // Reuse the same hash-routing _handlePushHash already does
                // for a cold-started app, rather than duplicating it here.
                App._handlePushHash();
            }
        });
        this.subscribeIfPermitted();
    },

    subscribeIfPermitted() {
        if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
            this.subscribe();
        }
    },

    async subscribe() {
        if (!this._reg || !('PushManager' in window)) return;
        const vapid = (typeof App !== 'undefined' && App.config && App.config.vapid_public_key) || '';
        if (!vapid) return; // server has push disabled (no VAPID key)
        try {
            let sub = await this._reg.pushManager.getSubscription();
            if (!sub) {
                sub = await this._reg.pushManager.subscribe({
                    userVisibleOnly: true,
                    applicationServerKey: this._urlB64ToUint8Array(vapid),
                });
            }
            const json = sub.toJSON();
            await API.push.subscribe({ endpoint: sub.endpoint, keys: { p256dh: json.keys.p256dh, auth: json.keys.auth } });
        } catch (e) {
            /* push unavailable / permission race — ignore */
        }
    },

    // Drop this browser's subscription — called on explicit logout so pushes
    // for the previous user don't keep arriving here.
    async unsubscribe() {
        if (!this._reg) return;
        try {
            const sub = await this._reg.pushManager.getSubscription();
            if (sub) {
                await API.push.unsubscribe({ endpoint: sub.endpoint }).catch(() => {});
                await sub.unsubscribe().catch(() => {});
            }
        } catch (e) { /* ignore */ }
    },

    _urlB64ToUint8Array(base64) {
        const padding = '='.repeat((4 - (base64.length % 4)) % 4);
        const b64 = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/');
        const raw = atob(b64);
        const arr = new Uint8Array(raw.length);
        for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
        return arr;
    },
};
