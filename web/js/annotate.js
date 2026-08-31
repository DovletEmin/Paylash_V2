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
    // Selection is a set, not one id: a marquee drag and a resize both act
    // on however many shapes were caught, and moving one of several has to
    // move all of them.
    selectedIds: new Set(),
    // Author ids whose layer is currently hidden. Separate from `visible`,
    // which is the master switch: with three people marking up one render,
    // "show me only what Meret asked for" is the question you actually have.
    hiddenAuthors: new Set(),

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
    // Last pointer position, kept so that pressing or releasing Shift can
    // re-apply the constraint to a drag already in progress without waiting
    // for the pointer to move again.
    _lastPt: null,

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
        this.selectedIds.clear();
        // Author visibility is per image: hiding a colleague's notes on one
        // render says nothing about the next one.
        this.hiddenAuthors.clear();
        PreviewPage._authorsOpen = false;
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
        live.addEventListener('dblclick', e => this._onDoubleClick(e));
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
        if (this.tool === 'select') { this._beginSelect(p, ev.shiftKey || ev.ctrlKey || ev.metaKey); return; }

        this._drawing = {
            id: this._newId(), t: this.tool, c: this.color, w: this.width,
            d: this.dashed ? 1 : 0, p: [p],
        };
        this._scheduleLive();
    },

    _onMove(ev) {
        if (!this.editing) return;
        const p = this._pt(ev);
        // Remembered so that pressing or releasing Shift mid-drag can redo
        // the constraint without waiting for the pointer to move again.
        this._lastPt = p;

        if (this.tool === 'laser' && this._laser.length) {
            this._laser.push({ p, t: performance.now() });
            return;
        }
        if (this.tool === 'eraser' && ev.buttons) { this._eraseAt(p, false); return; }
        if (this._dragging) {
            if (this._dragging.mode === 'move') this._moveSelection(p);
            else if (this._dragging.mode === 'resize') this._resizeSelection(p, ev.shiftKey);
            else if (this._dragging.mode === 'marquee') { this._dragging.to = p; this._scheduleLive(); }
            return;
        }
        if (!this._drawing) return;

        if (this._drawing.t === 'pen' || this._drawing.t === 'hl') {
            const last = this._drawing.p[this._drawing.p.length - 1];
            if (Math.hypot(p[0] - last[0], p[1] - last[1]) < this.MIN_POINT_GAP) return;
            this._drawing.p.push(p);
        } else {
            // Every other tool is defined by two corners; dragging just
            // moves the second one.
            this._drawing.p[1] = this._constrain(this._drawing.p[0], p, ev.shiftKey);
        }
        this._scheduleLive();
    },

    // Shift snaps a drag the way every drawing tool does: lines and arrows
    // to 15° increments, rectangles and ellipses to a true square or circle.
    //
    // The maths runs in SCREEN pixels, not normalised coordinates. The image
    // is rarely square, so x and y have different scales — constraining in
    // normalised space would produce a "square" that is visibly a rectangle.
    _constrain(a, b, on) {
        if (!on) return b;
        const { w, h } = this._box();
        const dx = (b[0] - a[0]) * w, dy = (b[1] - a[1]) * h;
        if (this.tool === 'line' || this.tool === 'arrow') {
            const step = Math.PI / 12; // 15°
            const ang = Math.round(Math.atan2(dy, dx) / step) * step;
            const len = Math.hypot(dx, dy);
            return [a[0] + (len * Math.cos(ang)) / w, a[1] + (len * Math.sin(ang)) / h];
        }
        const side = Math.max(Math.abs(dx), Math.abs(dy));
        return [a[0] + (Math.sign(dx || 1) * side) / w, a[1] + (Math.sign(dy || 1) * side) / h];
    },

    _onUp(ev) {
        if (!this.editing) return;
        try { this._canvases.live.releasePointerCapture(ev.pointerId); } catch { /* already released */ }

        if (this._dragging) {
            const mode = this._dragging.mode;
            if (mode === 'marquee') this._finishMarquee();
            this._dragging = null;
            this.redrawStatic();
            this._redrawLive();
            // A marquee only changed what is selected — nothing about the
            // drawing itself, so there is nothing to save.
            if (mode !== 'marquee') this._markDirty();
            PreviewPage.renderAnnotateBar();
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

    // Double-click re-opens a label for editing, whichever tool is armed —
    // the gesture is unambiguous and it is the only way back into a typo.
    _onDoubleClick(ev) {
        if (!this.editing) return;
        const hit = this._hitTest(this._pt(ev));
        if (!hit || hit.t !== 'text') return;
        ev.preventDefault();
        this._dragging = null;
        this.selectedIds.clear();
        this.redrawStatic();
        this._beginText(null, hit);
    },

    _onLeave() {
        // Leaving the canvas mid-laser should not freeze the dot on screen.
        if (this.tool === 'laser') this._laser = [];
    },

    /* ── Select and move ──────────────────────────────────────────────── */

    /* ── Selection, moving, resizing ──────────────────────────────────── */

    // Corners first, then edge midpoints — the painter and the hit test
    // index into the same list, so the order is part of the contract. A
    // coordinate of 0.5 means "this handle does not move that axis".
    HANDLES: [[0, 0], [1, 0], [1, 1], [0, 1], [0.5, 0], [1, 0.5], [0.5, 1], [0, 0.5]],
    HANDLE_PX: 9,
    // Below this a marquee drag was really just a click on empty space.
    MARQUEE_MIN: 0.008,

    _selectedShapes() { return this.scene.filter(s => this.selectedIds.has(s.id)); },

    // A shape's bounds in normalised coordinates. Text is the odd one out:
    // its anchor is the baseline-left of the string, so the box has to be
    // reconstructed from the measured advance width and the font size.
    _bbox(s) {
        if (s.t === 'text') {
            const { w, h } = this._box();
            const width = this._textWidth(s) / w;
            const height = (s.fs || 0.03) * w / h; // fs is normalised to WIDTH
            return { x0: s.p[0][0], y0: s.p[0][1] - height, x1: s.p[0][0] + width, y1: s.p[0][1] };
        }
        const xs = s.p.map(q => q[0]), ys = s.p.map(q => q[1]);
        return { x0: Math.min(...xs), y0: Math.min(...ys), x1: Math.max(...xs), y1: Math.max(...ys) };
    },

    _selectionBox() {
        const sel = this._selectedShapes();
        if (!sel.length) return null;
        return sel.reduce((b, s) => {
            const q = this._bbox(s);
            return { x0: Math.min(b.x0, q.x0), y0: Math.min(b.y0, q.y0), x1: Math.max(b.x1, q.x1), y1: Math.max(b.y1, q.y1) };
        }, this._bbox(sel[0]));
    },

    // Which resize handle is under the pointer, or null. Measured in screen
    // pixels so the grab area is the same size however the image is scaled.
    _handleHit(p) {
        const box = this._selectionBox();
        if (!box) return null;
        const { w, h } = this._box();
        for (let i = 0; i < this.HANDLES.length; i++) {
            const [hx, hy] = this.HANDLES[i];
            const cx = (box.x0 + (box.x1 - box.x0) * hx) * w;
            const cy = (box.y0 + (box.y1 - box.y0) * hy) * h;
            if (Math.hypot(p[0] * w - cx, p[1] * h - cy) <= this.HANDLE_PX) return i;
        }
        return null;
    },

    _beginSelect(p, additive) {
        // A handle takes priority over whatever is underneath it, or a
        // shape filling its own bounding box would be impossible to resize.
        const handle = this._handleHit(p);
        if (handle !== null) { this._beginResize(handle, p); return; }

        const hit = this._hitTest(p);
        if (hit) {
            if (additive) {
                if (this.selectedIds.has(hit.id)) this.selectedIds.delete(hit.id);
                else this.selectedIds.add(hit.id);
            } else if (!this.selectedIds.has(hit.id)) {
                // Clicking a shape that is already part of a multi-selection
                // must not collapse the selection — you are starting a drag
                // of the whole group.
                this.selectedIds = new Set([hit.id]);
            }
            if (this.selectedIds.size) this._beginMove(p);
        } else {
            if (!additive) this.selectedIds.clear();
            this._dragging = { mode: 'marquee', from: p, to: p };
        }
        this.redrawStatic();
        this._redrawLive();
    },

    _beginMove(p) {
        // Pushed here rather than on release: a move used to be the one
        // edit undo could not take back.
        this._pushHistory();
        this._dragging = {
            mode: 'move', from: p,
            orig: this._selectedShapes().map(s => ({ id: s.id, p: JSON.parse(JSON.stringify(s.p)) })),
        };
    },

    _beginResize(idx, p) {
        this._pushHistory();
        this._dragging = {
            mode: 'resize', idx, from: p, box: this._selectionBox(),
            orig: this._selectedShapes().map(s => ({ id: s.id, p: JSON.parse(JSON.stringify(s.p)), fs: s.fs })),
        };
    },

    _moveSelection(p) {
        const d = this._dragging;
        const dx = p[0] - d.from[0], dy = p[1] - d.from[1];
        for (const o of d.orig) {
            const s = this.scene.find(x => x.id === o.id);
            if (s) s.p = o.p.map(([x, y]) => [x + dx, y + dy]);
        }
        this.redrawStatic();
    },

    // Drags one handle and rescales every selected shape's points into the
    // new box. Working from the ORIGINAL geometry each frame (not the last
    // one) keeps a long drag from accumulating rounding error, and lets the
    // box be dragged back through itself and out the other side.
    _resizeSelection(p, shift) {
        const d = this._dragging, b = d.box;
        const [hx, hy] = this.HANDLES[d.idx];
        let x0 = b.x0, y0 = b.y0, x1 = b.x1, y1 = b.y1;
        if (hx === 0) x0 = p[0]; else if (hx === 1) x1 = p[0];
        if (hy === 0) y0 = p[1]; else if (hy === 1) y1 = p[1];

        const ow = b.x1 - b.x0, oh = b.y1 - b.y0;
        // A perfectly horizontal line has zero height, and a vertical one
        // zero width. Scaling by 0/0 would produce NaN and erase the shape,
        // so a collapsed axis is simply carried through unscaled.
        let sx = ow ? (x1 - x0) / ow : 1;
        let sy = oh ? (y1 - y0) / oh : 1;
        if (shift && hx !== 0.5 && hy !== 0.5) {
            // Corner handle with Shift: keep the aspect ratio by taking
            // whichever axis the pointer pushed further.
            const m = Math.max(Math.abs(sx), Math.abs(sy));
            sx = Math.sign(sx || 1) * m;
            sy = Math.sign(sy || 1) * m;
            if (hx === 0) x0 = b.x1 - ow * sx; else x1 = b.x0 + ow * sx;
            if (hy === 0) y0 = b.y1 - oh * sy; else y1 = b.y0 + oh * sy;
        }
        const ax = hx === 0 ? x1 : x0, ay = hy === 0 ? y1 : y0;   // anchored edge
        const bx = hx === 0 ? b.x1 : b.x0, by = hy === 0 ? b.y1 : b.y0;

        for (const o of d.orig) {
            const s = this.scene.find(x => x.id === o.id);
            if (!s) continue;
            s.p = o.p.map(([x, y]) => [ax + (x - bx) * sx, ay + (y - by) * sy]);
            // Text has no geometry beyond its anchor, so it is the font that
            // has to grow. Width drives it, matching how fs is normalised.
            if (s.t === 'text' && o.fs) s.fs = Math.max(0.004, o.fs * Math.abs(sx || 1));
        }
        this.redrawStatic();
    },

    _finishMarquee() {
        const d = this._dragging;
        const x0 = Math.min(d.from[0], d.to[0]), x1 = Math.max(d.from[0], d.to[0]);
        const y0 = Math.min(d.from[1], d.to[1]), y1 = Math.max(d.from[1], d.to[1]);
        if (x1 - x0 < this.MARQUEE_MIN && y1 - y0 < this.MARQUEE_MIN) return; // a click, not a drag
        for (const s of this.scene) {
            const b = this._bbox(s);
            // Touching counts, the way it does in most drawing tools —
            // requiring full containment makes long strokes hard to catch.
            if (b.x1 >= x0 && b.x0 <= x1 && b.y1 >= y0 && b.y0 <= y1) this.selectedIds.add(s.id);
        }
    },

    selectAll() {
        this.selectedIds = new Set(this.scene.map(s => s.id));
        this.redrawStatic();
        PreviewPage.renderAnnotateBar();
    },

    deleteSelected() {
        if (!this.selectedIds.size) return;
        this._pushHistory();
        this.scene = this.scene.filter(s => !this.selectedIds.has(s.id));
        this.selectedIds.clear();
        this.redrawStatic();
        this._markDirty();
        PreviewPage.renderAnnotateBar();
    },

    duplicateSelected() {
        const sel = this._selectedShapes();
        if (!sel.length) return;
        this._pushHistory();
        const off = 0.02;
        const copies = sel.map(s => Object.assign(JSON.parse(JSON.stringify(s)), {
            id: this._newId(),
            p: s.p.map(([x, y]) => [x + off, y + off]),
        }));
        this.scene.push(...copies);
        this.selectedIds = new Set(copies.map(c => c.id));
        this.redrawStatic();
        this._markDirty();
    },

    // Arrow-key nudge. A plain press moves by a pixel's worth, Shift by ten
    // — the same muscle memory as every other drawing tool.
    nudgeSelected(dx, dy, big) {
        if (!this.selectedIds.size) return;
        const { w, h } = this._box();
        const step = big ? 10 : 1;
        this._pushHistory();
        for (const s of this._selectedShapes()) {
            s.p = s.p.map(([x, y]) => [x + (dx * step) / w, y + (dy * step) / h]);
        }
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
    // `existing` re-opens a text shape that is already on the canvas, so a
    // typo does not mean deleting it and starting again. Committing then
    // replaces it in place rather than appending a second one.
    _beginText(p, existing) {
        this._removeTextInput();
        const { w, h } = this._box();
        const fs = existing ? (existing.fs || 0.03) : this.width * 6; // stroke width doubles as the size control
        const colour = existing ? existing.c : this.color;
        if (existing) p = existing.p[0];
        const input = document.createElement('input');
        input.type = 'text';
        input.className = 'annot-text-input';
        input.value = existing ? (existing.txt || '') : '';
        input.style.left = (p[0] * w) + 'px';
        input.style.top = (p[1] * h - fs * w) + 'px';
        input.style.color = colour;
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
            if (existing) {
                // Emptying an existing label deletes it — that is what
                // clearing the box and pressing Enter plainly means.
                if (txt === (existing.txt || '')) return;
                this._pushHistory();
                if (!txt) this.scene = this.scene.filter(x => x.id !== existing.id);
                else existing.txt = txt;
                this.redrawStatic();
                this._markDirty();
                return;
            }
            if (!txt) return;
            this._pushHistory();
            this.scene.push({ id: this._newId(), t: 'text', c: colour, w: this.width, fs, txt, p: [p] });
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
        // that your own marks stay legible on top of a busy review. Either
        // layer can be switched off by author (see hiddenAuthors).
        for (const layer of this.others) {
            if (this.hiddenAuthors.has(layer.user_id)) continue;
            for (const s of layer.shapes) this._paint(s.t === 'hl' ? hl : base, s, w, h);
        }
        if (!this.hiddenAuthors.has(this.myId())) {
            for (const s of this.scene) this._paint(s.t === 'hl' ? hl : base, s, w, h);
            if (this.selectedIds.size) this._paintSelection(base, w, h);
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

        if (this._dragging && this._dragging.mode === 'marquee') this._paintMarquee(ctx, w, h);
        if (this._laser.length) this._paintLaser(ctx, w, h);
    },

    // The signed-in user's id, used to key their own layer in the author
    // list. -1 when there is somehow no user, which simply never matches.
    myId() { return App.user ? App.user.id : -1; },

    _font(sizePx) {
        return `600 ${sizePx}px ${getComputedStyle(document.body).fontFamily || 'sans-serif'}`;
    },

    // Measured, not estimated: a text shape's selection box and its resize
    // handles have to sit on the glyphs actually painted, and character
    // counts are hopeless across Cyrillic, Latin and Turkmen diacritics.
    _textWidth(s) {
        if (!this._canvases) return 0;
        const { w } = this._box();
        const ctx = this._canvases.base.getContext('2d');
        ctx.save();
        ctx.font = this._font(Math.max(9, (s.fs || 0.03) * w));
        const width = ctx.measureText(s.txt || '').width;
        ctx.restore();
        return width;
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
                ctx.font = this._font(size);
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

    // The selection outline plus the eight handles that resize it. Drawn on
    // the static canvas because it only changes when the selection does.
    _paintSelection(ctx, w, h) {
        const box = this._selectionBox();
        if (!box) return;
        const x0 = box.x0 * w, y0 = box.y0 * h, x1 = box.x1 * w, y1 = box.y1 * h;
        ctx.save();
        ctx.strokeStyle = '#3b82f6';
        ctx.lineWidth = 1.5;
        ctx.setLineDash([5, 4]);
        ctx.strokeRect(x0, y0, x1 - x0, y1 - y0);

        ctx.setLineDash([]);
        const r = this.HANDLE_PX / 2;
        for (const [hx, hy] of this.HANDLES) {
            const cx = x0 + (x1 - x0) * hx, cy = y0 + (y1 - y0) * hy;
            ctx.fillStyle = '#fff';
            ctx.fillRect(cx - r, cy - r, r * 2, r * 2);
            ctx.lineWidth = 1.5;
            ctx.strokeRect(cx - r, cy - r, r * 2, r * 2);
        }
        ctx.restore();
    },

    // The rubber band, on the live canvas: it follows the pointer every
    // frame and vanishes on release, so it never belongs with the scene.
    _paintMarquee(ctx, w, h) {
        const d = this._dragging;
        const x0 = Math.min(d.from[0], d.to[0]) * w, x1 = Math.max(d.from[0], d.to[0]) * w;
        const y0 = Math.min(d.from[1], d.to[1]) * h, y1 = Math.max(d.from[1], d.to[1]) * h;
        ctx.save();
        ctx.fillStyle = 'rgba(59,130,246,.12)';
        ctx.fillRect(x0, y0, x1 - x0, y1 - y0);
        ctx.strokeStyle = '#3b82f6';
        ctx.lineWidth = 1;
        ctx.setLineDash([4, 3]);
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
        this.selectedIds.clear();
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
        this.selectedIds.clear();
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
        this.selectedIds.clear();
        this.redrawStatic();
        this._markDirty();
        PreviewPage.renderAnnotateBar();
    },

    redo() {
        if (!this._redo.length) return;
        this._history.push(JSON.parse(JSON.stringify(this.scene)));
        this.scene = this._redo.pop();
        this.selectedIds.clear();
        this.redrawStatic();
        this._markDirty();
        PreviewPage.renderAnnotateBar();
    },

    clearMine() {
        if (!this.scene.length) return;
        UI.confirmAction(I18N.t('annot.clear_title'), I18N.t('annot.clear_body'), I18N.t('common.delete'), () => {
            this._pushHistory();
            this.scene = [];
            this.selectedIds.clear();
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
            } else if (mod && ev.key.toLowerCase() === 'd') {
                ev.preventDefault();
                this.duplicateSelected();
            } else if (mod && ev.key.toLowerCase() === 'a') {
                ev.preventDefault();
                this.setTool('select');
                this.selectAll();
            } else if (ev.key === 'Delete' || ev.key === 'Backspace') {
                if (this.selectedIds.size) { ev.preventDefault(); this.deleteSelected(); }
            } else if (ev.key.startsWith('Arrow') && this.selectedIds.size) {
                // Only claimed while something is selected — otherwise the
                // arrows still step between photos, as they always have.
                ev.preventDefault();
                const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[ev.key];
                if (d) this.nudgeSelected(d[0], d[1], ev.shiftKey);
            } else if (ev.key === 'Escape') {
                this.selectedIds.clear();
                this.redrawStatic();
            } else {
                // Excalidraw's single-key tool shortcuts, same letters where
                // the tool exists in both.
                const keys = { v: 'select', p: 'pen', d: 'pen', h: 'hl', k: 'laser', a: 'arrow', l: 'line', r: 'rect', o: 'ellipse', t: 'text', e: 'eraser' };
                const t = keys[ev.key.toLowerCase()];
                if (t && !mod) { ev.preventDefault(); this.setTool(t); }
            }
        });

        // Shift changes what the current drag means, so pressing or
        // releasing it has to re-evaluate immediately — otherwise the shape
        // only snaps once the pointer happens to move again.
        const reapply = ev => {
            if (!this._mounted || !this._drawing || !this._lastPt) return;
            if (ev.key !== 'Shift') return;
            if (this._drawing.t === 'pen' || this._drawing.t === 'hl') return;
            this._drawing.p[1] = this._constrain(this._drawing.p[0], this._lastPt, ev.type === 'keydown');
            this._scheduleLive();
        };
        document.addEventListener('keydown', reapply);
        document.addEventListener('keyup', reapply);
    },

    /* ── Authors ──────────────────────────────────────────────────────── */

    // Everyone with markup on this image, mine first — the order the legend
    // is drawn in and the order the layers are painted in agree, so the list
    // reads top-to-bottom the way the marks stack.
    authors() {
        const list = [];
        if (this.scene.length) list.push({ id: this.myId(), name: I18N.t('annot.you'), count: this.scene.length, mine: true });
        for (const o of this.others) {
            if (o.shapes.length) list.push({ id: o.user_id, name: o.user_name, count: o.shapes.length, mine: false });
        }
        return list;
    },

    toggleAuthor(userId) {
        if (this.hiddenAuthors.has(userId)) this.hiddenAuthors.delete(userId);
        else {
            this.hiddenAuthors.add(userId);
            // Hiding your own layer must not leave an invisible selection
            // that Delete would still act on.
            if (userId === this.myId()) this.selectedIds.clear();
        }
        this.redrawStatic();
        PreviewPage.renderAnnotateBar();
    },

    /* ── Export ───────────────────────────────────────────────────────── */

    // Renders the photo and every visible layer into an offscreen canvas at
    // the image's NATIVE resolution and hands back a PNG. Native, not the
    // on-screen size, because the point of exporting is to send the result
    // to someone outside the app or to print it — and because normalised
    // coordinates mean the same shapes redraw at any resolution for free.
    //
    // The image comes from this app's own origin, so the canvas is not
    // tainted and toBlob is allowed.
    async exportPNG() {
        if (!this._mounted || !this._img) return;
        const W = this._img.naturalWidth || this._img.clientWidth;
        const H = this._img.naturalHeight || this._img.clientHeight;
        if (!W || !H) return;

        const canvas = document.createElement('canvas');
        canvas.width = W;
        canvas.height = H;
        const ctx = canvas.getContext('2d');
        ctx.drawImage(this._img, 0, 0, W, H);

        const layers = [];
        for (const o of this.others) if (!this.hiddenAuthors.has(o.user_id)) layers.push(o.shapes);
        if (!this.hiddenAuthors.has(this.myId())) layers.push(this.scene);

        // Highlighter first and in multiply, mirroring the on-screen stack:
        // on screen that blend is a property of its own canvas element, here
        // it is a compositing mode on the one canvas.
        ctx.globalCompositeOperation = 'multiply';
        for (const shapes of layers) for (const sh of shapes) if (sh.t === 'hl') this._paint(ctx, sh, W, H);
        ctx.globalCompositeOperation = 'source-over';
        for (const shapes of layers) for (const sh of shapes) if (sh.t !== 'hl') this._paint(ctx, sh, W, H);

        const blob = await new Promise(res => canvas.toBlob(res, 'image/png'));
        if (!blob) { UI.toast(I18N.t('annot.export_failed'), 'error'); return; }
        const base = (PreviewPage.currentFileName || 'image').replace(/\.[^.]+$/, '');
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${base} — ${I18N.t('annot.export_suffix')}.png`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        // Revoked on the next tick rather than immediately: some browsers
        // have not started reading the blob when click() returns.
        setTimeout(() => URL.revokeObjectURL(url), 10000);
        UI.toast(I18N.t('annot.export_done'), 'success');
    },
};
