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
	// Touched only by this connection's readPump goroutine (no lock needed) —
	// a server-side throttle so a chatty/hostile client can't fan "typing"
	// events out to a conversation faster than once every couple of seconds.
	lastTypingConv int
	lastTypingAt   time.Time
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
// oldest one for that user if already at the cap. The bool return reports
// whether this is the user's FIRST live connection (they just came online),
// so the caller can fire a presence broadcast exactly on the transition.
func (hub *chatHub) register(userID int, ws *websocket.Conn) (*chatConn, bool) {
	c := &chatConn{userID: userID, ws: ws, send: make(chan []byte, sendBufferSize)}
	hub.mu.Lock()
	conns := hub.conns[userID]
	becameOnline := len(conns) == 0
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
	return c, becameOnline
}

// unregister removes a connection and reports whether it was the user's LAST
// one (they just went offline), the mirror of register's return.
func (hub *chatHub) unregister(c *chatConn) bool {
	hub.mu.Lock()
	conns := hub.conns[c.userID]
	for i, existing := range conns {
		if existing == c {
			hub.conns[c.userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	becameOffline := len(hub.conns[c.userID]) == 0
	if becameOffline {
		delete(hub.conns, c.userID)
	}
	hub.mu.Unlock()
	close(c.send)
	return becameOffline
}

// isOnline reports whether the user currently has at least one live socket.
func (hub *chatHub) isOnline(userID int) bool {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return len(hub.conns[userID]) > 0
}

// count returns the total number of live connections across every user —
// backs the paylash_ws_connections gauge in /metrics.
func (hub *chatHub) count() int {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	n := 0
	for _, conns := range hub.conns {
		n += len(conns)
	}
	return n
}

// LiveConnectionCount exposes chatHub.count() to internal/server's /metrics
// handler, which has no other reason to depend on this package's internals.
func (h *Handler) LiveConnectionCount() int {
	return h.chatHub.count()
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

// readPump detects disconnects (ReadMessage erroring is how gorilla surfaces
// a closed/dead connection) and dispatches the messages a client sends UP the
// socket — today only "typing". The pong handler resets the read deadline so
// a live-but-quiet connection isn't mistaken for dead; every inbound frame
// resets it too. On the user's last connection dropping it fires the offline
// presence transition.
func (c *chatConn) readPump(h *Handler) {
	hub := h.chatHub
	defer func() {
		if becameOffline := hub.unregister(c); becameOffline {
			h.onUserOffline(c.userID)
		}
		c.ws.Close()
	}()
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		c.ws.SetReadDeadline(time.Now().Add(pongWait))
		h.handleClientMessage(c, data)
	}
}

// handleClientMessage processes a frame the browser sent up the socket. Only
// "typing" is understood; anything else is ignored (forward-compatible).
func (h *Handler) handleClientMessage(c *chatConn, data []byte) {
	if len(data) > 512 { // a control frame like this is tiny; cap it defensively
		return
	}
	var msg struct {
		Type           string `json:"type"`
		ConversationID int    `json:"conversation_id"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.Type == "typing" {
		h.handleTyping(c, msg.ConversationID)
	}
}

// handleTyping relays a "user is typing" signal to the OTHER participants of a
// conversation, after verifying the sender belongs to it. Throttled per
// connection so it can't be used to flood, and never echoed back to the typer.
func (h *Handler) handleTyping(c *chatConn, convID int) {
	if convID <= 0 {
		return
	}
	now := time.Now()
	if convID == c.lastTypingConv && now.Sub(c.lastTypingAt) < 2*time.Second {
		return
	}
	ok, err := h.db.IsParticipant(convID, c.userID)
	if err != nil || !ok {
		return
	}
	c.lastTypingConv = convID
	c.lastTypingAt = now
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		return
	}
	var name string
	others := make([]int, 0, len(participants))
	for _, p := range participants {
		if p.UserID == c.userID {
			name = p.DisplayName
		} else {
			others = append(others, p.UserID)
		}
	}
	h.chatHub.broadcast(others, map[string]any{
		"type": "typing", "conversation_id": convID, "user_id": c.userID, "user_name": name,
	})
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
	c, becameOnline := h.chatHub.register(user.ID, ws)
	if becameOnline {
		h.onUserOnline(user.ID)
	}
	go c.writePump()
	c.readPump(h)
}
