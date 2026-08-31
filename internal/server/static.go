package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

/*
Serving the embedded frontend with browser caching that actually works.

The problem this solves: the frontend is embedded with go:embed, and every
file in an embed.FS reports a ZERO modification time. http.ServeContent
omits Last-Modified when the modtime is zero and http.FileServer never
generates an ETag, so a static asset came back with no validator of any
kind. A browser therefore had nothing to revalidate against, and every
single page load re-downloaded the whole frontend — about 725KB across 21
scripts plus the stylesheet — no matter how many times it had been fetched
before.

The fix has two halves:

  * Every asset gets a strong ETag, computed once at startup from its
    content. That alone turns a repeat load into conditional requests that
    answer 304 with no body.

  * index.html is rewritten at startup so each script and stylesheet URL
    carries ?v=<buildID>, and a request that arrives with the current
    buildID is served immutable for a year. A repeat load then costs a
    single conditional request for index.html and nothing else: the rest
    comes from the browser's own cache without touching the network.

The buildID is a hash of every asset's content, so a new deployment mints
new URLs and clients pick the change up immediately. That is what makes the
year-long cache safe without a build step or content-hashed filenames:
nothing is ever cached under a URL that could later mean something else.
index.html itself is always no-cache, since it is the thing that hands out
those URLs.
*/

type staticHandler struct {
	fileSrv http.Handler
	// etags maps a slash-separated path with no leading slash ("js/app.js")
	// to its quoted strong ETag.
	etags     map[string]string
	buildID   string
	index     []byte
	indexETag string
}

// assetRefRE matches the local script/stylesheet URLs in index.html that are
// safe to version: an absolute path into the bundled /js/ or /css/ trees.
// Anything else — a remote URL, an image, the manifest — is deliberately
// left alone, so this can only ever add a query string to a file this
// handler is itself serving.
var assetRefRE = regexp.MustCompile(`(src|href)="(/(?:js|css)/[A-Za-z0-9._/-]+)"`)

func newStaticHandler(webSub fs.FS) (*staticHandler, error) {
	h := &staticHandler{
		fileSrv: http.FileServer(http.FS(webSub)),
		etags:   make(map[string]string),
	}

	// Hash every asset once. The whole frontend is a fraction of a megabyte
	// already resident in the binary, so this costs a few milliseconds at
	// boot and saves the work on every request afterwards.
	var paths []string
	err := fs.WalkDir(webSub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(webSub, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		h.etags[p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hashing embedded assets: %w", err)
	}

	// The build id folds in every asset's hash, and the walk is sorted first
	// so the id depends only on content — not on the order the filesystem
	// happened to yield entries in.
	sort.Strings(paths)
	all := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(all, "%s %s\n", p, h.etags[p])
	}
	h.buildID = hex.EncodeToString(all.Sum(nil))[:12]

	index, err := fs.ReadFile(webSub, "index.html")
	if err != nil {
		return nil, fmt.Errorf("reading index.html: %w", err)
	}
	h.index = assetRefRE.ReplaceAll(index, []byte(`$1="$2?v=`+h.buildID+`"`))
	sum := sha256.Sum256(h.index)
	h.indexETag = `"` + hex.EncodeToString(sum[:16]) + `"`

	return h, nil
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" || p == "index.html" {
		h.serveIndex(w, r)
		return
	}

	etag, ok := h.etags[p]
	if !ok {
		// Not a real file: this is an SPA route like /files or /chat, which
		// the client-side router resolves from index.html.
		h.serveIndex(w, r)
		return
	}

	w.Header().Set("Etag", etag)
	if r.URL.Query().Get("v") == h.buildID {
		// Reached through a URL minted by this exact build, so the bytes
		// behind it can never change. Safe to keep for a year and never
		// revalidate.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// An unversioned request — the service worker, the manifest, an
		// image referenced from CSS, or a stale URL from a previous build.
		// Revalidate every time, but the ETag still keeps the body off the
		// wire when nothing changed.
		w.Header().Set("Cache-Control", "no-cache")
	}
	h.fileSrv.ServeHTTP(w, r)
}

// serveIndex always revalidates: index.html is what hands out the versioned
// asset URLs, so a cached copy of it would pin a client to an old build.
func (h *staticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Etag", h.indexETag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A zero modtime keeps ServeContent from emitting a Last-Modified it
	// cannot stand behind; the ETag above is the validator, and
	// ServeContent honours If-None-Match against it for free.
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
}
