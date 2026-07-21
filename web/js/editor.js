/* Paylash — Editor Page (Collabora) */
const EditorPage = {
    currentFileId: null,
    currentFileName: '',

    render() {
        return `
        <div class="editor-page">
            <div class="editor-toolbar">
                <button class="btn btn-ghost btn-sm" onclick="App.navigate('files')">${I18N.t('editor.back')}</button>
                <span class="editor-filename">${UI.esc(this.currentFileName)}</span>
                <div class="editor-toolbar-right">
                    <button class="btn btn-ghost btn-sm" onclick="EditorPage.download()">${UI.icons.download} ${I18N.t('files.action_download')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="FilesPage.showVersionsModal({id:EditorPage.currentFileId,name:EditorPage.currentFileName})">🕓 ${I18N.t('files.action_versions')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="SharesPage.showShareModal({id:EditorPage.currentFileId,name:EditorPage.currentFileName})">${UI.icons.share} ${I18N.t('files.action_share')}</button>
                </div>
            </div>
            <div class="editor-container" id="editor-container">
                <div class="editor-loading"><div class="spinner"></div><p>${I18N.t('editor.loading')}</p></div>
            </div>
        </div>`;
    },

    async open(fileId, fileName) {
        this.currentFileId = fileId;
        this.currentFileName = fileName;
        App.navigate('editor');
    },

    async init() {
        if (!this.currentFileId) { App.navigate('files'); return; }
        const c = document.getElementById('editor-container');
        try {
            const data = await API.collabora.editorURL(this.currentFileId);
            if (data.editor_url) {
                c.innerHTML = `<iframe src="${data.editor_url}" class="editor-iframe" allowfullscreen></iframe>`;
            } else {
                c.innerHTML = `<div class="empty-state"><p>${I18N.t('editor.unavailable')}</p><p class="text-muted">${I18N.t('editor.unavailable_hint')}</p></div>`;
            }
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>${I18N.t('editor.open_failed')}</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    download() { if (this.currentFileId) FilesPage.download(this.currentFileId, this.currentFileName); }
};
