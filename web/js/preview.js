/* Paylash — Media Preview Page */
const PreviewPage = {
    currentFileId: null,
    currentFileName: '',
    currentFileSize: 0,
    // Text preview fetches the whole file into a JS string to render it —
    // fine for a config file or log, not for something CAD/render-scale.
    // Above this, point the user at Download instead of hanging the tab.
    TEXT_PREVIEW_LIMIT: 5 * 1024 * 1024,

    render() {
        return `
        <div class="editor-page">
            <div class="editor-toolbar">
                <button class="btn btn-ghost btn-sm" onclick="App.navigate('files')">${I18N.t('editor.back')}</button>
                <span class="editor-filename" id="preview-filename">${UI.esc(this.currentFileName)}</span>
                <div class="editor-toolbar-right">
                    <button class="btn btn-ghost btn-sm" id="comments-toggle-btn" onclick="PreviewPage.toggleComments()">💬 ${I18N.t('comments.toggle')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="PreviewPage.download()">${UI.icons.download} ${I18N.t('files.action_download')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="SharesPage.showShareModal({id:PreviewPage.currentFileId,name:PreviewPage.currentFileName})">${UI.icons.share} ${I18N.t('files.action_share')}</button>
                </div>
            </div>
            <div class="preview-body">
                <div class="preview-container" id="preview-container">
                    <div class="editor-loading"><div class="spinner"></div><p>${I18N.t('common.loading')}</p></div>
                </div>
                <div class="comments-panel hidden" id="comments-panel"></div>
            </div>
        </div>`;
    },

    async open(fileId, fileName, size) {
        this.currentFileId = fileId;
        this.currentFileName = fileName;
        this.currentFileSize = size || 0;
        this._pendingPin = null;
        App.navigate('preview');
    },

    // Finds whichever currently-loaded file list contains the file being
    // previewed (the folder/scope the user was actually browsing) so the
    // side arrows and arrow keys can step through the same set of images —
    // scoped per source so switching photos never crosses into an unrelated
    // list.  Returns [] (no navigation UI) when the file isn't part of any
    // list still held in memory, e.g. opened straight from a search result.
    _findSiblings() {
        const lists = [];
        if (typeof FilesPage !== 'undefined' && Array.isArray(FilesPage.files)) lists.push(FilesPage.files);
        if (typeof AdminPage !== 'undefined' && AdminPage._adminBrowser && Array.isArray(AdminPage._adminBrowser.files)) {
            lists.push(AdminPage._adminBrowser.files);
        }
        for (const list of lists) {
            if (list.some(f => f.id === this.currentFileId)) {
                return list.filter(f => UI.mediaType(f.name) === 'image');
            }
        }
        return [];
    },

    navArrowHTML(dir) {
        const siblings = this._findSiblings();
        const idx = siblings.findIndex(f => f.id === this.currentFileId);
        if (idx === -1 || siblings.length < 2) return '';
        const canGo = dir === 'prev' ? idx > 0 : idx < siblings.length - 1;
        const label = I18N.t(dir === 'prev' ? 'preview.nav_prev' : 'preview.nav_next');
        const glyph = dir === 'prev' ? UI.icons.chevronLeft : UI.icons.chevronRight;
        if (!canGo) return `<button class="preview-nav-arrow preview-nav-${dir}" disabled aria-label="${label}">${glyph}</button>`;
        return `<button class="preview-nav-arrow preview-nav-${dir}" onclick="PreviewPage.navigateSibling(${dir === 'prev' ? -1 : 1})" aria-label="${label}">${glyph}</button>`;
    },

    navigateSibling(delta) {
        const siblings = this._findSiblings();
        const idx = siblings.findIndex(f => f.id === this.currentFileId);
        if (idx === -1) return;
        const next = siblings[idx + delta];
        if (!next) return;
        this.currentFileId = next.id;
        this.currentFileName = next.name;
        this.currentFileSize = next.size_bytes || 0;
        this._pendingPin = null;
        this.init();
    },

    _keyNavBound: false,
    // Registered once for the page's whole lifetime (this is a single-page
    // app — the script never reloads) rather than per-open/per-init, so
    // repeatedly opening previews never stacks up duplicate listeners; the
    // handler itself checks it's actually the active page before acting.
    _bindKeyNav() {
        if (this._keyNavBound) return;
        this._keyNavBound = true;
        document.addEventListener('keydown', ev => {
            if (App.currentPage !== 'preview' || UI.mediaType(PreviewPage.currentFileName) !== 'image') return;
            if (ev.key === 'ArrowLeft') { ev.preventDefault(); PreviewPage.navigateSibling(-1); }
            else if (ev.key === 'ArrowRight') { ev.preventDefault(); PreviewPage.navigateSibling(1); }
        });
    },

    async init() {
        if (!this.currentFileId) { App.navigate('files'); return; }
        this._bindKeyNav();
        const nameEl = document.getElementById('preview-filename');
        if (nameEl) nameEl.textContent = this.currentFileName;
        const c = document.getElementById('preview-container');
        const type = UI.mediaType(this.currentFileName);
        const url = `/api/files/${this.currentFileId}/download`;

        if (type === 'image') {
            c.innerHTML = `<div class="preview-media preview-image" id="preview-image-wrap">
                ${this.navArrowHTML('prev')}
                <img src="${url}" alt="${UI.esc(this.currentFileName)}" onclick="PreviewPage.onImageClick(event,this)">
                <div class="comment-pins" id="comment-pins"></div>
                ${this.navArrowHTML('next')}
            </div>`;
        } else if (type === 'audio') {
            c.innerHTML = `<div class="preview-media preview-audio">
                <div class="preview-audio-icon">🎵</div>
                <div class="preview-audio-name">${UI.esc(this.currentFileName)}</div>
                <audio controls autoplay src="${url}" style="width:100%;max-width:500px"></audio>
            </div>`;
        } else if (type === 'video') {
            c.innerHTML = `<div class="preview-media preview-video"><video controls autoplay preload="metadata"><source src="${url}" type="${this.guessMime()}">${I18N.t('preview.video_unsupported')}</video></div>`;
        } else if (type === 'text') {
            if (this.currentFileSize > this.TEXT_PREVIEW_LIMIT) {
                c.innerHTML = `<div class="empty-state"><p>${I18N.t('preview.too_large', { size: UI.formatBytes(this.currentFileSize) })}</p><p class="text-muted">${I18N.t('preview.try_download')}</p><button class="btn btn-primary btn-sm" style="margin-top:10px" onclick="PreviewPage.download()">${UI.icons.download} ${I18N.t('files.action_download')}</button></div>`;
                return;
            }
            try {
                const res = await fetch(url, { credentials: 'same-origin' });
                if (!res.ok) throw new Error(I18N.t('preview.load_failed'));
                const text = await res.text();
                c.innerHTML = `<div class="preview-media preview-text"><pre class="preview-code">${UI.esc(text)}</pre></div>`;
            } catch (e) {
                c.innerHTML = `<div class="empty-state"><p>${I18N.t('preview.file_load_failed')}</p><p class="text-muted">${UI.esc(e.message)}</p></div>`;
            }
        } else {
            c.innerHTML = `<div class="empty-state"><p>${I18N.t('preview.unsupported_type')}</p></div>`;
        }

        if (this._commentsOpen) this.loadComments();
    },

    guessMime() {
        const ext = this.currentFileName.split('.').pop().toLowerCase();
        const map = { mp4:'video/mp4', webm:'video/webm', ogg:'video/ogg', mov:'video/mp4', m4v:'video/mp4' };
        return map[ext] || 'video/mp4';
    },

    toggleZoom(img) {
        img.classList.toggle('zoomed');
    },

    // Clicking the image normally zooms it — but while placing a pinned
    // comment (see togglePinMode), the same click instead records where on
    // the image the pin should sit.
    onImageClick(ev, img) {
        if (this._pinMode) {
            const rect = img.getBoundingClientRect();
            const xPct = Math.max(0, Math.min(100, ((ev.clientX - rect.left) / rect.width) * 100));
            const yPct = Math.max(0, Math.min(100, ((ev.clientY - rect.top) / rect.height) * 100));
            this._pendingPin = { x: xPct, y: yPct };
            this._pinMode = false;
            img.classList.remove('pin-mode');
            this.renderPins();
            this.renderCommentsPanel();
            document.getElementById('comment-body-input')?.focus();
            return;
        }
        this.toggleZoom(img);
    },

    download() { if (this.currentFileId) FilesPage.download(this.currentFileId, this.currentFileName); },

    /* ── Comments (review notes, optionally pinned to a point on an image) ── */

    _commentsOpen: false,
    _comments: [],
    _pinMode: false,
    _pendingPin: null,

    toggleComments() {
        this._commentsOpen = !this._commentsOpen;
        const panel = document.getElementById('comments-panel');
        const btn = document.getElementById('comments-toggle-btn');
        if (!panel) return;
        panel.classList.toggle('hidden', !this._commentsOpen);
        if (btn) btn.classList.toggle('active', this._commentsOpen);
        if (this._commentsOpen) this.loadComments();
    },

    async loadComments() {
        if (!this.currentFileId) return;
        try { this._comments = (await API.files.comments.list(this.currentFileId)) || []; }
        catch { this._comments = []; }
        this.renderCommentsPanel();
        this.renderPins();
    },

    renderCommentsPanel() {
        const panel = document.getElementById('comments-panel');
        if (!panel || !this._commentsOpen) return;
        const isImage = UI.mediaType(this.currentFileName) === 'image';
        const list = this._comments.map((c, i) => this.commentItemHTML(c, i)).join('')
            || `<p class="text-muted comments-empty">${I18N.t('comments.empty')}</p>`;
        const pinHint = this._pendingPin
            ? `<div class="comment-pin-hint">📍 ${I18N.t('comments.pin_placed')} <button class="btn btn-ghost btn-sm" onclick="PreviewPage.cancelPin()">${I18N.t('common.cancel')}</button></div>`
            : '';
        panel.innerHTML = `
            <div class="comments-panel-header">
                <h4>${I18N.t('comments.title')} (${this._comments.length})</h4>
                ${isImage ? `<button class="btn btn-icon btn-ghost btn-sm ${this._pinMode ? 'active' : ''}" onclick="PreviewPage.togglePinMode()" title="${I18N.t('comments.add_pin')}" aria-label="${I18N.t('comments.add_pin')}">📍</button>` : ''}
            </div>
            <div class="comments-list">${list}</div>
            <div class="comment-composer">
                ${pinHint}
                <textarea id="comment-body-input" class="form-control" rows="2" placeholder="${I18N.t('comments.placeholder')}"></textarea>
                <button class="btn btn-primary btn-sm" onclick="PreviewPage.submitComment()">${I18N.t('comments.post')}</button>
            </div>`;
    },

    commentItemHTML(c, i) {
        const canDelete = App.user && (App.user.id === c.user_id || App.user.role === 'admin');
        const pinBadge = c.x_pct != null ? `<span class="comment-pin-badge" title="${I18N.t('comments.pinned')}">${i + 1}</span>` : '';
        return `<div class="comment-item" id="comment-item-${c.id}" ${c.x_pct != null ? `onclick="PreviewPage.highlightPin(${c.id})"` : ''}>
            ${UI.avatarHTML(c.user_id, c.user_name, 'share-user-avatar-sm')}
            <div class="comment-item-body">
                <div class="comment-item-head"><strong>${UI.esc(c.user_name)}</strong>${pinBadge} <span class="text-muted">${UI.formatDate(c.created_at)}</span></div>
                <div class="comment-item-text">${UI.esc(c.body)}</div>
            </div>
            ${canDelete ? `<button class="btn btn-icon btn-sm comment-delete" onclick="event.stopPropagation();PreviewPage.deleteComment(${c.id})" title="${I18N.t('common.delete')}" aria-label="${I18N.t('common.delete')}">✕</button>` : ''}
        </div>`;
    },

    renderPins() {
        const el = document.getElementById('comment-pins');
        if (!el) return;
        const pinned = this._comments
            .map((c, i) => ({ ...c, num: i + 1 }))
            .filter(c => c.x_pct != null && c.y_pct != null);
        el.innerHTML = pinned.map(c => `
            <button class="comment-pin" style="left:${c.x_pct}%;top:${c.y_pct}%" onclick="PreviewPage.highlightPin(${c.id})" title="${UI.esc(c.user_name)}">${c.num}</button>
        `).join('') + (this._pendingPin ? `<span class="comment-pin comment-pin-pending" style="left:${this._pendingPin.x}%;top:${this._pendingPin.y}%"></span>` : '');
    },

    highlightPin(commentId) {
        if (!this._commentsOpen) this.toggleComments();
        const el = document.getElementById('comment-item-' + commentId);
        if (!el) return;
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        el.classList.add('flash');
        setTimeout(() => el.classList.remove('flash'), 900);
    },

    togglePinMode() {
        this._pinMode = !this._pinMode;
        document.querySelector('#preview-image-wrap img')?.classList.toggle('pin-mode', this._pinMode);
        this.renderCommentsPanel();
    },

    cancelPin() {
        this._pendingPin = null;
        this.renderPins();
        this.renderCommentsPanel();
    },

    async submitComment() {
        const ta = document.getElementById('comment-body-input');
        const body = ta ? ta.value.trim() : '';
        if (!body) return;
        try {
            const pin = this._pendingPin;
            const created = await API.files.comments.create(this.currentFileId, body, pin ? pin.x : null, pin ? pin.y : null);
            this._comments.push(created);
            this._pendingPin = null;
            this.renderCommentsPanel();
            this.renderPins();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    deleteComment(id) {
        UI.confirmAction(I18N.t('comments.delete_title'), I18N.t('comments.delete_body'), I18N.t('common.delete'), async () => {
            try {
                await API.files.comments.delete(this.currentFileId, id);
                this._comments = this._comments.filter(c => c.id !== id);
                this.renderCommentsPanel();
                this.renderPins();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },
};
