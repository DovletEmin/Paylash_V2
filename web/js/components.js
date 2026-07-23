/* Paylash — UI Components & Utilities */
const ESC_CHARS = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

// Named once so every is*()/icon lookup in UI reads from the same list
// instead of repeating its own inline array — the previous version had
// fileIconClass's "image" check and isImage() quietly listing different
// extensions (bmp/ico/tiff/tif were images to isImage() but not to the
// icon-color class), exactly the kind of drift this centralizes against.
// isThumbnailable still has to be hand-kept in sync with a SEPARATE list —
// the Go backend's own decode support (see isThumbnailableImage in
// internal/api/files.go) — that boundary can't be expressed in JS alone.
const EXT_ICON_MAP = { pdf:'📄',doc:'📝',docx:'📝',txt:'📃',odt:'📝',xls:'📊',xlsx:'📊',ods:'📊',csv:'📊',ppt:'📽',pptx:'📽',odp:'📽',jpg:'🖼',jpeg:'🖼',png:'🖼',gif:'🖼',webp:'🖼',svg:'🖼',mp3:'🎵',wav:'🎵',mp4:'🎬',avi:'🎬',mkv:'🎬',zip:'📦',rar:'📦','7z':'📦' };
const EXT_DOCUMENT = ['doc','docx','odt','txt','pdf','ppt','pptx','odp','xls','xlsx','ods','csv'];
const EXT_IMAGE = ['jpg','jpeg','png','gif','webp','svg','bmp','ico','tiff','tif'];
const EXT_THUMBNAILABLE = ['jpg','jpeg','png','gif'];
const EXT_AUDIO = ['mp3','wav','ogg','flac','aac','m4a','wma','opus'];
const EXT_VIDEO = ['mp4','webm','ogg','mov','avi','mkv','wmv','flv','m4v'];
const EXT_TEXT = ['txt','log','md','json','xml','yaml','yml','ini','cfg','conf','sh','bat','ps1',
                   'py','js','ts','go','java','c','cpp','h','hpp','css','html','htm','sql','env','toml'];
const EXT_COLLABORA_EDIT = ['doc','docx','odt','xls','xlsx','ods','ppt','pptx','odp','csv'];
const EXT_COLLABORA_VIEW = [...EXT_COLLABORA_EDIT, 'pdf'];

