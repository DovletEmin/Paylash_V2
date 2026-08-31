/* Paýlaş — Image markup layer
   ─────────────────────────────────────────────────────────────────────────
   Freehand review markup drawn over a previewed image: pen, highlighter,
   arrows, shapes, text, an eraser and a laser pointer. The companion to the
   pinned notes in preview.js — those say "look here", this says what to
   change.

   WHY THIS IS HAND-WRITTEN AND NOT EXCALIDRAW
   Excalidraw is a React component of about a megabyte. This frontend has no
   build step (see index.html: plain <script> tags, embedded into the Go
   binary via go:embed), and Caddy serves a CSP of default-src 'self' with no
   CDN allowance, so a third-party bundle can neither be built in nor fetched
   at runtime. What follows borrows Excalidraw's model — vector shapes, one
   active tool, undo over the whole scene — on a plain canvas.

   COORDINATES
   Every stored number is normalised against the image, never pixels: x and y
   as a fraction of rendered width/height, stroke width and font size as a
   fraction of width. A layer therefore lands identically on a phone, on a 4K
   monitor and on the zoomed view, and survives the image being replaced by a
   re-render at another resolution. Pixels only ever exist between reading a
   pointer event and writing to a canvas.

   LAYERS
   The server keeps one row per (file, author), so everyone's markup composes
   on screen while nobody can overwrite anyone else's — see the comment on
   file_annotations in internal/db/db.go. This module edits `scene` (the
   signed-in user's own shapes) and renders `others` (everyone else's) read
   only, underneath. */
