/* Paylash — File Manager */
const FilesPage = {
    currentFolder: null,
    currentScope: 'personal',
    currentProjectId: null,
    currentProjectName: '',
    currentProjectPermission: null,
    viewMode: 'grid',
    breadcrumbs: [],
    files: [],
    folders: [],
    filesLimit: 50,
    filesOffset: 0,
    hasMoreFiles: false,

    // Whether the current user may upload/create/rename/delete in the active scope.
    canManage() {
        const isAdmin = App.user && App.user.role === 'admin';
        if (isAdmin) return true;
        if (this.currentScope === 'personal' || this.currentScope === 'common') return true;
        if (this.currentScope === 'project') return this.currentProjectPermission === 'edit';
        return false;
    },

    render() {
        const canUpload = this.canManage();
        return `
        <div class="files-page">
            <div class="files-toolbar">
                <div class="files-toolbar-left">
                </div>
                <div class="files-toolbar-right">
                    <div class="search-box">
                        <span class="search-icon">${UI.icons.search}</span>
                        <input type="text" id="file-search" placeholder="Gözle…" oninput="FilesPage.onSearch(this.value)">
                    </div>
                    <button class="btn btn-icon btn-ghost ${this.viewMode === 'grid' ? 'active' : ''}" onclick="FilesPage.setView('grid')" title="Setka">${UI.icons.grid}</button>
                    <button class="btn btn-icon btn-ghost ${this.viewMode === 'list' ? 'active' : ''}" onclick="FilesPage.setView('list')" title="Sanaw">${UI.icons.list}</button>
                </div>
            </div>
            ${canUpload ? `<div class="files-actions">
                <button class="btn btn-primary btn-sm" onclick="FilesPage.showUploadModal()">${UI.icons.upload} Ýükle</button>
                <button class="btn btn-ghost btn-sm" onclick="FilesPage.showNewFolderModal()">${UI.icons.plus} Täze papka</button>
                <div class="new-file-dropdown">
                    <button class="btn btn-ghost btn-sm" onclick="FilesPage.toggleNewFileMenu(event)">${UI.icons.fileNew} Täze faýl</button>
                    <div class="new-file-menu hidden" id="new-file-menu">
                        <div class="new-file-option" onclick="FilesPage.createNewFile('docx')">
                            <span class="new-file-option-icon docx-icon">📝</span>
                            <div class="new-file-option-info">
                                <span class="new-file-option-name">Word dokument</span>
                                <span class="new-file-option-ext">.docx</span>
                            </div>
                        </div>
                        <div class="new-file-option" onclick="FilesPage.createNewFile('xlsx')">
                            <span class="new-file-option-icon xlsx-icon">📊</span>
                            <div class="new-file-option-info">
                                <span class="new-file-option-name">Excel tablisa</span>
                                <span class="new-file-option-ext">.xlsx</span>
                            </div>
                        </div>
                    </div>
                </div>
            </div>` : ''}
            <div class="breadcrumbs" id="breadcrumbs"></div>
            <input type="file" id="file-input" multiple style="display:none" onchange="FilesPage.handleFileSelect(this.files)">
            <div id="upload-progress" class="upload-progress hidden"></div>
            <div id="files-content">${UI.skeletonCards(6)}</div>
            <div class="drop-overlay hidden" id="drop-overlay">
                <div class="drop-overlay-content">${UI.icons.upload}<p>Faýllary goýberiň</p></div>
            </div>
        </div>`;
    },

    async init() { await this.loadFiles(); this.initDragDrop(); },

    _dragInited: false,
    async loadFiles() {
        const c = document.getElementById('files-content');
        if (!c) return;
        c.innerHTML = UI.skeletonCards(6);
        this.filesOffset = 0;
        try {
            const p = { scope: this.currentScope, limit: this.filesLimit, offset: 0 };
            if (this.currentFolder) p.folder_id = this.currentFolder;
            if (this.currentScope === 'project' && this.currentProjectId) p.project_id = this.currentProjectId;
            const data = await API.files.list(p);
            this.files = data.files || [];
            this.folders = data.folders || [];
            this.breadcrumbs = data.breadcrumbs || [];
            this.hasMoreFiles = this.files.length === this.filesLimit;
            this.renderBreadcrumbs();
            this.renderFiles();
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>Faýllary ýükläp bolmady</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    async loadMoreFiles() {
        const btn = document.getElementById('files-load-more');
        if (btn) { btn.disabled = true; btn.textContent = 'Ýüklenýär…'; }
        try {
            this.filesOffset += this.filesLimit;
            const p = { scope: this.currentScope, limit: this.filesLimit, offset: this.filesOffset };
            if (this.currentFolder) p.folder_id = this.currentFolder;
            if (this.currentScope === 'project' && this.currentProjectId) p.project_id = this.currentProjectId;
            const data = await API.files.list(p);
            const more = data.files || [];
            this.files = this.files.concat(more);
            this.hasMoreFiles = more.length === this.filesLimit;
            this.renderFiles();
        } catch (err) {
            UI.toast(err.message, 'error');
        }
    },

    renderBreadcrumbs() {
        const el = document.getElementById('breadcrumbs');
        if (!el) return;
        const rootLabel = this.currentScope === 'personal' ? 'Şahsy' : this.currentScope === 'project' ? (this.currentProjectName || 'Taslama') : 'Umumy';
        let h = `<a class="breadcrumb-item" onclick="FilesPage.goToFolder(null)">${UI.esc(rootLabel)}</a>`;
        for (const b of this.breadcrumbs) {
            h += `<span class="breadcrumb-sep">/</span><a class="breadcrumb-item" onclick="FilesPage.goToFolder(${b.id})">${UI.esc(b.name)}</a>`;
        }
        el.innerHTML = h;
    },

    renderFiles() {
        const c = document.getElementById('files-content');
        if (!c) return;
        const items = [...this.folders.map(f => ({ ...f, isFolder: true })), ...this.files];
        if (!items.length) {
            c.innerHTML = '<div class="empty-state"><div class="empty-state-icon">📂</div><p>Bu ýerde faýl ýok</p><p class="text-muted">Faýl ýükläň ýa-da papka dörediň</p></div>';
            return;
        }
        if (this.viewMode === 'grid') {
            c.innerHTML = '<div class="file-grid">' + items.map(i => this.gridCard(i)).join('') + '</div>';
        } else {
            c.innerHTML = `<div class="file-list">
                <div class="file-list-header"><div>Ady</div><div>Ölçegi</div><div>Senesi</div><div></div></div>
                ${items.map(i => this.listRow(i)).join('')}
            </div>`;
        }
        if (this.hasMoreFiles) {
            c.innerHTML += `<div style="text-align:center;margin-top:12px">
                <button class="btn btn-ghost btn-sm" id="files-load-more" onclick="FilesPage.loadMoreFiles()">Ýene görkez</button>
            </div>`;
        }
    },

    gridCard(item) {
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const dbl = item.isFolder ? `FilesPage.goToFolder(${item.id})` : `UI.openFile(${item.id},${UI.escJson(item.name)},${item.size_bytes || 0})`;
        const itemJson = UI.escJson(item);
        const ext = item.isFolder ? '' : item.name.split('.').pop().toLowerCase();
        const iconHtml = !item.isFolder && UI.isImage(ext)
            ? `<img class="file-card-thumb" src="/api/files/${item.id}/download" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
            : `<div class="file-card-icon ${cls}">${UI.fileIcon(item.name, item.isFolder)}</div>`;
        return `<div class="file-card" ondblclick="${dbl}" oncontextmenu="FilesPage.showMenu(event,${itemJson})">
            ${iconHtml}
            <div class="file-card-name" title="${UI.esc(item.name)}">${UI.esc(item.name)}</div>
            ${!item.isFolder ? `<div class="file-card-meta">${UI.formatBytes(item.size_bytes || 0)} · ${UI.formatDate(item.updated_at || item.created_at)}</div>` : '<div class="file-card-meta">Papka</div>'}
        </div>`;
    },

    // Falls back to the generic file icon if a thumbnail fails to load
    // (e.g. the image was deleted between listing and rendering).
    thumbError(img) { img.outerHTML = '<div class="file-card-icon image">🖼</div>'; },

    listRow(item) {
        const icon = UI.fileIcon(item.name, item.isFolder);
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const dbl = item.isFolder ? `FilesPage.goToFolder(${item.id})` : `UI.openFile(${item.id},${UI.escJson(item.name)},${item.size_bytes || 0})`;
        const itemJson = UI.escJson(item);
        return `<div class="file-list-row" ondblclick="${dbl}" oncontextmenu="FilesPage.showMenu(event,${itemJson})">
            <div class="file-list-name"><span class="file-list-icon ${cls}">${icon}</span>${UI.esc(item.name)}</div>
            <div class="file-list-size">${item.isFolder ? '—' : UI.formatBytes(item.size_bytes || 0)}</div>
            <div class="file-list-date">${UI.formatDate(item.updated_at || item.created_at)}</div>
            <div class="file-list-actions"><button class="btn btn-icon btn-sm" onclick="FilesPage.showMenu(event,${itemJson})">⋮</button></div>
        </div>`;
    },

    showMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const canManage = this.canManage();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: 'Aç', icon: '📂', handler: () => this.goToFolder(item.id) });
            if (canManage) {
                items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.renameFolder(item) });
                items.push({ action: 'move', label: 'Göçürmek', icon: '📁', handler: () => this.moveItem(item) });
                items.push({ divider: true });
                items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.deleteFolder(item) });
            }
        } else {
            if (UI.isMediaPreviewable(item.name)) items.push({ action: 'preview', label: 'Görmek', icon: '👁', handler: () => PreviewPage.open(item.id, item.name, item.size_bytes) });
            else if (UI.isCollaboraEditable(item.name)) items.push({ action: 'edit', label: 'Redaktirle', icon: '📝', handler: () => EditorPage.open(item.id, item.name) });
            else if (UI.isCollaboraViewable(item.name)) items.push({ action: 'view', label: 'Açmak', icon: '👁', handler: () => EditorPage.open(item.id, item.name) });
            items.push({ action: 'download', label: 'Ýükle', icon: '📥', handler: () => this.download(item.id, item.name) });
            items.push({ action: 'versions', label: 'Wersiýalar', icon: '🕓', handler: () => this.showVersionsModal(item) });
            if (canManage) {
                items.push({ action: 'share', label: 'Paýlaş', icon: '🔗', handler: () => SharesPage.showShareModal(item) });
                items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.renameFile(item) });
                items.push({ action: 'move', label: 'Göçürmek', icon: '📁', handler: () => this.moveItem(item) });
                items.push({ divider: true });
                items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.deleteFile(item) });
            }
        }
        UI.showContextMenu(e.clientX, e.clientY, items);
    },

    async showVersionsModal(item) {
        UI.showModal('Wersiýalar', '<div class="text-muted" style="text-align:center;padding:20px"><div class="spinner"></div></div>', '');
        try {
            const versions = await API.files.versions(item.id);
            const body = !versions || !versions.length
                ? '<p class="text-muted">Wersiýa taryhy ýok</p>'
                : `<div class="version-list">${versions.map(v => `
                    <div class="version-item">
                        <div class="version-item-info">
                            <div>${v.is_latest ? '<strong>Häzirki</strong>' : UI.formatDate(v.last_modified)}</div>
                            <div class="text-muted" style="font-size:.78rem">${UI.formatBytes(v.size_bytes)}${v.is_latest ? '' : ' · ' + new Date(v.last_modified).toLocaleString('tk-TM')}</div>
                        </div>
                        <div style="display:flex;gap:6px">
                            <button class="btn btn-sm btn-ghost" onclick="API.files.downloadVersion(${item.id},'${v.version_id}')">${UI.icons.download}</button>
                            ${!v.is_latest ? `<button class="btn btn-sm btn-ghost" onclick="FilesPage.restoreVersion(${item.id},'${v.version_id}')">Öwür</button>` : ''}
                        </div>
                    </div>`).join('')}</div>`;
            UI.showModal('Wersiýalar — ' + item.name, body, '<button class="btn btn-ghost" onclick="UI.closeModal()">Ýap</button>');
        } catch (e) {
            UI.showModal('Wersiýalar', `<p class="text-muted">${UI.esc(e.message)}</p>`, '<button class="btn btn-ghost" onclick="UI.closeModal()">Ýap</button>');
        }
    },

    async restoreVersion(id, versionId) {
        try {
            await API.files.restoreVersion(id, versionId);
            UI.toast('Wersiýa dikeldildi', 'success');
            UI.closeModal();
            this.loadFiles();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    setScope(s, projectId, projectName, permission) {
        this.currentScope = s;
        this.currentProjectId = s === 'project' ? projectId : null;
        this.currentProjectName = s === 'project' ? (projectName || '') : '';
        this.currentProjectPermission = s === 'project' ? (permission || 'view') : null;
        this.currentFolder = null;
        this._dragInited = false;
        App.renderPage('files');
    },
    setView(m) { this.viewMode = m; this.renderFiles(); },
    goToFolder(id) { this.currentFolder = id; this.loadFiles(); },

    async onSearch(q) {
        if (!q || q.length < 2) { this.loadFiles(); return; }
        try { const data = await API.files.search(q); this.files = data || []; this.folders = []; this.breadcrumbs = []; this.renderBreadcrumbs(); this.renderFiles(); } catch {}
    },

    showUploadModal() { document.getElementById('file-input').click(); },

    async handleFileSelect(fileList) {
        if (!fileList.length) return;
        const prog = document.getElementById('upload-progress');
        prog.classList.remove('hidden');
        for (const file of fileList) {
            const id = 'u-' + Math.random().toString(36).substr(2, 6);
            const isLarge = typeof Uploader !== 'undefined' && Uploader.isLarge(file);
            const resumeBadge = isLarge
                ? '<span class="upload-item-badge" title="Sahypany täzelesiňizem ýa-da internet üzülsedem, ýükleme togtan ýerinden dowam eder">↻ dowam eder</span>'
                : '';
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)} ${resumeBadge}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            try {
                const onProgress = pct => {
                    const f = document.getElementById(id + '-f'), p = document.getElementById(id + '-p');
                    if (f) f.style.width = pct + '%'; if (p) p.textContent = Math.round(pct) + '%';
                };
                // Large files (CAD/renders/scans) go through the resumable
                // direct-to-MinIO uploader instead of the single-shot XHR —
                // see web/js/upload.js for why. Uploader falls back to the
                // simple path itself if the resumable one isn't available.
                if (isLarge) {
                    await Uploader.uploadLarge(file, this.currentScope, this.currentFolder, this.currentProjectId, onProgress);
                } else {
                    await API.files.upload(file, this.currentScope, this.currentFolder, this.currentProjectId, onProgress);
                }
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(`"${file.name}" ýüklenip bilmedi: ${err.message}`, 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadFiles(); App.loadStorageUsage();
        document.getElementById('file-input').value = '';
    },

    initDragDrop() {
        if (this._dragInited) return;
        if (!this.canManage()) return;
        const pg = document.querySelector('.files-page');
        const ov = document.getElementById('drop-overlay');
        if (!pg || !ov) return;
        this._dragInited = true;
        let dragCounter = 0;
        pg.addEventListener('dragenter', ev => { ev.preventDefault(); dragCounter++; ov.classList.remove('hidden'); });
        pg.addEventListener('dragover', ev => { ev.preventDefault(); });
        pg.addEventListener('dragleave', ev => { ev.preventDefault(); dragCounter--; if (dragCounter <= 0) { dragCounter = 0; ov.classList.add('hidden'); } });
        pg.addEventListener('drop', ev => { ev.preventDefault(); dragCounter = 0; ov.classList.add('hidden'); if (ev.dataTransfer.files.length) this.handleFileSelect(ev.dataTransfer.files); });
    },

    // A same-origin <a download> click lets the browser's own download
    // manager stream the response straight to disk — unlike the fetch()+blob
    // approach this replaced, which buffered the *entire* file in page
    // memory first. That was fine for small documents but would have made
    // downloading a 100GB scan/render either crawl or crash the tab, which
    // defeats the point of the large-file support built elsewhere in this
    // app (chunked resumable upload, presigned direct-download redirect).
    download(id, name) {
        const a = document.createElement('a');
        a.href = `/api/files/${id}/download`;
        a.download = name;
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
    },

    renameFile(item) {
        UI.showModal('Adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="rename-input" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="FilesPage.doRenameFile(${item.id})">Üýtget</button>`);
        setTimeout(() => { const i = document.getElementById('rename-input'); if (i) { i.focus(); i.select(); } }, 100);
    },
    async doRenameFile(id) {
        const n = document.getElementById('rename-input').value.trim(); if (!n) return;
        try { await API.files.rename(id, n); UI.closeModal(); UI.toast('Ady üýtgedildi', 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    renameFolder(item) {
        UI.showModal('Papkanyň adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="rename-input" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="FilesPage.doRenameFolder(${item.id})">Üýtget</button>`);
        setTimeout(() => { const i = document.getElementById('rename-input'); if (i) { i.focus(); i.select(); } }, 100);
    },
    async doRenameFolder(id) {
        const n = document.getElementById('rename-input').value.trim(); if (!n) return;
        try { await API.folders.rename(id, n); UI.closeModal(); UI.toast('Ady üýtgedildi', 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    async moveItem(item) {
        try {
            const folders = await API.folders.tree(this.currentScope, this.currentProjectId);
            const excluded = new Set();
            if (item.isFolder) {
                excluded.add(item.id);
                const byParent = {};
                for (const f of folders) { const k = f.parent_id || 'root'; (byParent[k] = byParent[k] || []).push(f); }
                const stack = [item.id];
                while (stack.length) {
                    const cur = stack.pop();
                    for (const f of (byParent[cur] || [])) { excluded.add(f.id); stack.push(f.id); }
                }
            }
            const lines = UI.flattenFolderTree(folders).filter(l => !excluded.has(l.id));
            const options = ['<option value="">— Kök —</option>']
                .concat(lines.map(l => `<option value="${l.id}">${'— '.repeat(l.depth)}${UI.esc(l.name)}</option>`));
            UI.showModal(`"${UI.esc(item.name)}" göçürmek`,
                `<div class="form-group"><label>Nirä göçürmeli</label><select id="move-target" class="form-control">${options.join('')}</select></div>`,
                `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="FilesPage.doMove(${item.id},${!!item.isFolder})">Göçür</button>`);
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    async doMove(id, isFolder) {
        const val = document.getElementById('move-target').value;
        const targetId = val ? parseInt(val) : null;
        try {
            if (isFolder) await API.folders.move(id, targetId);
            else await API.files.move(id, targetId);
            UI.closeModal();
            UI.toast('Göçürildi', 'success');
            this.loadFiles();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    deleteFile(item) {
        UI.showModal('Faýly pozmak',
            `<p>"<strong>${UI.esc(item.name)}</strong>" faýlyny pozmak isleýärsiňizmi?</p><p class="text-muted">Bu yzyna gaýtaryp bolmaýar.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-danger" onclick="FilesPage.doDeleteFile(${item.id})">Poz</button>`);
    },
    async doDeleteFile(id) { try { await API.files.delete(id); UI.closeModal(); UI.toast('Faýl pozuldy', 'success'); this.loadFiles(); App.loadStorageUsage(); } catch (e) { UI.toast(e.message, 'error'); } },

    deleteFolder(item) {
        UI.showModal('Papkany pozmak',
            `<p>"<strong>${UI.esc(item.name)}</strong>" papkasyny pozmak isleýärsiňizmi?</p><p class="text-muted">Ähli faýllar pozular.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-danger" onclick="FilesPage.doDeleteFolder(${item.id})">Poz</button>`);
    },
    async doDeleteFolder(id) { try { await API.folders.delete(id); UI.closeModal(); UI.toast('Papka pozuldy', 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); } },

    showNewFolderModal() {
        UI.showModal('Täze papka', `<div class="form-group"><label>Papkanyň ady</label><input type="text" id="new-folder-name" class="form-control" placeholder="Papka ady"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="FilesPage.doCreateFolder()">Döret</button>`);
        setTimeout(() => { const i = document.getElementById('new-folder-name'); if (i) i.focus(); }, 100);
    },
    async doCreateFolder() {
        const n = document.getElementById('new-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, this.currentScope, this.currentFolder, this.currentProjectId); UI.closeModal(); UI.toast('Papka döredildi', 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    toggleNewFileMenu(e) {
        e.stopPropagation();
        const menu = document.getElementById('new-file-menu');
        if (!menu) return;
        menu.classList.toggle('hidden');
        const closeMenu = (ev) => {
            if (!menu.contains(ev.target)) {
                menu.classList.add('hidden');
                document.removeEventListener('click', closeMenu);
            }
        };
        if (!menu.classList.contains('hidden')) {
            setTimeout(() => document.addEventListener('click', closeMenu), 0);
        }
    },

    createNewFile(type) {
        document.getElementById('new-file-menu')?.classList.add('hidden');
        const defaults = { docx: 'Täze dokument', xlsx: 'Täze tablisa' };
        const defaultName = defaults[type] || 'Täze faýl';
        UI.showModal('Täze faýl döret',
            `<div class="form-group"><label>Faýlyň ady</label><input type="text" id="new-file-name" class="form-control" placeholder="${UI.esc(defaultName)}" value="${UI.esc(defaultName)}"></div>
             <div class="new-file-type-badge"><span class="file-type-label">${type.toUpperCase()}</span></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="FilesPage.doCreateNewFile('${type}')">Döret</button>`);
        setTimeout(() => { const i = document.getElementById('new-file-name'); if (i) { i.focus(); i.select(); } }, 100);
    },

    async doCreateNewFile(type) {
        const name = document.getElementById('new-file-name')?.value.trim();
        if (!name) { UI.toast('Faýl adyny giriziň', 'error'); return; }
        try {
            const file = await API.files.createBlank(name, type, this.currentScope, this.currentFolder, this.currentProjectId);
            UI.closeModal();
            UI.toast('Faýl döredildi', 'success');
            this.loadFiles();
            App.loadStorageUsage();
            // Open the file in editor if it's an editable document
            if (type === 'docx' || type === 'xlsx') {
                setTimeout(() => EditorPage.open(file.id, file.name), 500);
            }
        } catch (e) { UI.toast(e.message || 'Faýl döredip bolmady', 'error'); }
    },

};
