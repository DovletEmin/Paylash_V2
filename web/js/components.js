/* Paylash — UI Components & Utilities */
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

    showModal(title, bodyHTML, footerHTML) {
        const o = document.getElementById('modal-overlay');
        o.innerHTML = `<div class="modal">
            <div class="modal-header">
                <h3 class="modal-title">${this.esc(title)}</h3>
                <button class="modal-close" onclick="UI.closeModal()">✕</button>
            </div>
            <div class="modal-body">${bodyHTML}</div>
            ${footerHTML ? `<div class="modal-footer">${footerHTML}</div>` : ''}
        </div>`;
        o.classList.remove('hidden');
        requestAnimationFrame(() => o.classList.add('visible'));
    },

    closeModal() {
        const o = document.getElementById('modal-overlay');
        o.classList.remove('visible');
        setTimeout(() => { o.classList.add('hidden'); o.innerHTML = ''; }, 200);
    },

    showContextMenu(x, y, items) {
        const m = document.getElementById('context-menu');
        let html = '';
        for (const item of items) {
            if (item.divider) { html += '<div class="context-menu-divider"></div>'; continue; }
            html += `<div class="context-menu-item${item.danger ? ' danger' : ''}" data-action="${item.action}">${item.icon || ''} ${this.esc(item.label)}</div>`;
        }
        m.innerHTML = html;
        m.style.left = Math.min(x, innerWidth - 180) + 'px';
        m.style.top = Math.min(y, innerHeight - 220) + 'px';
        m.classList.remove('hidden');
        m.querySelectorAll('.context-menu-item').forEach(el => {
            el.addEventListener('click', () => {
                const itm = items.find(i => i.action === el.dataset.action);
                if (itm?.handler) itm.handler();
                this.hideContextMenu();
            });
        });
    },

    hideContextMenu() { document.getElementById('context-menu').classList.add('hidden'); },

    passwordField(id, placeholder) {
        return `<div class="pw-field"><input type="password" id="${id}" class="form-control" placeholder="${this.esc(placeholder || '')}"><button type="button" class="pw-toggle" onclick="UI.togglePw('${id}')" tabindex="-1">👁</button></div>`;
    },

    togglePw(id) {
        const inp = document.getElementById(id);
        if (!inp) return;
        inp.type = inp.type === 'password' ? 'text' : 'password';
    },

    esc(s) { if (!s) return ''; const d = document.createElement('div'); d.textContent = s; return d.innerHTML; },

    formatBytes(b) {
        if (!b || b === 0) return '0 B';
        const u = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(b) / Math.log(1024));
        return (b / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + u[i];
    },

    formatDate(d) {
        if (!d) return '';
        const dt = new Date(d), now = new Date(), diff = Math.floor((now - dt) / 60000);
        if (diff < 1) return 'şu wagt';
        if (diff < 60) return diff + ' min öň';
        const h = Math.floor(diff / 60);
        if (h < 24) return h + ' sag öň';
        const days = Math.floor(h / 24);
        if (days < 7) return days + ' gün öň';
        return dt.toLocaleDateString('tk-TM');
    },

    fileIcon(name, isFolder) {
        if (isFolder) return '📁';
        const ext = name.split('.').pop().toLowerCase();
        const map = { pdf:'📄',doc:'📝',docx:'📝',txt:'📃',odt:'📝',xls:'📊',xlsx:'📊',ods:'📊',csv:'📊',ppt:'📽',pptx:'📽',odp:'📽',jpg:'🖼',jpeg:'🖼',png:'🖼',gif:'🖼',webp:'🖼',svg:'🖼',mp3:'🎵',wav:'🎵',mp4:'🎬',avi:'🎬',mkv:'🎬',zip:'📦',rar:'📦','7z':'📦' };
        return map[ext] || '📄';
    },

    fileIconClass(name, isFolder) {
        if (isFolder) return 'folder';
        const ext = name.split('.').pop().toLowerCase();
        if (['doc','docx','odt','txt','pdf','ppt','pptx','odp','xls','xlsx','ods','csv'].includes(ext)) return 'document';
        if (['jpg','jpeg','png','gif','webp','svg'].includes(ext)) return 'image';
        return 'other';
    },

    isCollaboraEditable(name) {
        const ext = name.split('.').pop().toLowerCase();
        return ['doc','docx','odt','xls','xlsx','ods','ppt','pptx','odp','csv'].includes(ext);
    },

    isCollaboraViewable(name) {
        const ext = name.split('.').pop().toLowerCase();
        return ['doc','docx','odt','xls','xlsx','ods','ppt','pptx','odp','pdf','csv'].includes(ext);
    },

    isMediaPreviewable(name) {
        const ext = name.split('.').pop().toLowerCase();
        return this.isImage(ext) || this.isAudio(ext) || this.isVideo(ext) || this.isText(ext);
    },

    isImage(ext) {
        return ['jpg','jpeg','png','gif','webp','svg','bmp','ico','tiff','tif'].includes(ext);
    },

    isAudio(ext) {
        return ['mp3','wav','ogg','flac','aac','m4a','wma','opus'].includes(ext);
    },

    isVideo(ext) {
        return ['mp4','webm','ogg','mov','avi','mkv','wmv','flv','m4v'].includes(ext);
    },

    isText(ext) {
        return ['txt','log','md','json','xml','yaml','yml','ini','cfg','conf','sh','bat','ps1',
                'py','js','ts','go','java','c','cpp','h','hpp','css','html','htm','sql','env','toml'].includes(ext);
    },

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
