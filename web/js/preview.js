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
                    <button class="btn btn-ghost btn-sm" onclick="PreviewPage.download()">${UI.icons.download} ${I18N.t('files.action_download')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="SharesPage.showShareModal({id:PreviewPage.currentFileId,name:PreviewPage.currentFileName})">${UI.icons.share} ${I18N.t('files.action_share')}</button>
                </div>
            </div>
            <div class="preview-container" id="preview-container">
                <div class="editor-loading"><div class="spinner"></div><p>${I18N.t('common.loading')}</p></div>
            </div>
        </div>`;
    },

    async open(fileId, fileName, size) {
        this.currentFileId = fileId;
        this.currentFileName = fileName;
        this.currentFileSize = size || 0;
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
        if (typeof AdminPage !== 'undefined') {
            if (AdminPage._adminProjectFiles && Array.isArray(AdminPage._adminProjectFiles.files)) lists.push(AdminPage._adminProjectFiles.files);
            if (AdminPage._adminCommonFiles && Array.isArray(AdminPage._adminCommonFiles.files)) lists.push(AdminPage._adminCommonFiles.files);
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
            c.innerHTML = `<div class="preview-media preview-image">
                ${this.navArrowHTML('prev')}
                <img src="${url}" alt="${UI.esc(this.currentFileName)}" onclick="PreviewPage.toggleZoom(this)">
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
    },

    guessMime() {
        const ext = this.currentFileName.split('.').pop().toLowerCase();
        const map = { mp4:'video/mp4', webm:'video/webm', ogg:'video/ogg', mov:'video/mp4', m4v:'video/mp4' };
        return map[ext] || 'video/mp4';
    },

    toggleZoom(img) {
        img.classList.toggle('zoomed');
    },

    download() { if (this.currentFileId) FilesPage.download(this.currentFileId, this.currentFileName); }
};
