package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"paylash/internal/models"
	"paylash/internal/storage"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	linkPreviewFetchTimeout = 8 * time.Second
	linkPreviewCacheTTL     = 24 * time.Hour
	linkPreviewUserAgent    = "Mozilla/5.0 (compatible; PaylashLinkPreview/1.0)"
	htmlFetchMaxBytes       = 2 << 20 // 2MB — plenty for a page's <head>
	imageFetchMaxBytes      = 5 << 20 // 5MB
)

// linkPreviewURLPattern mirrors the client's own linkify pass (see
// linkifyEscaped in chat.js) — the preview always corresponds to whatever
// actually renders as a clickable link in the message.
var linkPreviewURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// extractFirstURL returns the first http(s) URL in body, trimmed of common
// trailing punctuation, or "" if there is none.
func extractFirstURL(body string) string {
	m := linkPreviewURLPattern.FindString(body)
	if m == "" {
		return ""
	}
	return trimTrailingPunctuation(m)
}

// trailingPunctuation is stripped unconditionally from a matched URL's end —
// characters a sentence commonly ends a link with, never legitimately part
// of one.
const trailingPunctuation = ".,!?;:'\"]}"

// trimTrailingPunctuation strips common trailing punctuation off a matched
// URL, one character at a time — except a trailing ')' is left alone once
// the URL has at least as many '(' as ')', since a huge number of real URLs
// end in a legitimate closing paren (Wikipedia article slugs like
// .../wiki/Foo_(bar), among others). Blindly stripping every trailing ')'
// would otherwise truncate exactly those URLs into a broken link.
func trimTrailingPunctuation(u string) string {
	for u != "" {
		last := u[len(u)-1]
		if last == ')' {
			if strings.Count(u, "(") >= strings.Count(u, ")") {
				break
			}
			u = u[:len(u)-1]
			continue
		}
		if strings.IndexByte(trailingPunctuation, last) < 0 {
			break
		}
		u = u[:len(u)-1]
	}
	return u
}

// linkPreviewHTTPClient is the one client every unfurl fetch (page + image)
// goes through. Its Transport dials through safeDialContext, which resolves
// and validates the destination on every single connection it makes —
// including ones made to follow a redirect, since Go's client re-invokes
// DialContext per hop through the same Transport. That matters here more
// than for a typical outbound HTTP call: this app's own Postgres, MinIO, and
// Collabora containers sit on private Docker-network addresses, and
// 169.254.169.254 is the cloud metadata endpoint on most providers, so
// naively fetching whatever URL a chat message contains would let any
// participant probe or hit them by just pasting a link.
var linkPreviewHTTPClient = &http.Client{
	Timeout: linkPreviewFetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
	Transport: &http.Transport{
		DialContext: safeDialContext,
		Proxy:       nil, // never tunnel through a proxy that could reach an otherwise-blocked host
	},
}

// safeDialContext resolves host itself and refuses to connect to any
// resolved address that's loopback, private, link-local, or otherwise not a
// genuine public destination.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	var lastErr error = fmt.Errorf("no usable address for %s", host)
	for _, ipAddr := range ips {
		if isDisallowedIP(ipAddr.IP) {
			lastErr = fmt.Errorf("refusing to connect to disallowed address %s", ipAddr.IP)
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// isDisallowedIP blocks loopback/private/link-local/unspecified/multicast
// ranges — this covers RFC1918 (Docker-network peers), 127.0.0.0/8,
// 169.254.0.0/16 (includes the cloud metadata endpoint), and their IPv6
// equivalents.
func isDisallowedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 0 { // 0.0.0.0/8
		return true
	}
	return false
}

// maybeUnfurlLink kicks off best-effort async link-preview resolution for a
// freshly sent text message — never blocks the caller. Any failure
// (unreachable host, blocked by the SSRF guard, non-HTML response, no
// usable title) just leaves the message with no preview.
func (h *Handler) maybeUnfurlLink(convID, msgID int, body string) {
	if u := extractFirstURL(body); u != "" {
		go h.fetchLinkPreview(convID, msgID, u)
	}
}

// fetchLinkPreview resolves rawURL (fetching+parsing it if not already
// cached) and, on success, attaches it to msgID and broadcasts the result.
// Runs in its own goroutine — recover() guards against a malformed page
// tripping something in the HTML tokenizer from ever taking the process
// down, since this processes attacker-controlled remote content.
func (h *Handler) fetchLinkPreview(convID, msgID int, rawURL string) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("link preview: panic fetching %s: %v", rawURL, p)
		}
	}()

	pageURL, err := url.Parse(rawURL)
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.Host == "" || pageURL.User != nil {
		return
	}

	if cached, err := h.db.GetLinkPreviewCache(rawURL, linkPreviewCacheTTL); err == nil && cached != nil {
		h.attachLinkPreview(convID, msgID, cached)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), linkPreviewFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", linkPreviewUserAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := linkPreviewHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return
	}

	title, description, imageRef := parseOpenGraph(io.LimitReader(resp.Body, htmlFetchMaxBytes))
	title = truncateRunes(strings.TrimSpace(title), 300)
	if title == "" {
		return
	}
	description = truncateRunes(strings.TrimSpace(description), 500)

	imageKey := ""
	if imageRef != "" {
		if resolved, err := pageURL.Parse(imageRef); err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			imageKey = h.fetchAndStoreLinkPreviewImage(ctx, resolved.String())
		}
	}

	if err := h.db.UpsertLinkPreview(rawURL, title, description, imageKey); err != nil {
		return
	}
	h.attachLinkPreview(convID, msgID, &models.LinkPreview{URL: rawURL, Title: title, Description: description, HasImage: imageKey != ""})
}

