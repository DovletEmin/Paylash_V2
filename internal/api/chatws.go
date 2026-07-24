package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"paylash/internal/authutil"
)

const (
	// pongWait/pingPeriod detect a dead connection that never sent a proper
	// close frame (network drop, laptop sleep) — pingPeriod must stay well
	// under pongWait so at least one ping has a chance to land and be
	// answered before the deadline expires.
	pongWait   = 60 * time.Second
	pingPeriod = 54 * time.Second

	// maxConnsPerUser bounds fd exhaustion from a compromised/buggy client
	// opening connections in a loop — generous enough for a handful of real
	// browser tabs plus the desktop app.
	maxConnsPerUser = 20

	// sendBufferSize is the outbound channel depth per connection. A client
	// that never drains it (stuck tab, dead network the pinger hasn't
	// noticed yet) gets that one connection closed rather than blocking
	// broadcast to every other participant.
	sendBufferSize = 16
)

var chatUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Deliberately NOT overridden to always return true — this app has zero
	// CORS anywhere by design, and gorilla's default CheckOrigin (same-origin
	// only) is exactly the right behavior here.
}

// chatConn is one live WebSocket connection. writePump owns every write to
// ws — gorilla connections allow exactly one concurrent writer, so push/
// broadcast never call ws.WriteJSON directly, only send on this channel.
type chatConn struct {
	userID int
	ws     *websocket.Conn
	send   chan []byte
}

// chatHub tracks every user's live connections in memory — consistent with
// this app's existing single-instance-only architecture (see keyedLimiter):
// no cross-instance pub/sub, resets on restart, fine at this scale.
type chatHub struct {
	mu    sync.Mutex
	conns map[int][]*chatConn
}

func newChatHub() *chatHub {
	return &chatHub{conns: make(map[int][]*chatConn)}
}

// register adds a connection, enforcing maxConnsPerUser by dropping the
// oldest one for that user if already at the cap.
func (hub *chatHub) register(userID int, ws *websocket.Conn) *chatConn {
	c := &chatConn{userID: userID, ws: ws, send: make(chan []byte, sendBufferSize)}
	hub.mu.Lock()
	conns := hub.conns[userID]
	var oldest *chatConn
	if len(conns) >= maxConnsPerUser {
		oldest = conns[0]
		conns = conns[1:]
	}
	hub.conns[userID] = append(conns, c)
	hub.mu.Unlock()
	if oldest != nil {
		// Close the socket, not oldest.send directly — that connection's own
		// readPump/unregister will close its send channel exactly once when
		// ws.Close() makes ReadMessage error out. Closing the channel here
		// too would double-close it (readPump's deferred unregister always
		// closes it) and panic.
		oldest.ws.Close()
	}
	return c
}

func (hub *chatHub) unregister(c *chatConn) {
	hub.mu.Lock()
	conns := hub.conns[c.userID]
	for i, existing := range conns {
		if existing == c {
			hub.conns[c.userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(hub.conns[c.userID]) == 0 {
		delete(hub.conns, c.userID)
	}
	hub.mu.Unlock()
	close(c.send)
}

// broadcast fans an event out to every one of userIDs' active connections
// (multiple tabs/devices per user) — a non-blocking send, so one stuck
// connection can never delay delivery to anyone else.
func (hub *chatHub) broadcast(userIDs []int, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("chat ws: marshal event: %v", err)
		return
	}
	for _, uid := range userIDs {
		hub.mu.Lock()
		conns := append([]*chatConn(nil), hub.conns[uid]...)
		hub.mu.Unlock()
		for _, c := range conns {
			select {
			case c.send <- data:
			default:
				c.ws.Close()
			}
		}
	}
}

// writePump owns all writes to c.ws — the ping ticker lives here too, so a
// keepalive ping can never race a broadcast write on the same connection.
func (c *chatConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump's only real job in v1 (no typing indicators yet) is detecting
// disconnects: ReadMessage returning an error is how gorilla surfaces a
// closed/dead connection, and the pong handler resets the read deadline so
// a live-but-quiet connection isn't mistaken for a dead one.
func (c *chatConn) readPump(hub *chatHub) {
	defer func() {
		hub.unregister(c)
		c.ws.Close()
	}()
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.ws.ReadMessage(); err != nil {
			return
		}
	}
}

// ChatWebSocket upgrades the connection — auth needs no extra plumbing here:
// this is a normal same-origin browser GET carrying the session cookie like
// any other request, and AuthMiddleware/LoggingMiddleware both pass
// http.ResponseWriter straight through, so the http.Hijacker support
// gorilla's Upgrade needs survives the middleware chain unmodified.
func (h *Handler) ChatWebSocket(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	ws, err := chatUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("chat ws upgrade: %v", err)
		return
	}
	c := h.chatHub.register(user.ID, ws)
	go c.writePump()
	c.readPump(h.chatHub)
}