const Annotate = {

    /* ── Tunables ── */
    // Long enough that a burst of strokes is one request, short enough that
    // closing the tab right after drawing almost never loses anything —
    // and every exit path this module can see also flushes explicitly.
    SAVE_DEBOUNCE: 800,
    // A laser point's lifetime. Excalidraw's pointer fades over roughly a
    // second; shorter feels twitchy, longer smears across the image.
    LASER_LIFE: 900,
    // Undo depth. Each entry is a full scene copy, which is cheap here
    // because a scene is bounded server-side at 1500 shapes anyway.
    HISTORY_MAX: 60,
    // Pointer slack for hit-testing, as a fraction of image width. Fingers
    // are imprecise and a hairline stroke is genuinely hard to hit.
    HIT_TOLERANCE: 0.012,
    // Simplification: a freehand point closer than this to the previous one
    // adds nothing a human can see and costs a point in the payload.
    MIN_POINT_GAP: 0.002,

    PALETTE: ['#ef4444', '#f59e0b', '#22c55e', '#3b82f6', '#a855f7', '#111827', '#ffffff'],
    WIDTHS: [0.0025, 0.005, 0.010],
    TOOLS: ['select', 'pen', 'hl', 'laser', 'arrow', 'line', 'rect', 'ellipse', 'text', 'eraser'],

    /* ── State ── */
    fileId: null,
    scene: [],        // my shapes, editable
    others: [],       // [{user_id, user_name, shapes:[…]}] — read-only
    myAnnotationId: null,
    visible: true,    // the show/hide checkbox
    editing: false,   // is the tool palette open
    tool: 'pen',
    color: '#ef4444',
    width: 0.005,
    dashed: false,
    selectedId: null,

    _mounted: false,
    _img: null,
    _wrap: null,
    _canvases: null,  // {hl, base, live}
    _history: [],
    _redo: [],
    _drawing: null,
    _dragging: null,
    _laser: [],
    _laserRAF: null,
    _liveRAF: null,
    _saveTimer: null,
    _dirty: false,
    _ro: null,

    // 24×24 stroke icons in the same idiom as UI.icons, so the palette sits
    // beside the rest of the app's chrome instead of looking bolted on.
    ICONS: {
        select:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 2l7.5 18 2.5-7.5L20.5 10z"/></svg>',
        pen:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19l7-7-4-4-7 7-1 5z"/><path d="M16 6l2-2 4 4-2 2"/></svg>',
        hl:      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h6"/><path d="M9 14l-3 3v3h3l3-3z"/><path d="M11 12l5-8 5 4-6 7z"/></svg>',
        laser:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="2.5" fill="currentColor" stroke="none"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/></svg>',
        arrow:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20L20 4"/><path d="M13 4h7v7"/></svg>',
        line:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20L20 4"/></svg>',
        rect:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="5" width="16" height="14" rx="2"/></svg>',
        ellipse: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8"/></svg>',
        text:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 6V4h14v2"/><path d="M12 4v16"/><path d="M9 20h6"/></svg>',
        eraser:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 20H20"/><path d="M15.5 4.5l4 4L10 18H6l-1.5-1.5z"/></svg>',
        undo:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 14L4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12H9"/></svg>',
        redo:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 14l5-5-5-5"/><path d="M20 9H10a6 6 0 0 0 0 12h5"/></svg>',
        dashed:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 12h4M10 12h4M17 12h4"/></svg>',
    },

    /* ── Lifecycle ────────────────────────────────────────────────────── */

    // Called by PreviewPage once an image is on screen. Safe to call again
    // for a different file: it tears the previous mount down first, flushing
    // any unsaved strokes rather than dropping them.
    async mount(fileId, imgEl) {
        this.unmount();
        this.fileId = fileId;
        this._img = imgEl;
        this._wrap = imgEl.parentElement;
        this.scene = [];
        this.others = [];
        this.myAnnotationId = null;
        this._history = [];
        this._redo = [];
        this.selectedId = null;
        this._drawing = null;
        this._laser = [];
        this._dirty = false;
        this._mounted = true;

        this._buildCanvases();
        this._bindGlobalOnce();
        this._fit();
        // The image may still be loading; its rendered box is meaningless
        // until it isn't, and the canvases have to match that box exactly.
        if (!imgEl.complete) imgEl.addEventListener('load', () => this._fit(), { once: true });
        this._ro = new ResizeObserver(() => this._fit());
        this._ro.observe(imgEl);

        await this.load();
    },

    // Deliberately not awaited on the save: stepping through photos with the
    // arrow keys would otherwise stall on a network round trip between every
    // image. flush() captures what it needs before it yields, so the request
    // completes correctly against the file it belongs to long after this
    // mount is gone.
    unmount() {
        if (!this._mounted) return;
        this.flush();
        this._ro?.disconnect();
        this._ro = null;
        if (this._laserRAF) cancelAnimationFrame(this._laserRAF);
        if (this._liveRAF) cancelAnimationFrame(this._liveRAF);
        this._laserRAF = this._liveRAF = null;
        this._removeTextInput();
        for (const c of Object.values(this._canvases || {})) c.remove();
        this._canvases = null;
        this._mounted = false;
        this.editing = false;
    },

    async load() {
        let list = [];
        try { list = (await API.files.annotations.list(this.fileId)) || []; } catch { list = []; }
        const me = App.user ? App.user.id : -1;
        this.others = [];
        for (const a of list) {
            const shapes = Array.isArray(a.shapes) ? a.shapes : [];
            if (a.user_id === me) {
                this.scene = shapes;
                this.myAnnotationId = a.id;
            } else {
                this.others.push({ user_id: a.user_id, user_name: a.user_name, shapes });
            }
        }
        this.redrawStatic();
        PreviewPage.renderAnnotateBar();
    },

    // Whether there is anything at all to show — drives the checkbox's
    // enabled state so it isn't offered for an image nobody has marked up.
    hasAnything() {
        return this.scene.length > 0 || this.others.some(o => o.shapes.length > 0);
    },

    /* ── Canvas plumbing ──────────────────────────────────────────────── */

    // Three stacked canvases rather than one, because they need different
    // compositing:
    //   hl   — highlighter only, in mix-blend-mode:multiply so it darkens
    //          the photo underneath like a real marker instead of veiling
    //          it. Blending has to happen against the IMAGE, which means it
    //          has to be a property of the element, not of a draw call.
    //   base — every other committed shape, painted over the highlighter.
    //   live — the stroke in progress and the laser trail, cleared and
    //          repainted every frame so dragging never repaints the scene.
    _buildCanvases() {
        const mk = cls => {
            const c = document.createElement('canvas');
            c.className = 'annot-canvas ' + cls;
            this._wrap.appendChild(c);
            return c;
        };
        this._canvases = { hl: mk('annot-hl'), base: mk('annot-base'), live: mk('annot-live') };
        const live = this._canvases.live;
        live.addEventListener('pointerdown', e => this._onDown(e));
        live.addEventListener('pointermove', e => this._onMove(e));
        live.addEventListener('pointerup', e => this._onUp(e));
        live.addEventListener('pointercancel', e => this._onUp(e));
        live.addEventListener('pointerleave', e => this._onLeave(e));
        this._applyInteractivity();
    },

    // Sizes the canvases to the image's rendered box, in device pixels so
    // strokes stay crisp on a HiDPI screen, then repaints. Called on load,
    // on resize, and whenever the zoom class changes the box.
    _fit() {
        if (!this._mounted || !this._img) return;
        const w = this._img.clientWidth, h = this._img.clientHeight;
        if (!w || !h) return;
        const dpr = Math.min(window.devicePixelRatio || 1, 3);
        for (const c of Object.values(this._canvases)) {
            c.width = Math.round(w * dpr);
            c.height = Math.round(h * dpr);
            c.style.width = w + 'px';
            c.style.height = h + 'px';
            c.getContext('2d').setTransform(dpr, 0, 0, dpr, 0, 0);
        }
        this.redrawStatic();
        this._redrawLive();
    },

    // Only the live canvas ever takes pointer events, and only while the
    // palette is open — otherwise it would swallow the click-to-zoom and
    // the comment-pin placement that the image already owns.
    _applyInteractivity() {
        if (!this._canvases) return;
        const on = this.editing && this.visible;
        this._canvases.live.style.pointerEvents = on ? 'auto' : 'none';
        this._canvases.live.dataset.tool = this.tool;
        for (const c of Object.values(this._canvases)) c.classList.toggle('hidden', !this.visible);
    },

    _box() {
        return { w: this._img.clientWidth, h: this._img.clientHeight };
    },

    // Pointer event → normalised image coordinates. Clamped only loosely:
    // a stroke that runs off the edge is a real thing people draw, and the
    // server accepts a little overflow for exactly that reason.
    _pt(ev) {
        const r = this._img.getBoundingClientRect();
        return [
            Math.max(-0.5, Math.min(1.5, (ev.clientX - r.left) / r.width)),
            Math.max(-0.5, Math.min(1.5, (ev.clientY - r.top) / r.height)),
        ];
    },

    /* ── Drawing ──────────────────────────────────────────────────────── */

    _onDown(ev) {
        if (!this.editing || ev.button === 2) return;
        ev.preventDefault();
        this._canvases.live.setPointerCapture(ev.pointerId);
        const p = this._pt(ev);

        if (this.tool === 'laser') { this._laser = [{ p, t: performance.now() }]; this._startLaser(); return; }
        if (this.tool === 'eraser') { this._eraseAt(p, true); return; }
        if (this.tool === 'text') { this._beginText(p); return; }
        if (this.tool === 'select') { this._beginSelect(p); return; }

        this._drawing = {
            id: this._newId(), t: this.tool, c: this.color, w: this.width,
            d: this.dashed ? 1 : 0, p: [p],
        };
        this._scheduleLive();
    },

    _onMove(ev) {
        if (!this.editing) return;
        const p = this._pt(ev);

        if (this.tool === 'laser' && this._laser.length) {
            this._laser.push({ p, t: performance.now() });
            return;
        }
        if (this.tool === 'eraser' && ev.buttons) { this._eraseAt(p, false); return; }
        if (this._dragging) { this._moveSelection(p); return; }
        if (!this._drawing) return;

        if (this._drawing.t === 'pen' || this._drawing.t === 'hl') {
            const last = this._drawing.p[this._drawing.p.length - 1];
            if (Math.hypot(p[0] - last[0], p[1] - last[1]) < this.MIN_POINT_GAP) return;
            this._drawing.p.push(p);
        } else {
            // Every other tool is defined by two corners; dragging just
            // moves the second one.
            this._drawing.p[1] = p;
        }
        this._scheduleLive();
    },

    _onUp(ev) {
        if (!this.editing) return;
        try { this._canvases.live.releasePointerCapture(ev.pointerId); } catch { /* already released */ }

        if (this._dragging) {
            this._dragging = null;
            this._markDirty();
            return;
        }
        if (this.tool === 'laser') { this._laser = []; return; }
        if (this.tool === 'eraser') { if (this._erasedThisStroke) { this._erasedThisStroke = false; this._markDirty(); } return; }
        if (!this._drawing) return;

        const s = this._drawing;
        this._drawing = null;
        // A click with no drag leaves a degenerate shape — a zero-size
        // rectangle, an arrow of no length. Only a pen keeps it, as a dot.
        const isDot = s.p.length === 1;
        if (isDot && s.t !== 'pen' && s.t !== 'hl') { this._redrawLive(); return; }
        if (!isDot && s.t !== 'pen' && s.t !== 'hl') {
            const [a, b] = s.p;
            if (Math.hypot(b[0] - a[0], b[1] - a[1]) < 0.004) { this._redrawLive(); return; }
        }
        this._pushHistory();
        this.scene.push(s);
        this._redrawLive();
        this.redrawStatic();
        this._markDirty();
    },

    _onLeave() {
        // Leaving the canvas mid-laser should not freeze the dot on screen.
        if (this.tool === 'laser') this._laser = [];
    },

    /* ── Select and move ──────────────────────────────────────────────── */

    _beginSelect(p) {
        const hit = this._hitTest(p);
        this.selectedId = hit ? hit.id : null;
        if (hit) this._dragging = { id: hit.id, from: p, orig: JSON.parse(JSON.stringify(hit.p)) };
        this.redrawStatic();
        this._redrawLive();
    },

    _moveSelection(p) {
        const s = this.scene.find(x => x.id === this._dragging.id);
        if (!s) return;
        const dx = p[0] - this._dragging.from[0], dy = p[1] - this._dragging.from[1];
        s.p = this._dragging.orig.map(([x, y]) => [x + dx, y + dy]);
        this.redrawStatic();
    },

    deleteSelected() {
        if (!this.selectedId) return;
        this._pushHistory();
        this.scene = this.scene.filter(s => s.id !== this.selectedId);
        this.selectedId = null;
        this.redrawStatic();
        this._markDirty();
    },

    /* ── Eraser ───────────────────────────────────────────────────────── */

    _erasedThisStroke: false,

    // Excalidraw's eraser removes whole elements rather than pixels, which
    // is the right model for vector markup: a half-erased stroke is not
    // something you can store or undo cleanly.
    _eraseAt(p, isFirst) {
        if (isFirst) { this._pushHistory(); this._erasedThisStroke = false; }
        const hit = this._hitTest(p);
        if (!hit) return;
        this.scene = this.scene.filter(s => s.id !== hit.id);
        this._erasedThisStroke = true;
        this.redrawStatic();
    },

    // Topmost of MY shapes under the point — other people's layers are read
    // only, so they are deliberately not hit-testable.
    _hitTest(p) {
        const { w, h } = this._box();
        const tol = this.HIT_TOLERANCE;
        for (let i = this.scene.length - 1; i >= 0; i--) {
            const s = this.scene[i];
            if (this._shapeHit(s, p, tol, w, h)) return s;
        }
        return null;
    },

    _shapeHit(s, [x, y], tol, w, h) {
        // Compare in normalised space, but correct for the image's aspect
        // so the tolerance is a circle on screen rather than an ellipse.
        const ar = h / w || 1;
        const d2seg = (px, py, ax, ay, bx, by) => {
            const dx = bx - ax, dy = (by - ay) * ar, ppx = px - ax, ppy = (py - ay) * ar;
            const len = dx * dx + dy * dy;
            const t = len ? Math.max(0, Math.min(1, (ppx * dx + ppy * dy) / len)) : 0;
            return Math.hypot(ppx - t * dx, ppy - t * dy);
        };
        const reach = Math.max(tol, (s.w || 0) * 0.75);

        if (s.t === 'text') {
            const [tx, ty] = s.p[0];
            const fw = (s.fs || 0.03) * (s.txt || '').length * 0.55;
            return x >= tx - tol && x <= tx + fw + tol && y >= ty - (s.fs || 0.03) / ar - tol && y <= ty + tol;
        }
        if (s.t === 'rect' || s.t === 'ellipse') {
            const [a, b] = s.p;
            const x0 = Math.min(a[0], b[0]), x1 = Math.max(a[0], b[0]);
            const y0 = Math.min(a[1], b[1]), y1 = Math.max(a[1], b[1]);
            // Anywhere inside counts: an empty outline is fiddly to hit and
            // there is no fill to click instead.
            return x >= x0 - reach && x <= x1 + reach && y >= y0 - reach && y <= y1 + reach;
        }
        const pts = s.p;
        if (pts.length === 1) return d2seg(x, y, pts[0][0], pts[0][1], pts[0][0], pts[0][1]) <= reach;
        for (let i = 0; i < pts.length - 1; i++) {
            if (d2seg(x, y, pts[i][0], pts[i][1], pts[i + 1][0], pts[i + 1][1]) <= reach) return true;
        }
        return false;
    },

    /* ── Text ─────────────────────────────────────────────────────────── */

    // An overlaid input rather than a prompt(): the text has to be typed
    // where it will land, at the size and colour it will have, or placing it
    // accurately is guesswork.
    _beginText(p) {
        this._removeTextInput();
        const { w, h } = this._box();
        const fs = this.width * 6; // stroke width doubles as the size control
        const input = document.createElement('input');
        input.type = 'text';
        input.className = 'annot-text-input';
        input.style.left = (p[0] * w) + 'px';
        input.style.top = (p[1] * h - fs * w) + 'px';
        input.style.color = this.color;
        input.style.fontSize = (fs * w) + 'px';
        input.maxLength = 200;
        this._wrap.appendChild(input);
        this._textInput = input;
        setTimeout(() => input.focus(), 0);

        // One exit point for all three ways out — Enter, Escape, and losing
        // focus — because they overlap: removing the input steals focus and
        // fires blur synchronously, so whichever path runs first re-enters
        // this one. `done` makes the second arrival a no-op, and `keep` is
        // what makes Escape genuinely discard: without it the blur that
        // Escape itself causes would commit the very text being abandoned.
        let done = false;
        const finish = (keep) => {
            if (done) return;
            done = true;
            const txt = keep ? input.value.trim() : '';
            this._removeTextInput();
            if (!txt) return;
            this._pushHistory();
            this.scene.push({ id: this._newId(), t: 'text', c: this.color, w: this.width, fs, txt, p: [p] });
            this.redrawStatic();
            this._markDirty();
        };
        input.addEventListener('keydown', ev => {
            ev.stopPropagation(); // don't let the preview's arrow-key nav steal typing
            if (ev.key === 'Enter') { ev.preventDefault(); finish(true); }
            else if (ev.key === 'Escape') { ev.preventDefault(); finish(false); }
        });
        // Clicking away, or switching tools, keeps what was typed — the same
        // thing a text box anywhere else in the app does.
        input.addEventListener('blur', () => finish(true));
    },

    // The reference is cleared BEFORE the node is detached, not after:
    // removing a focused element fires blur synchronously, and that handler
    // calls back in here. Nulling first makes the re-entrant call find
    // nothing to do instead of trying to remove an already-detached node,
    // which throws.
    _removeTextInput() {
        const el = this._textInput;
        if (!el) return;
        this._textInput = null;
        el.remove();
    },

    /* ── Laser pointer ────────────────────────────────────────────────── */

    // Purely ephemeral — never enters the scene and never reaches the
    // server. It is a way to say "this bit, here" while someone is looking
    // at the same screen, not a mark on the drawing.
    _startLaser() {
        if (this._laserRAF) return;
        const step = () => {
            const now = performance.now();
            this._laser = this._laser.filter(pt => now - pt.t < this.LASER_LIFE);
            this._redrawLive();
            if (this._laser.length || this._drawing) this._laserRAF = requestAnimationFrame(step);
            else this._laserRAF = null;
        };
        this._laserRAF = requestAnimationFrame(step);
    },

    /* ── Rendering ────────────────────────────────────────────────────── */

    redrawStatic() {
        if (!this._mounted || !this._canvases) return;
        const { w, h } = this._box();
        if (!w || !h) return;
        const hl = this._canvases.hl.getContext('2d');
        const base = this._canvases.base.getContext('2d');
        hl.clearRect(0, 0, w, h);
        base.clearRect(0, 0, w, h);
        if (!this.visible) return;

        // Other people's markup sits underneath and is never selectable, so
        // that your own marks stay legible on top of a busy review.
        for (const layer of this.others) for (const s of layer.shapes) this._paint(s.t === 'hl' ? hl : base, s, w, h);
        for (const s of this.scene) this._paint(s.t === 'hl' ? hl : base, s, w, h);

        if (this.selectedId) {
            const sel = this.scene.find(s => s.id === this.selectedId);
            if (sel) this._paintSelection(base, sel, w, h);
        }
    },

    _scheduleLive() {
        if (this._liveRAF) return;
        this._liveRAF = requestAnimationFrame(() => { this._liveRAF = null; this._redrawLive(); });
    },

    _redrawLive() {
        if (!this._mounted || !this._canvases) return;
        const { w, h } = this._box();
        if (!w || !h) return;
        const ctx = this._canvases.live.getContext('2d');
        ctx.clearRect(0, 0, w, h);
        if (!this.visible) return;

        // A highlighter in progress goes onto the multiply canvas with the
        // committed ones, or it would look like a different tool until the
        // moment the pointer comes up.
        if (this._drawing && this._drawing.t === 'hl') {
            const hl = this._canvases.hl.getContext('2d');
            hl.clearRect(0, 0, w, h);
            for (const layer of this.others) for (const s of layer.shapes) if (s.t === 'hl') this._paint(hl, s, w, h);
            for (const s of this.scene) if (s.t === 'hl') this._paint(hl, s, w, h);
            this._paint(hl, this._drawing, w, h);
        } else if (this._drawing) {
            this._paint(ctx, this._drawing, w, h);
        }

        if (this._laser.length) this._paintLaser(ctx, w, h);
    },

    _paint(ctx, s, w, h) {
        const px = n => n * w, py = n => n * h;
        ctx.save();
        ctx.strokeStyle = s.c || '#ef4444';
        ctx.fillStyle = s.c || '#ef4444';
        ctx.lineWidth = Math.max(1, (s.w || 0.005) * w);
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        if (s.d) ctx.setLineDash([ctx.lineWidth * 2.5, ctx.lineWidth * 2.2]);

        if (s.t === 'hl') {
            // Wide, blunt and semi-transparent; the multiply blend on the
            // canvas element does the rest.
            ctx.lineWidth = Math.max(6, (s.w || 0.005) * w * 4);
            ctx.globalAlpha = 0.45;
            ctx.lineCap = 'butt';
        }

        const p = s.p || [];
        switch (s.t) {
            case 'pen':
            case 'hl':
                this._paintFreehand(ctx, p, px, py);
                break;
            case 'line':
            case 'arrow': {
                if (p.length < 2) break;
                ctx.beginPath();
                ctx.moveTo(px(p[0][0]), py(p[0][1]));
                ctx.lineTo(px(p[1][0]), py(p[1][1]));
                ctx.stroke();
                if (s.t === 'arrow') this._paintArrowhead(ctx, px(p[0][0]), py(p[0][1]), px(p[1][0]), py(p[1][1]));
                break;
            }
            case 'rect': {
                if (p.length < 2) break;
                ctx.strokeRect(px(p[0][0]), py(p[0][1]), px(p[1][0] - p[0][0]), py(p[1][1] - p[0][1]));
                break;
            }
            case 'ellipse': {
                if (p.length < 2) break;
                const cx = px((p[0][0] + p[1][0]) / 2), cy = py((p[0][1] + p[1][1]) / 2);
                const rx = Math.abs(px(p[1][0] - p[0][0]) / 2), ry = Math.abs(py(p[1][1] - p[0][1]) / 2);
                ctx.beginPath();
                ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
                ctx.stroke();
                break;
            }
            case 'text': {
                if (!p.length) break;
                const size = Math.max(9, (s.fs || 0.03) * w);
                ctx.setLineDash([]);
                ctx.font = `600 ${size}px ${getComputedStyle(document.body).fontFamily || 'sans-serif'}`;
                ctx.textBaseline = 'alphabetic';
                // A dark halo so red-on-red or white-on-white text is still
                // readable over an arbitrary photo.
                ctx.lineWidth = Math.max(2, size * 0.14);
                ctx.strokeStyle = 'rgba(0,0,0,.55)';
                ctx.strokeText(s.txt || '', px(p[0][0]), py(p[0][1]));
                ctx.fillText(s.txt || '', px(p[0][0]), py(p[0][1]));
                break;
            }
            default:
                // A shape drawn by a newer client than this one: skip it
                // rather than throwing and losing the whole layer.
                break;
        }
        ctx.restore();
    },

    // Quadratic curves through the midpoints of consecutive samples — the
    // standard way to turn a jittery pointer trail into a smooth line
    // without storing more points than were captured.
    _paintFreehand(ctx, p, px, py) {
        if (!p.length) return;
        if (p.length === 1) {
            ctx.beginPath();
            ctx.arc(px(p[0][0]), py(p[0][1]), ctx.lineWidth / 2, 0, Math.PI * 2);
            ctx.fill();
            return;
        }
        ctx.beginPath();
        ctx.moveTo(px(p[0][0]), py(p[0][1]));
        for (let i = 1; i < p.length - 1; i++) {
            const mx = (px(p[i][0]) + px(p[i + 1][0])) / 2;
            const my = (py(p[i][1]) + py(p[i + 1][1])) / 2;
            ctx.quadraticCurveTo(px(p[i][0]), py(p[i][1]), mx, my);
        }
        ctx.lineTo(px(p[p.length - 1][0]), py(p[p.length - 1][1]));
        ctx.stroke();
    },

    _paintArrowhead(ctx, x0, y0, x1, y1) {
        const ang = Math.atan2(y1 - y0, x1 - x0);
        const len = Math.max(8, ctx.lineWidth * 4);
        const spread = Math.PI / 7;
        ctx.setLineDash([]);
        ctx.beginPath();
        ctx.moveTo(x1, y1);
        ctx.lineTo(x1 - len * Math.cos(ang - spread), y1 - len * Math.sin(ang - spread));
        ctx.moveTo(x1, y1);
        ctx.lineTo(x1 - len * Math.cos(ang + spread), y1 - len * Math.sin(ang + spread));
        ctx.stroke();
    },

    _paintSelection(ctx, s, w, h) {
        const xs = s.p.map(q => q[0] * w), ys = s.p.map(q => q[1] * h);
        const pad = Math.max(8, (s.w || 0.005) * w);
        const x0 = Math.min(...xs) - pad, y0 = Math.min(...ys) - pad;
        const x1 = Math.max(...xs) + pad, y1 = Math.max(...ys) + pad;
        ctx.save();
        ctx.strokeStyle = '#3b82f6';
        ctx.lineWidth = 1.5;
        ctx.setLineDash([5, 4]);
        ctx.strokeRect(x0, y0, x1 - x0, y1 - y0);
        ctx.restore();
    },

    // A bright core inside a soft glow, both fading with age — reads as a
    // laser rather than as a thin red line.
    _paintLaser(ctx, w, h) {
        const now = performance.now();
        ctx.save();
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        for (let i = 1; i < this._laser.length; i++) {
            const age = (now - this._laser[i].t) / this.LASER_LIFE;
            const a = Math.max(0, 1 - age);
            const [x0, y0] = this._laser[i - 1].p, [x1, y1] = this._laser[i].p;
            ctx.globalAlpha = a * 0.35;
            ctx.strokeStyle = '#ff2d55';
            ctx.lineWidth = 14 * a + 4;
            ctx.beginPath(); ctx.moveTo(x0 * w, y0 * h); ctx.lineTo(x1 * w, y1 * h); ctx.stroke();
            ctx.globalAlpha = a;
            ctx.strokeStyle = '#fff';
            ctx.lineWidth = 3.5 * a + 1;
            ctx.beginPath(); ctx.moveTo(x0 * w, y0 * h); ctx.lineTo(x1 * w, y1 * h); ctx.stroke();
        }
        const head = this._laser[this._laser.length - 1];
        if (head) {
            ctx.globalAlpha = 1;
            ctx.fillStyle = '#ff2d55';
            ctx.shadowColor = '#ff2d55';
            ctx.shadowBlur = 18;
            ctx.beginPath(); ctx.arc(head.p[0] * w, head.p[1] * h, 5, 0, Math.PI * 2); ctx.fill();
            ctx.shadowBlur = 0;
            ctx.fillStyle = '#fff';
            ctx.beginPath(); ctx.arc(head.p[0] * w, head.p[1] * h, 2, 0, Math.PI * 2); ctx.fill();
        }
        ctx.restore();
    },

    /* ── Commands ─────────────────────────────────────────────────────── */

    setTool(t) {
        if (!this.TOOLS.includes(t)) return;
        this.tool = t;
        this.selectedId = null;
        this._removeTextInput();
        this._applyInteractivity();
        this.redrawStatic();
        PreviewPage.renderAnnotateBar();
    },

    setColor(c) { this.color = c; PreviewPage.renderAnnotateBar(); },
    setWidth(w) { this.width = w; PreviewPage.renderAnnotateBar(); },
    toggleDashed() { this.dashed = !this.dashed; PreviewPage.renderAnnotateBar(); },

    setEditing(on) {
        this.editing = on;
        // Drawing on markup you can't see makes no sense, so entering edit
        // mode turns visibility back on rather than silently doing nothing.
        if (on && !this.visible) this.visible = true;
        this.selectedId = null;
        this._removeTextInput();
        this._applyInteractivity();
        this.redrawStatic();
        PreviewPage.renderAnnotateBar();
    },

    // The show/hide checkbox. Hiding also drops out of edit mode: a hidden
    // canvas that still swallowed clicks would look like a broken image.
    setVisible(on) {
        this.visible = on;
        if (!on) { this.editing = false; this._removeTextInput(); }
        this._applyInteractivity();
        this.redrawStatic();
        this._redrawLive();
        PreviewPage.renderAnnotateBar();
    },

    undo() {
        if (!this._history.length) return;
        this._redo.push(JSON.parse(JSON.stringify(this.scene)));
        this.scene = this._history.pop();
        this.selectedId = null;
        this.redrawStatic();
        this._markDirty();
        PreviewPage.renderAnnotateBar();
    },

    redo() {
        if (!this._redo.length) return;
        this._history.push(JSON.parse(JSON.stringify(this.scene)));
        this.scene = this._redo.pop();
        this.selectedId = null;
        this.redrawStatic();
        this._markDirty();
        PreviewPage.renderAnnotateBar();
    },

    clearMine() {
        if (!this.scene.length) return;
        UI.confirmAction(I18N.t('annot.clear_title'), I18N.t('annot.clear_body'), I18N.t('common.delete'), () => {
            this._pushHistory();
            this.scene = [];
            this.selectedId = null;
            this.redrawStatic();
            this._markDirty();
            PreviewPage.renderAnnotateBar();
        });
    },

    _pushHistory() {
        this._history.push(JSON.parse(JSON.stringify(this.scene)));
        if (this._history.length > this.HISTORY_MAX) this._history.shift();
        this._redo = [];
    },

    _newId() { return Math.random().toString(36).slice(2, 10); },

    /* ── Saving ───────────────────────────────────────────────────────── */

    _markDirty() {
        this._dirty = true;
        PreviewPage.renderAnnotateBar();
        clearTimeout(this._saveTimer);
        this._saveTimer = setTimeout(() => this.flush(), this.SAVE_DEBOUNCE);
    },

    _pending: 0,
    _savePromise: null,

    // Sends the current layer. Three things here are load-bearing:
    //
    // The file id and the shapes are captured SYNCHRONOUSLY, before any
    // await. Saves are queued behind one another, so by the time a queued
    // one runs the user may already be looking at the next photo — reading
    // this.fileId at that point would file one image's markup under
    // another's.
    //
    // _dirty is cleared before the request rather than after, so a stroke
    // drawn while this one is in flight marks the layer dirty again and
    // earns its own save instead of being declared clean by a response that
    // predates it.
    //
    // Saves are serialised rather than fired in parallel: two PUTs racing to
    // the same row would let the older one land last.
    flush() {
        clearTimeout(this._saveTimer);
        this._saveTimer = null;
        if (!this._dirty || !this.fileId) return Promise.resolve();
        this._dirty = false;
        const fileId = this.fileId;
        const payload = JSON.parse(JSON.stringify(this.scene));
        this._pending++;
        PreviewPage.renderAnnotateBar();

        this._savePromise = (this._savePromise || Promise.resolve())
            .then(() => API.files.annotations.save(fileId, payload))
            .catch(e => {
                // Only re-arm for a retry if this is still the image on
                // screen; a failed save for one we have navigated away from
                // has no canvas left to retry from, and re-arming would make
                // the NEXT image inherit the failure.
                if (this.fileId === fileId) this._dirty = true;
                UI.toast((e && e.message) || I18N.t('annot.save_failed'), 'error');
            })
            .finally(() => {
                this._pending--;
                PreviewPage.renderAnnotateBar();
            });
        return this._savePromise;
    },

    isSaving() { return this._pending > 0; },
    isDirty() { return this._dirty; },

    _globalBound: false,
    // Bound once for the app's lifetime, same reasoning as PreviewPage's own
    // key handlers: this is a single-page app, so per-mount listeners would
    // stack up with every image opened.
    _bindGlobalOnce() {
        if (this._globalBound) return;
        this._globalBound = true;

        // Every way out of a preview that this module can observe. SPA
        // navigation has no unmount hook, so popstate stands in for the
        // browser's Back button and pagehide for closing the tab.
        window.addEventListener('popstate', () => this.flush());
        window.addEventListener('pagehide', () => this.flush());
        document.addEventListener('visibilitychange', () => { if (document.hidden) this.flush(); });

        document.addEventListener('keydown', ev => {
            if (!this._mounted || !this.editing) return;
            if (ev.target instanceof HTMLInputElement || ev.target instanceof HTMLTextAreaElement) return;
            const mod = ev.ctrlKey || ev.metaKey;
            if (mod && ev.key.toLowerCase() === 'z') {
                ev.preventDefault();
                ev.shiftKey ? this.redo() : this.undo();
            } else if (mod && ev.key.toLowerCase() === 'y') {
                ev.preventDefault();
                this.redo();
            } else if (ev.key === 'Delete' || ev.key === 'Backspace') {
                if (this.selectedId) { ev.preventDefault(); this.deleteSelected(); }
            } else if (ev.key === 'Escape') {
                this.selectedId = null;
                this.redrawStatic();
            } else {
                // Excalidraw's single-key tool shortcuts, same letters where
                // the tool exists in both.
                const keys = { v: 'select', p: 'pen', d: 'pen', h: 'hl', k: 'laser', a: 'arrow', l: 'line', r: 'rect', o: 'ellipse', t: 'text', e: 'eraser' };
                const t = keys[ev.key.toLowerCase()];
                if (t && !mod) { ev.preventDefault(); this.setTool(t); }
            }
        });
    },
};
