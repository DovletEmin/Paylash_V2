// Paylash API Client
const API = {
    async _request(method, url, body) {
        const opts = {
            method,
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin',
        };
        if (body && method !== 'GET') opts.body = JSON.stringify(body);
        const res = await fetch(url, opts);
        if (res.status === 401 && !url.includes('/auth/me')) {
            if (typeof App !== 'undefined') App.navigate('login');
            throw new Error(I18N.t('common.session_expired'));
        }
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || I18N.t('common.error_generic'));
        return data;
    },

    public: {
        config() { return API._request('GET', '/api/public/config'); },
    },

    auth: {
        register(username, password, fullName) {
            return API._request('POST', '/api/auth/register', { username, password, full_name: fullName });
        },
        login(username, password) {
            return API._request('POST', '/api/auth/login', { username, password });
        },
        logout() { return API._request('POST', '/api/auth/logout'); },
        me() { return API._request('GET', '/api/auth/me'); },
        updateProfile(displayName, oldPassword, newPassword) {
            return API._request('PATCH', '/api/auth/profile', {
                display_name: displayName, old_password: oldPassword, new_password: newPassword
            });
        },
        uploadAvatar(file) {
            const form = new FormData();
            form.append('avatar', file);
            return fetch('/api/auth/avatar', { method: 'POST', body: form, credentials: 'same-origin' })
                .then(r => r.json().then(d => r.ok ? d : Promise.reject(new Error(d.error || I18N.t('common.error_short')))));
        },
    },

    // Projects the current employee can see (personal sidebar)
    projects: {
        list() { return API._request('GET', '/api/projects'); },
    },

    files: {
        list(params) {
            let url = `/api/files?scope=${params.scope || 'personal'}`;
            if (params.folder_id) url += `&folder_id=${params.folder_id}`;
            if (params.project_id) url += `&project_id=${params.project_id}`;
            if (params.sort) url += `&sort=${params.sort}`;
            if (params.order) url += `&order=${params.order}`;
            if (params.limit) url += `&limit=${params.limit}`;
            if (params.offset) url += `&offset=${params.offset}`;
            return API._request('GET', url);
        },
        upload(file, scope, folderId, projectId, onProgress) {
            const form = new FormData();
            form.append('file', file);
            form.append('scope', scope || 'personal');
            if (folderId) form.append('folder_id', String(folderId));
            if (projectId) form.append('project_id', String(projectId));

            return new Promise((resolve, reject) => {
                const xhr = new XMLHttpRequest();
                xhr.open('POST', '/api/files/upload');
                xhr.withCredentials = true;
                if (onProgress) {
                    xhr.upload.onprogress = (e) => {
                        if (e.lengthComputable) onProgress(Math.round(e.loaded / e.total * 100));
                    };
                }
                xhr.onload = () => {
                    if (xhr.status >= 200 && xhr.status < 300) {
                        resolve(JSON.parse(xhr.responseText));
                    } else {
                        try { reject(new Error(JSON.parse(xhr.responseText).error)); }
                        catch { reject(new Error(I18N.t('common.upload_failed'))); }
                    }
                };
                xhr.onerror = () => reject(new Error(I18N.t('common.network_error')));
                xhr.send(form);
            });
        },
        rename(id, name) { return API._request('PATCH', `/api/files/${id}`, { name }); },
        move(id, folderId) { return API._request('PATCH', `/api/files/${id}/move`, { folder_id: folderId || null }); },
        delete(id) { return API._request('DELETE', `/api/files/${id}`); },
        search(q) { return API._request('GET', `/api/search?q=${encodeURIComponent(q)}`); },
        storageUsage(scope, projectId) {
            let url = `/api/storage/usage?scope=${scope || 'personal'}`;
            if (projectId) url += `&project_id=${projectId}`;
            return API._request('GET', url);
        },
        createBlank(name, type, scope, folderId, projectId) {
            const body = { name, type, scope: scope || 'personal' };
            if (folderId) body.folder_id = folderId;
            if (projectId) body.project_id = projectId;
            return API._request('POST', '/api/files/create', body);
        },
        versions(id) { return API._request('GET', `/api/files/${id}/versions`); },
        restoreVersion(id, versionId) { return API._request('POST', `/api/files/${id}/versions/${encodeURIComponent(versionId)}/restore`); },
        downloadVersion(id, versionId) { window.open(`/api/files/${id}/versions/${encodeURIComponent(versionId)}/download`, '_blank'); },
    },

    folders: {
        tree(scope, projectId) {
            let url = `/api/folders/tree?scope=${scope || 'personal'}`;
            if (projectId) url += `&project_id=${projectId}`;
            return API._request('GET', url);
        },
        create(name, scope, parentId, projectId) {
            const body = { name, scope: scope || 'personal', parent_id: parentId || null };
            if (projectId) body.project_id = projectId;
            return API._request('POST', '/api/folders', body);
        },
        rename(id, name) { return API._request('PATCH', `/api/folders/${id}`, { name }); },
        move(id, parentId) { return API._request('PATCH', `/api/folders/${id}/move`, { parent_id: parentId || null }); },
        delete(id) { return API._request('DELETE', `/api/folders/${id}`); },
        downloadURL(id) { return `/api/folders/${id}/download`; },
    },

    uploads: {
        init(fileName, size, scope, folderId, projectId) {
            const body = { file_name: fileName, size, scope: scope || 'personal' };
            if (folderId) body.folder_id = folderId;
            if (projectId) body.project_id = projectId;
            return API._request('POST', '/api/uploads/init', body);
        },
        status(id) { return API._request('GET', `/api/uploads/${id}`); },
        partURL(id, partNumber) { return API._request('GET', `/api/uploads/${id}/parts/${partNumber}/url`); },
        complete(id, parts) { return API._request('POST', `/api/uploads/${id}/complete`, { parts }); },
        abort(id) { return API._request('DELETE', `/api/uploads/${id}`); },
    },

    trash: {
        list() { return API._request('GET', '/api/trash'); },
        restoreFile(id) { return API._request('POST', `/api/trash/files/${id}/restore`); },
        restoreFolder(id) { return API._request('POST', `/api/trash/folders/${id}/restore`); },
        purgeFile(id) { return API._request('DELETE', `/api/trash/files/${id}`); },
        purgeFolder(id) { return API._request('DELETE', `/api/trash/folders/${id}`); },
        empty() { return API._request('DELETE', '/api/trash'); },
    },

    sharing: {
        share(fileId, userId, permission) {
            return API._request('POST', `/api/files/${fileId}/share`, {
                user_id: userId, permission: permission || 'view'
            });
        },
        deleteShare(fileId, userId) {
            return API._request('DELETE', `/api/files/${fileId}/share/${userId}`);
        },
        updateSharePermission(fileId, userId, permission) {
            return API._request('PATCH', `/api/files/${fileId}/share/${userId}`, { permission });
        },
        setPublic(fileId, isPublic) {
            return API._request('PATCH', `/api/files/${fileId}/share/public`, { is_public: isPublic });
        },
        setVisibility(fileId, visibility) {
            return API._request('PATCH', `/api/files/${fileId}/visibility`, { visibility });
        },
        sharedWithMe() { return API._request('GET', '/api/shared-with-me'); },
        sharedByMe() { return API._request('GET', '/api/shared-by-me'); },
        getFileShares(fileId) { return API._request('GET', `/api/files/${fileId}/shares`); },
        searchUsers(q) { return API._request('GET', `/api/users/search?q=${encodeURIComponent(q)}`); },
    },

    collabora: {
        editorURL(fileId) { return API._request('GET', `/api/collabora/editor-url?file_id=${fileId}`); },
    },

    admin: {
        dashboard() { return API._request('GET', '/api/admin/dashboard'); },
        auditLog(limit) { return API._request('GET', `/api/admin/audit-log${limit ? '?limit=' + limit : ''}`); },
        uploads: {
            list() { return API._request('GET', '/api/admin/uploads'); },
            abort(id) { return API._request('DELETE', `/api/admin/uploads/${id}`); },
        },
        publicQuota: {
            get() { return API._request('GET', '/api/admin/public-quota'); },
            set(quotaMB) { return API._request('PATCH', '/api/admin/public-quota', { quota_mb: quotaMB }); },
        },
        projects: {
            list() { return API._request('GET', '/api/admin/projects'); },
            create(name, quotaBytes) { return API._request('POST', '/api/admin/projects', { name, quota_bytes: quotaBytes || 5368709120 }); },
            update(id, name, quotaBytes) { return API._request('PATCH', `/api/admin/projects/${id}`, { name, quota_bytes: quotaBytes }); },
            delete(id) { return API._request('DELETE', `/api/admin/projects/${id}`); },
            bulkQuota(quotaMB) { return API._request('POST', '/api/admin/projects/bulk-quota', { quota_mb: quotaMB }); },
            members: {
                list(projectId) { return API._request('GET', `/api/admin/projects/${projectId}/members`); },
                add(projectId, userId, permission) { return API._request('POST', `/api/admin/projects/${projectId}/members`, { user_id: userId, permission: permission || 'view' }); },
                update(projectId, userId, permission) { return API._request('PATCH', `/api/admin/projects/${projectId}/members/${userId}`, { permission }); },
                remove(projectId, userId) { return API._request('DELETE', `/api/admin/projects/${projectId}/members/${userId}`); },
            },
        },
        users: {
            list() { return API._request('GET', '/api/admin/users'); },
            create(data) {
                return API._request('POST', '/api/admin/users', data);
            },
            update(id, data) {
                return API._request('PATCH', `/api/admin/users/${id}`, data);
            },
            delete(id) { return API._request('DELETE', `/api/admin/users/${id}`); },
            deleteAll() { return API._request('DELETE', '/api/admin/users/all'); },
            bulkQuota(quotaMB) { return API._request('POST', '/api/admin/users/bulk-quota', { quota_mb: quotaMB }); },
            importFile(file) {
                const form = new FormData();
                form.append('file', file);
                return fetch('/api/admin/users/import', { method: 'POST', body: form, credentials: 'same-origin' })
                    .then(r => r.json().then(d => r.ok ? d : Promise.reject(new Error(d.error || I18N.t('common.error_short')))));
            },
        },
    },
};
