/* Paylash — Editor Page (Collabora) */
const EditorPage = {
    currentFileId: null,
    currentFileName: '',

    render() {
        return `
        <div class="editor-page">
            <div class="editor-toolbar">
                <button class="btn btn-ghost btn-sm" onclick="App.navigate('files')">← Yza</button>
                <span class="editor-filename">${UI.esc(this.currentFileName)}</span>
                <div class="editor-toolbar-right">
                    <button class="btn btn-ghost btn-sm" onclick="EditorPage.download()">${UI.icons.download} Ýükle</button>
                    <button class="btn btn-ghost btn-sm" onclick="SharesPage.showShareModal({id:EditorPage.currentFileId,name:EditorPage.currentFileName})">${UI.icons.share} Paýlaş</button>
                </div>
            </div>
            <div class="editor-container" id="editor-container">
                <div class="editor-loading"><div class="spinner"></div><p>Redaktor ýüklenýär…</p></div>
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
                c.innerHTML = '<div class="empty-state"><p>Redaktor elýeterli däl</p><p class="text-muted">Collabora Online işleýändigini barlaň</p></div>';
            }
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>Redaktory açyp bolmady</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    download() { if (this.currentFileId) FilesPage.download(this.currentFileId, this.currentFileName); }
};