const UI = {
    toast(msg, type = 'info') {
        const c = document.getElementById('toast-container');
        const icons = { success: '✓', error: '✕', info: 'ℹ' };
        const el = document.createElement('div');
        el.className = `toast toast-${type}`;
        el.innerHTML = `<span class="toast-icon">${icons[type] || 'ℹ'}</span><span>${this.esc(msg)}</span>`;
        c.appendChild(el);
        setTimeout(() => { el.classList.add('toast-removing'); setTimeout(() => el.remove(), 250); }, 3500);
    },

    // title/hideClose describe the *last* modal shown — read by the
    // Escape/focus-trap keydown handler below, which is bound once for the
    // page's lifetime (see the SharesPage._bindDropdownClose /
    // PreviewPage._bindKeyNav pattern this follows) rather than per-open.
    _modalHideClose: false,
    _modalTriggerEl: null,
    _modalTitleSeq: 0,
    showModal(title, bodyHTML, footerHTML, hideClose) {
        const o = document.getElementById('modal-overlay');
        const titleId = 'modal-title-' + (this._modalTitleSeq++);
        o.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${titleId}">
            <div class="modal-header">
                <h3 class="modal-title" id="${titleId}">${this.esc(title)}</h3>
                ${hideClose ? '' : `<button class="modal-close" onclick="UI.closeModal()" aria-label="${this.esc(I18N.t('common.close'))}">✕</button>`}
            </div>
            <div class="modal-body">${bodyHTML}</div>
            ${footerHTML ? `<div class="modal-footer">${footerHTML}</div>` : ''}
        </div>`;
        o.classList.remove('hidden');
        this._modalHideClose = !!hideClose;
        // Only remembered on the *first* showModal of a nested sequence
        // (e.g. UI.confirmAction opened from inside an already-open custom
        // modal) — otherwise closing the confirm dialog would try to focus
        // an element that's about to be replaced by the modal underneath it
        // reopening, instead of the thing the user originally had focused.
        if (!document.getElementById('modal-overlay').classList.contains('visible')) {
            this._modalTriggerEl = document.activeElement;
        }
        this._bindModalKeydown();
        requestAnimationFrame(() => {
            o.classList.add('visible');
            const focusable = o.querySelector('input, textarea, select, button, [tabindex]');
            if (focusable) focusable.focus();
        });
    },

    _modalKeydownBound: false,
    _bindModalKeydown() {
        if (this._modalKeydownBound) return;
        this._modalKeydownBound = true;
        document.addEventListener('keydown', ev => {
            const o = document.getElementById('modal-overlay');
            if (!o.classList.contains('visible')) return;
            if (ev.key === 'Escape') {
                // A hideClose modal (forced password change) is mandatory —
                // Escape must not be a silent way around the same rule the
                // server now also enforces (see must_change_password).
                if (this._modalHideClose) return;
                ev.preventDefault();
                this.closeModal();
                return;
            }
            if (ev.key === 'Tab') {
                const focusables = Array.from(o.querySelectorAll('button, [href], input, select, textarea, [tabindex]'))
                    .filter(el => !el.disabled && el.tabIndex !== -1 && el.offsetParent !== null);
                if (!focusables.length) return;
                const first = focusables[0], last = focusables[focusables.length - 1];
                if (ev.shiftKey && document.activeElement === first) { ev.preventDefault(); last.focus(); }
                else if (!ev.shiftKey && document.activeElement === last) { ev.preventDefault(); first.focus(); }
            }
        });
    },

    closeModal() {
        const o = document.getElementById('modal-overlay');
        o.classList.remove('visible');
        setTimeout(() => { o.classList.add('hidden'); o.innerHTML = ''; }, 200);
        if (this._modalTriggerEl && typeof this._modalTriggerEl.focus === 'function') this._modalTriggerEl.focus();
        this._modalTriggerEl = null;
    },

    // Generic destructive-action confirmation — replaces window.confirm()
    // call sites so the prompt is styled and translatable like the rest of
    // the app instead of a browser-native dialog the app has no control
    // over. onConfirm may be async; the modal closes first so a slow
    // request doesn't leave the confirm button looking stuck.
    _confirmActionSeq: 0,
    confirmAction(title, bodyHTML, confirmLabel, onConfirm) {
        const cbName = '_confirmActionCb' + (this._confirmActionSeq++);
        this[cbName] = async () => { UI.closeModal(); delete UI[cbName]; await onConfirm(); };
        this.showModal(title, bodyHTML,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-danger" onclick="UI.${cbName}()">${confirmLabel}</button>`);
    },

    // Returns [x, y] to anchor a context menu at, from a click/contextmenu
    // event — falls back to the triggering element's own position when the
    // event was a keyboard-activated synthetic click (clientX/clientY are
    // both 0 there), so the menu opens near the button that opened it
    // instead of the page's top-left corner.
    eventPos(e) {
        if (e.clientX || e.clientY) return [e.clientX, e.clientY];
        const el = e.currentTarget || e.target;
        if (el && el.getBoundingClientRect) {
            const r = el.getBoundingClientRect();
            return [r.left, r.bottom];
        }
        return [0, 0];
    },

    _ctxMenuTriggerEl: null,
    showContextMenu(x, y, items) {
        const m = document.getElementById('context-menu');
        let html = '';
        for (const item of items) {
            if (item.divider) { html += '<div class="context-menu-divider" role="separator"></div>'; continue; }
            html += `<div class="context-menu-item${item.danger ? ' danger' : ''}" role="menuitem" tabindex="-1" data-action="${item.action}">${item.icon || ''} ${this.esc(item.label)}</div>`;
        }
        m.setAttribute('role', 'menu');
        m.innerHTML = html;
        m.style.left = Math.min(x, innerWidth - 180) + 'px';
        m.style.top = Math.min(y, innerHeight - 220) + 'px';
        m.classList.remove('hidden');
        this._ctxMenuTriggerEl = document.activeElement;
        const entries = m.querySelectorAll('.context-menu-item');
        entries.forEach(el => {
            el.addEventListener('click', () => {
                const itm = items.find(i => i.action === el.dataset.action);
                if (itm?.handler) itm.handler();
                this.hideContextMenu();
            });
        });
        entries[0]?.focus();
        this._bindContextMenuKeydown();
    },

    _ctxMenuKeydownBound: false,
    _bindContextMenuKeydown() {
        if (this._ctxMenuKeydownBound) return;
        this._ctxMenuKeydownBound = true;
        document.addEventListener('keydown', ev => {
            const m = document.getElementById('context-menu');
            if (m.classList.contains('hidden')) return;
            const entries = Array.from(m.querySelectorAll('.context-menu-item'));
            if (!entries.length) return;
            const idx = entries.indexOf(document.activeElement);
            if (ev.key === 'Escape') { ev.preventDefault(); this.hideContextMenu(); }
            else if (ev.key === 'ArrowDown') { ev.preventDefault(); (entries[idx + 1] || entries[0]).focus(); }
            else if (ev.key === 'ArrowUp') { ev.preventDefault(); (entries[idx - 1] || entries[entries.length - 1]).focus(); }
            else if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); document.activeElement?.click(); }
        });
    },

    hideContextMenu() {
        document.getElementById('context-menu').classList.add('hidden');
        if (this._ctxMenuTriggerEl && typeof this._ctxMenuTriggerEl.focus === 'function') this._ctxMenuTriggerEl.focus();
        this._ctxMenuTriggerEl = null;
    },

    passwordField(id, placeholder) {
        return `<div class="pw-field"><input type="password" id="${id}" class="form-control" placeholder="${this.esc(placeholder || '')}"><button type="button" class="pw-toggle" onclick="UI.togglePw('${id}')" aria-label="${this.esc(I18N.t('auth.toggle_password_visibility'))}">👁</button></div>`;
    },

    togglePw(id) {
        const inp = document.getElementById(id);
        if (!inp) return;
        inp.type = inp.type === 'password' ? 'text' : 'password';
    },

    // HTML-escapes a value for use in text content or a quoted attribute.
    // Escapes quotes too (unlike the old textContent/innerHTML trick this
    // replaced) — plain attribute values like title="${UI.esc(name)}" are
    // built by string concatenation throughout this codebase, and a
    // filename containing a bare " or ' could otherwise break out of the
    // attribute and inject new ones (a real stored-XSS vector via renames).
    esc(s) {
        if (s === null || s === undefined) return '';
        return String(s).replace(/[&<>"']/g, c => ESC_CHARS[c]);
    },

    // Safely embeds a JS value as an inline-handler argument, e.g.
    // onclick="Foo.bar(${UI.escJson(item)})". JSON.stringify produces valid
    // JS-literal syntax (and correctly backslash-escapes quotes inside
    // strings); the follow-up HTML-escape lets the result sit inside a
    // "-delimited attribute without the browser's HTML entity decoding
    // (which runs before the attribute is executed as JS) reintroducing a
    // raw quote that could break out of the JS literal.
    escJson(v) {
        return JSON.stringify(v).replace(/[&<>"']/g, c => ESC_CHARS[c]);
    },

    // Real profile photo when the person has one, falling back to an
    // initials circle otherwise — used everywhere another person shows up
    // (share dialogs, the shared-with-me/by-me groups, project members).
    // Previously every one of those spots hardcoded just the initials, so a
    // colleague's uploaded photo was only ever visible to themselves. There's
    // no cheap way to know up front whether userId has an avatar, so this
    // always points the <img> at /api/avatar/{id} and lets onerror (a 404
    // for someone with no photo) swap in the initials — same fallback
    // pattern as FilesPage.thumbError for file thumbnails.
    avatarHTML(userId, name, extraClass) {
        const initial = (name || '?').charAt(0).toUpperCase();
        const cls = ('share-user-avatar ' + (extraClass || '')).trim();
        if (!userId) return `<span class="${cls}">${this.esc(initial)}</span>`;
        return `<img class="${cls}" src="/api/avatar/${userId}" alt="${this.esc(initial)}" onerror="UI.avatarFallback(this)">`;
    },
    avatarFallback(img) {
        const span = document.createElement('span');
        span.className = img.className;
        span.textContent = img.alt || '?';
        img.replaceWith(span);
    },

    // Runs worker(item) for every item in `items`, at most `limit` at once —
    // used by bulk actions (move/delete/share many files) so a large
    // selection fires several requests in flight instead of awaiting them
    // one at a time, while still bounding how many the server sees
    // simultaneously (same idea as Uploader.CONCURRENCY for upload parts).
    // Returns [{item, error}, ...] for every item whose worker call threw —
    // one failure never stops the rest of the batch from running, matching
    // the existing per-item try/catch loops this replaces.
    async runPooled(items, limit, worker) {
        const errors = [];
        let cursor = 0;
        const runners = Array.from({ length: Math.max(1, Math.min(limit, items.length)) }, async () => {
            while (cursor < items.length) {
                const item = items[cursor++];
                try { await worker(item); }
                catch (error) { errors.push({ item, error }); }
            }
        });
        await Promise.all(runners);
        return errors;
    },

    // Flattens a scope's folder list (parent_id links) into a depth-ordered
    // array suitable for an indented <select> — used by the "move to" picker.
    flattenFolderTree(folders) {
        const byParent = {};
        for (const f of folders) {
            const key = f.parent_id || 'root';
            (byParent[key] = byParent[key] || []).push(f);
        }
        const lines = [];
        const visited = new Set();
        const walk = (parentKey, depth) => {
            const kids = (byParent[parentKey] || []).slice().sort((a, b) => a.name.localeCompare(b.name));
            for (const f of kids) {
                // Guards against hanging the tab if a parent_id cycle ever
                // made it into the data (shouldn't happen — the backend
                // rejects moves that would create one — but a walker with no
                // guard at all would infinite-loop instead of just being
                // wrong if one slipped through some other way).
                if (visited.has(f.id)) continue;
                visited.add(f.id);
                lines.push({ id: f.id, name: f.name, depth });
                walk(f.id, depth + 1);
            }
        };
        walk('root', 0);
        return lines;
    },

    // Shared double-click dispatch for non-folder items: preview media
    // inline, open office docs in Collabora, or fall back to a download.
    // Centralized so every file list (personal/project/common/admin
    // browsers/shares) makes this decision the same way instead of
    // duplicating the branch — and so item names never need to be
    // string-interpolated into inline JS handlers (see escJson above).
    openFile(id, name, size) {
        if (this.isMediaPreviewable(name)) return PreviewPage.open(id, name, size);
        if (this.isCollaboraViewable(name)) return EditorPage.open(id, name);
        return FilesPage.download(id, name);
    },

    formatBytes(b) {
        if (!b || b === 0) return '0 B';
        const u = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(b) / Math.log(1024));
        return (b / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + u[i];
    },

    formatDate(d) {
        if (!d) return '';
        const dt = new Date(d), now = new Date(), diff = Math.floor((now - dt) / 60000);
        if (diff < 1) return I18N.t('common.just_now');
        if (diff < 60) return I18N.t('common.minutes_ago', { n: diff });
        const h = Math.floor(diff / 60);
        if (h < 24) return I18N.t('common.hours_ago', { n: h });
        const days = Math.floor(h / 24);
        if (days < 7) return I18N.t('common.days_ago', { n: days });
        return dt.toLocaleDateString(I18N.dateLocale());
    },

    // Compact EN/RU/TK/TR switcher — used in the topbar (post-login shell)
    // and on the login/register screens (which render before App exists).
    langSwitcher() {
        const codes = I18N.SUPPORTED;
        return `<div class="lang-switcher" role="group" aria-label="${this.esc(I18N.t('app.language'))}">
            ${codes.map(c => `<button type="button" class="lang-btn ${I18N.lang === c ? 'active' : ''}" onclick="I18N.setLang('${c}')">${c.toUpperCase()}</button>`).join('')}
        </div>`;
    },

    fileIcon(name, isFolder) {
        if (isFolder) return '📁';
        const ext = name.split('.').pop().toLowerCase();
        return EXT_ICON_MAP[ext] || '📄';
    },

    fileIconClass(name, isFolder) {
        if (isFolder) return 'folder';
        const ext = name.split('.').pop().toLowerCase();
        if (EXT_DOCUMENT.includes(ext)) return 'document';
        if (EXT_IMAGE.includes(ext)) return 'image';
        return 'other';
    },

    isCollaboraEditable(name) {
        return EXT_COLLABORA_EDIT.includes(name.split('.').pop().toLowerCase());
    },

    isCollaboraViewable(name) {
        return EXT_COLLABORA_VIEW.includes(name.split('.').pop().toLowerCase());
    },

    isMediaPreviewable(name) {
        const ext = name.split('.').pop().toLowerCase();
        return this.isImage(ext) || this.isAudio(ext) || this.isVideo(ext) || this.isText(ext);
    },

    isImage(ext) { return EXT_IMAGE.includes(ext); },

    // Formats the server can actually decode and downscale into a cached
    // preview (see isThumbnailableImage in internal/api/files.go) — must be
    // kept in sync with that Go-side allowlist.
    isThumbnailable(ext) { return EXT_THUMBNAILABLE.includes(ext); },

    isAudio(ext) { return EXT_AUDIO.includes(ext); },

    isVideo(ext) { return EXT_VIDEO.includes(ext); },

    isText(ext) { return EXT_TEXT.includes(ext); },

    mediaType(name) {
        const ext = name.split('.').pop().toLowerCase();
        if (this.isImage(ext)) return 'image';
        if (this.isAudio(ext)) return 'audio';
        if (this.isVideo(ext)) return 'video';
        if (this.isText(ext)) return 'text';
        return null;
    },

    skeletonCards(n) {
        let h = '<div class="file-grid">';
        for (let i = 0; i < n; i++) h += '<div class="file-card"><div class="skeleton" style="width:40px;height:40px;margin-bottom:10px"></div><div class="skeleton" style="width:75%;height:12px;margin-bottom:4px"></div><div class="skeleton" style="width:45%;height:10px"></div></div>';
        return h + '</div>';
    },

    icons: {
        cloud:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>',
        folder:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>',
        share:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>',
        search:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
        grid:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>',
        list:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>',
        upload:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 16 12 12 8 16"/><line x1="12" y1="12" x2="12" y2="21"/><path d="M20.39 18.39A5 5 0 0 0 18 9h-1.26A8 8 0 1 0 3 16.3"/></svg>',
        settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
        logout:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>',
        edit:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>',
        trash:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>',
        download: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>',
        plus:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>',
        fileNew:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="12" y1="18" x2="12" y2="12"/><line x1="9" y1="15" x2="15" y2="15"/></svg>',
        back:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/></svg>',
        chevronLeft: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>',
        chevronRight: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>',
        menu:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="18" x2="21" y2="18"/></svg>',
        dashboard:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="9"/><rect x="14" y="3" width="7" height="5"/><rect x="14" y="12" width="7" height="9"/><rect x="3" y="16" width="7" height="5"/></svg>',
        school:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 10v6M2 10l10-5 10 5-10 5z"/><path d="M6 12v5c3 3 9 3 12 0v-5"/></svg>',
        book:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>',
        users:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>',
        user:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>',
        sun:      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>',
        moon:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>',
    }
};

document.addEventListener('click', () => UI.hideContextMenu());