// fetchAndStoreLinkPreviewImage downloads imageURL (already SSRF-guarded via
// linkPreviewHTTPClient) and stores it in storage.ChatAttachmentsBucket,
// returning its object key — or "" on any failure, which just means the
// preview card renders text-only.
func (h *Handler) fetchAndStoreLinkPreviewImage(ctx context.Context, imageURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", linkPreviewUserAgent)
	resp, err := linkPreviewHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, imageFetchMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > imageFetchMaxBytes {
		return ""
	}
	if err := h.minio.EnsureBucket(ctx, storage.ChatAttachmentsBucket); err != nil {
		return ""
	}
	key := "linkpreview/" + randomHexToken(16) + extFromMime(ct)
	if err := h.minio.Upload(ctx, storage.ChatAttachmentsBucket, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		return ""
	}
	return key
}

// attachLinkPreview persists the resolved preview against msgID and tells
// every current participant so an already-rendered message can get its
// preview card appended without a reload.
func (h *Handler) attachLinkPreview(convID, msgID int, preview *models.LinkPreview) {
	if err := h.db.SetMessageLinkPreview(msgID, preview.URL); err != nil {
		return
	}
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		return
	}
	h.chatHub.broadcast(participantIDs(participants), map[string]any{
		"type": "message.link_preview", "conversation_id": convID, "message_id": msgID, "preview": preview,
	})
}

// parseOpenGraph walks r's HTML looking for Open Graph / standard meta tags,
// falling back to <title> when there's no og:title. Stops as soon as </head>
// closes (or <body> starts, for pages with sloppy/missing </head>) since
// everything relevant lives in the head.
func parseOpenGraph(r io.Reader) (title, description, image string) {
	tokenizer := html.NewTokenizer(r)
	var fallbackTitle, fallbackDescription string
	inTitle := false
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := tokenizer.Token()
			switch tok.Data {
			case "meta":
				prop, content := metaAttr(tok)
				switch prop {
				case "og:title":
					title = content
				case "og:description":
					description = content
				case "description":
					// A generic <meta name="description"> is a lower-priority
					// fallback than og:description — but conventionally
					// appears EARLIER in <head> than a page's Open Graph
					// block, so this must never let an earlier plain
					// description block a later, better og:description from
					// winning (that ordering bug is exactly what the
					// fallbackTitle/title split below already avoids for
					// titles).
					if fallbackDescription == "" {
						fallbackDescription = content
					}
				case "og:image", "og:image:url":
					if image == "" {
						image = content
					}
				}
			case "title":
				inTitle = true
			case "body":
				return firstNonEmpty(title, fallbackTitle), firstNonEmpty(description, fallbackDescription), image
			}
		case html.TextToken:
			if inTitle {
				fallbackTitle += tokenizer.Token().Data
			}
		case html.EndTagToken:
			tok := tokenizer.Token()
			if tok.Data == "title" {
				inTitle = false
			}
			if tok.Data == "head" {
				return firstNonEmpty(title, fallbackTitle), firstNonEmpty(description, fallbackDescription), image
			}
		}
	}
	return firstNonEmpty(title, fallbackTitle), firstNonEmpty(description, fallbackDescription), image
}

// metaAttr pulls a <meta> tag's property/name and content — the tokenizer
// already HTML-unescapes attribute values, so these come back as plain text.
func metaAttr(tok html.Token) (prop, content string) {
	var name string
	for _, a := range tok.Attr {
		switch a.Key {
		case "property":
			name = a.Val
		case "name":
			if name == "" {
				name = a.Val
			}
		case "content":
			content = a.Val
		}
	}
	return name, content
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// truncateRunes caps s to at most n runes — the metadata columns this feeds
// (link_previews.title/description) have byte limits, and truncating by
// rune (not byte) avoids splitting a multi-byte character in half.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
