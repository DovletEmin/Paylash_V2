package server

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// metrics is a tiny in-process counter store exposed at GET /metrics in
// Prometheus text exposition format — enough for a small deployment's
// dashboard/alerting without pulling in a client library. This app already
// runs fully offline by design (see README), so a build step that needs
// network access to fetch a metrics dependency is worth avoiding; the actual
// need here — request counts/latency by route, current WebSocket
// connections, uptime — is small enough that hand-rolling it is both
// simpler and one less third-party dependency to trust.
type metrics struct {
	mu        sync.Mutex
	routes    map[routeKey]*routeStats
	startedAt time.Time
}

type routeKey struct {
	method  string
	pattern string
	status  int
}

type routeStats struct {
	count      int64
	totalMicro int64
}

func newMetrics() *metrics {
	return &metrics{routes: make(map[routeKey]*routeStats), startedAt: time.Now()}
}

// observe records one completed request. pattern is the ServeMux route
// pattern (r.Pattern, e.g. "GET /api/files/{id}"), never the raw URL —
// grouping by pattern instead of literal path keeps the metric's
// cardinality bounded regardless of how many distinct file/user/message ids
// are ever requested.
func (m *metrics) observe(method, pattern string, status int, d time.Duration) {
	if pattern == "" {
		pattern = "(unmatched)"
	}
	key := routeKey{method: method, pattern: pattern, status: status}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.routes[key]
	if s == nil {
		s = &routeStats{}
		m.routes[key] = s
	}
	s.count++
	s.totalMicro += d.Microseconds()
}

// liveConnCounter is satisfied by api.Handler — kept as a narrow interface
// here so internal/server doesn't need any other coupling to internal/api
// than the one method it actually needs for the ws-connections gauge.
type liveConnCounter interface {
	LiveConnectionCount() int
}

// writeTo renders every counted route plus the process-wide gauges as
// Prometheus text exposition format (the same plain-text line protocol
// `# HELP` / `# TYPE` / `metric{labels} value` a real Prometheus server or
// `curl` can both read directly).
func (m *metrics) writeTo(w io.Writer, conns liveConnCounter) {
	m.mu.Lock()
	keys := make([]routeKey, 0, len(m.routes))
	stats := make(map[routeKey]routeStats, len(m.routes))
	for k, s := range m.routes {
		keys = append(keys, k)
		stats[k] = *s
	}
	m.mu.Unlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pattern != keys[j].pattern {
			return keys[i].pattern < keys[j].pattern
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})

	fmt.Fprintln(w, "# HELP paylash_http_requests_total Total HTTP requests, by route pattern and status code.")
	fmt.Fprintln(w, "# TYPE paylash_http_requests_total counter")
	for _, k := range keys {
		fmt.Fprintf(w, "paylash_http_requests_total{method=%q,route=%q,status=%q} %d\n",
			k.method, k.pattern, fmt.Sprint(k.status), stats[k].count)
	}

	fmt.Fprintln(w, "# HELP paylash_http_request_duration_seconds_sum Total time spent handling requests, by route pattern and status code.")
	fmt.Fprintln(w, "# TYPE paylash_http_request_duration_seconds_sum counter")
	for _, k := range keys {
		fmt.Fprintf(w, "paylash_http_request_duration_seconds_sum{method=%q,route=%q,status=%q} %f\n",
			k.method, k.pattern, fmt.Sprint(k.status), float64(stats[k].totalMicro)/1e6)
	}

	fmt.Fprintln(w, "# HELP paylash_ws_connections Currently open chat WebSocket connections.")
	fmt.Fprintln(w, "# TYPE paylash_ws_connections gauge")
	n := 0
	if conns != nil {
		n = conns.LiveConnectionCount()
	}
	fmt.Fprintf(w, "paylash_ws_connections %d\n", n)

	fmt.Fprintln(w, "# HELP paylash_uptime_seconds Time since this process started.")
	fmt.Fprintln(w, "# TYPE paylash_uptime_seconds counter")
	fmt.Fprintf(w, "paylash_uptime_seconds %f\n", time.Since(m.startedAt).Seconds())
}

func (m *metrics) handler(conns liveConnCounter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		m.writeTo(&b, conns)
		w.Write([]byte(b.String()))
	}
}
