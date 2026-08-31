package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!DOCTYPE html><html><head>` +
			`<link rel="icon" href="/img/favicon-32.png">` +
			`<link rel="stylesheet" href="/css/style.css">` +
			`</head><body><div id="app"></div>` +
			`<script src="/js/app.js"></script>` +
			`<script src="/js/locales/ru.js"></script>` +
			`</body></html>`)},
		"css/style.css":      {Data: []byte("body{color:red}")},
		"js/app.js":          {Data: []byte("console.log(1)")},
		"js/locales/ru.js":   {Data: []byte("window.X={}")},
		"img/favicon-32.png": {Data: []byte("\x89PNG-not-really")},
		"sw.js":              {Data: []byte("self.addEventListener('push',()=>{})")},
	}
}

func newTestStatic(t *testing.T, fsys fstest.MapFS) *staticHandler {
	t.Helper()
	h, err := newStaticHandler(fsys)
	if err != nil {
		t.Fatalf("newStaticHandler: %v", err)
	}
	return h
}

func get(h *staticHandler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// The bug this whole file exists for: before versioning, an asset came back
// with no ETag and no Last-Modified, so a browser had nothing to revalidate
// against and re-downloaded the entire frontend on every page load.
func TestStaticAssetsCarryAValidator(t *testing.T) {
	h := newTestStatic(t, testAssets())
	for _, p := range []string{"/js/app.js", "/css/style.css", "/js/locales/ru.js", "/sw.js", "/"} {
		w := get(h, p, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, w.Code)
		}
		if w.Header().Get("Etag") == "" {
			t.Errorf("%s: no ETag, so nothing can be revalidated", p)
		}
		if w.Header().Get("Cache-Control") == "" {
			t.Errorf("%s: no Cache-Control", p)
		}
	}
}

func TestStaticRevalidationReturns304WithNoBody(t *testing.T) {
	h := newTestStatic(t, testAssets())
	for _, p := range []string{"/js/app.js", "/css/style.css", "/"} {
		etag := get(h, p, nil).Header().Get("Etag")
		w := get(h, p, map[string]string{"If-None-Match": etag})
		if w.Code != http.StatusNotModified {
			t.Errorf("%s: revalidation returned %d, want 304", p, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("%s: 304 re-sent %d bytes", p, w.Body.Len())
		}
	}
}

// index.html is rewritten so every script/stylesheet URL carries the build
// id — that is what makes the year-long cache on those URLs safe.
func TestIndexVersionsItsAssetURLs(t *testing.T) {
	h := newTestStatic(t, testAssets())
	body := get(h, "/", nil).Body.String()

	for _, want := range []string{
		`src="/js/app.js?v=` + h.buildID + `"`,
		`src="/js/locales/ru.js?v=` + h.buildID + `"`,
		`href="/css/style.css?v=` + h.buildID + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index.html missing %s\ngot: %s", want, body)
		}
	}
	// Only /js/ and /css/ are rewritten. Touching anything else would risk
	// versioning a URL this handler does not serve.
	if !strings.Contains(body, `href="/img/favicon-32.png"`) {
		t.Error("the favicon reference should have been left alone")
	}
}

func TestStaticCachePolicy(t *testing.T) {
	h := newTestStatic(t, testAssets())

	versioned := get(h, "/js/app.js?v="+h.buildID, nil).Header().Get("Cache-Control")
	if !strings.Contains(versioned, "immutable") || !strings.Contains(versioned, "max-age=31536000") {
		t.Errorf("a correctly versioned asset should be immutable, got %q", versioned)
	}

	// Anything else has to be revalidated: an unversioned URL, or one minted
	// by a previous build.
	for _, p := range []string{"/js/app.js", "/js/app.js?v=deadbeef", "/sw.js", "/"} {
		if cc := get(h, p, nil).Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", p, cc)
		}
	}
}

// The property that makes the year-long cache safe. Change any asset and the
// build id must change with it, so a deployed fix reaches clients instead of
// sitting behind a cache entry that is still valid for a year.
func TestBuildIDTracksContent(t *testing.T) {
	base := newTestStatic(t, testAssets())

	changed := testAssets()
	changed["js/app.js"] = &fstest.MapFile{Data: []byte("console.log(2)")}
	after := newTestStatic(t, changed)

	if base.buildID == after.buildID {
		t.Fatal("editing an asset left the build id unchanged — clients would keep the old file for a year")
	}
	if base.etags["js/app.js"] == after.etags["js/app.js"] {
		t.Error("editing an asset left its ETag unchanged")
	}
	// An untouched file keeps its own ETag, so only what actually changed
	// has to travel again.
	if base.etags["css/style.css"] != after.etags["css/style.css"] {
		t.Error("an untouched asset's ETag changed")
	}

	// Rebuilding from identical content must be reproducible, or every
	// restart would invalidate every client's cache for nothing.
	again := newTestStatic(t, testAssets())
	if base.buildID != again.buildID {
		t.Error("the build id is not reproducible from identical content")
	}
}

// Unknown paths are client-side routes and must still boot the app.
func TestSPAFallbackServesIndex(t *testing.T) {
	h := newTestStatic(t, testAssets())
	for _, p := range []string{"/files", "/chat", "/admin", "/preview"} {
		w := get(h, p, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", p, w.Code)
		}
		if !strings.Contains(w.Body.String(), `<div id="app">`) {
			t.Errorf("%s did not serve the SPA shell", p)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: SPA shell must revalidate, got %q", p, cc)
		}
	}
}

func TestStaticServesRealContent(t *testing.T) {
	h := newTestStatic(t, testAssets())
	if got := get(h, "/js/app.js", nil).Body.String(); got != "console.log(1)" {
		t.Errorf("js body = %q", got)
	}
	if got := get(h, "/css/style.css?v="+h.buildID, nil).Body.String(); got != "body{color:red}" {
		t.Errorf("versioned css body = %q", got)
	}
}
