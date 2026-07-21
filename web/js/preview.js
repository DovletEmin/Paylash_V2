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
                <span class="editor-filename">${UI.esc(this.currentFileName)}</span>
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

    async init() {
        if (!this.currentFileId) { App.navigate('files'); return; }
        const c = document.getElementById('preview-container');
        const type = UI.mediaType(this.currentFileName);
        const url = `/api/files/${this.currentFileId}/download`;

        if (type === 'image') {
            c.innerHTML = `<div class="preview-media preview-image"><img src="${url}" alt="${UI.esc(this.currentFileName)}" onclick="PreviewPage.toggleZoom(this)"></div>`;
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
