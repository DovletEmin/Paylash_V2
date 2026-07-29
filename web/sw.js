/* Paýlaş service worker — Web Push.
   Served from /sw.js so its scope is the whole app. Its only job is to show a
   notification when a push arrives (even with every tab closed) and to focus /
   open the app on the right conversation when one is clicked. */

self.addEventListener('push', (event) => {
    let data = {};
    try { data = event.data ? event.data.json() : {}; } catch (e) { data = {}; }
    const title = data.title || 'Paýlaş';
    const options = {
        body: data.body || '',
        icon: '/img/icon.png',
        badge: '/img/icon.png',
        tag: data.conversation_id ? 'chat-conv-' + data.conversation_id : undefined,
        renotify: !!data.conversation_id,
        // url is the generic click target for anything that isn't a chat
        // message (shares, file comments — see pushShareNotification/
        // pushCommentNotification server-side); conversation_id keeps its
        // own dedicated field since chat's cold-boot handling
        // (_handlePushHash) predates and still expects it specifically.
        data: { conversation_id: data.conversation_id || null, url: data.url || null },
    };
    event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const convId = event.notification.data && event.notification.data.conversation_id;
    const url = event.notification.data && event.notification.data.url;
    const targetUrl = convId ? '/#chat/' + convId : (url || '/');
    event.waitUntil((async () => {
        const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
        for (const client of wins) {
            if ('focus' in client) {
                await client.focus();
                // Tell the already-open app where to go.
                client.postMessage({ type: 'push-navigate', conversation_id: convId, url: url || null });
                return;
            }
        }
        // No open window — cold-open the app on the right page.
        if (self.clients.openWindow) await self.clients.openWindow(targetUrl);
    })());
});
