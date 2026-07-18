/* Paylash — Trash (soft-deleted files/folders) */
const TrashPage = {
    items: [],

    render() {
        return `
        <div class="files-page">
            <div class="files-toolbar">
                <div class="files-toolbar-left">
                    <p class="text-muted" style="font-size:.8rem">Pozulan faýllar 30 günden soň awtomatik hemişelik pozulýar</p>
                </div>
                <div class="files-toolbar-right">
                    <button class="btn btn-danger btn-sm" onclick="TrashPage.confirmEmpty()">${UI.icons.trash} Çöpi boşat</button>
                </div>
            </div>
            <div id="trash-content">${UI.skeletonCards(6)}</div>
        </div>`;
    },

    async init() { await this.load(); },

    async load() {
        const c = document.getElementById('trash-content');
        if (!c) return;
        c.innerHTML = UI.skeletonCards(6);
        try {
            const data = await API.trash.list();
            this.items = [
                ...(data.folders || []).map(f => ({ ...f, isFolder: true })),
                ...(data.files || []),
            ];
            this.renderItems();
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>Çöpi ýükläp bolmady</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    renderItems() {
        const c = document.getElementById('trash-content');
        if (!c) return;
        if (!this.items.length) {
            c.innerHTML = '<div class="empty-state"><div class="empty-state-icon">🗑</div><p>Çöp gutusy boş</p></div>';
            return;
        }
        c.innerHTML = '<div class="file-grid">' + this.items.map(i => this.card(i)).join('') + '</div>';
    },

    card(item) {
        const icon = UI.fileIcon(item.name, item.isFolder);
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const isFolder = item.isFolder ? 'true' : 'false';
        return `<div class="file-card">
            <div class="file-card-icon ${cls}">${icon}</div>
            <div class="file-card-name" title="${UI.esc(item.name)}">${UI.esc(item.name)}</div>
            <div class="file-card-meta">${UI.formatDate(item.deleted_at)} pozuldy</div>
            <div style="display:flex;gap:6px;margin-top:8px">
                <button class="btn btn-sm btn-ghost" style="flex:1" onclick="TrashPage.restore(${isFolder},${item.id})" title="Dikelt">↩ Dikelt</button>
                <button class="btn btn-sm btn-danger" onclick="TrashPage.confirmPurge(${isFolder},${item.id},${UI.escJson(item.name)})" title="Ebedilik poz">🗑</button>
            </div>
        </div>`;
    },

    async restore(isFolder, id) {
        try {
            if (isFolder) await API.trash.restoreFolder(id);
            else await API.trash.restoreFile(id);
            UI.toast('Dikeldildi', 'success');
            this.load();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    confirmPurge(isFolder, id, name) {
        UI.showModal('Ebedilik pozmak',
            `<p>"<strong>${UI.esc(name)}</strong>" ebedilik pozulsynmy?</p><p class="text-muted">Bu yzyna gaýtaryp bolmaýar.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-danger" onclick="TrashPage.doPurge(${isFolder},${id})">Poz</button>`);
    },

    async doPurge(isFolder, id) {
        try {
            if (isFolder) await API.trash.purgeFolder(id);
            else await API.trash.purgeFile(id);
            UI.closeModal();
            UI.toast('Ebedilik pozuldy', 'success');
            this.load();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    confirmEmpty() {
        if (!this.items.length) { UI.toast('Çöp gutusy eýýäm boş', 'info'); return; }
        UI.showModal('Çöpi boşatmak',
            `<p>Çöp gutusyndaky ähli faýllar we bukjalar ebedilik pozulsynmy?</p><p class="text-muted">Bu yzyna gaýtaryp bolmaýar.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-danger" onclick="TrashPage.doEmpty()">Boşat</button>`);
    },

    async doEmpty() {
        try {
            await API.trash.empty();
            UI.closeModal();
            UI.toast('Çöp gutusy boşadyldy', 'success');
            this.load();
        } catch (e) { UI.toast(e.message, 'error'); }
    },
};
