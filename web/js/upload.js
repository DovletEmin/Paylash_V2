/* Paylash — Resumable large-file uploader.
   Bulk bytes flow straight from the browser to MinIO via presigned URLs
   (see internal/api/uploads.go) — the app server only orchestrates: it
   never sees the file's contents. A session id is cached in localStorage so
   an interrupted upload (page reload, dropped connection) resumes instead
   of restarting from zero. */
const Uploader = {
    LARGE_FILE_THRESHOLD: 50 * 1024 * 1024, // below this, the simple single-shot /api/files/upload is simpler and fine
    CONCURRENCY: 3,
    MAX_PART_RETRIES: 3,

    isLarge(file) { return file.size >= this.LARGE_FILE_THRESHOLD; },

    _storageKey(file, scope, folderId, projectId) {
        return `paylash-upload:${scope}:${folderId || 0}:${projectId || 0}:${file.name}:${file.size}:${file.lastModified}`;
    },

    async uploadLarge(file, scope, folderId, projectId, onProgress) {
        const storageKey = this._storageKey(file, scope, folderId, projectId);
        let session;
        try {
            session = await this._openSession(file, scope, folderId, projectId, storageKey);
        } catch (e) {
            // Resumable upload isn't available (server not configured for
            // it) or failed to start — fall back to the simple path, which
            // still works up to the reverse proxy's body-size limit.
            return API.files.upload(file, scope, folderId, projectId, onProgress);
        }
        try {
            const result = await this._uploadParts(session, file, onProgress);
            localStorage.removeItem(storageKey);
            return result;
        } catch (e) {
            // Keep the localStorage entry so a retry can resume — only
            // clear it once the upload actually completes.
            throw e;
        }
    },

    async _openSession(file, scope, folderId, projectId, storageKey) {
        const saved = localStorage.getItem(storageKey);
        if (saved) {
            try {
                const parsed = JSON.parse(saved);
                const status = await API.uploads.status(parsed.id);
                if (status.status === 'in_progress') {
                    return {
                        id: parsed.id,
                        partSize: status.part_size,
                        partCount: status.part_count,
                        uploaded: new Map((status.uploaded_parts || []).map(p => [p.part_number, p.etag])),
                    };
                }
            } catch (e) { /* session gone (expired/purged) — fall through to a fresh one */ }
            localStorage.removeItem(storageKey);
        }

        const init = await API.uploads.init(file.name, file.size, scope, folderId, projectId);
        localStorage.setItem(storageKey, JSON.stringify({ id: init.id }));
        return { id: init.id, partSize: init.part_size, partCount: init.part_count, uploaded: new Map() };
    },

    async _uploadParts(session, file, onProgress) {
        const { id, partSize, partCount, uploaded } = session;

        const partBytes = (partNumber) => {
            const start = (partNumber - 1) * partSize;
            const end = Math.min(start + partSize, file.size);
            return end - start;
        };

        let uploadedBytes = 0;
        uploaded.forEach((_, pn) => { uploadedBytes += partBytes(pn); });
        const report = () => { if (onProgress) onProgress(Math.min(100, Math.round(uploadedBytes / file.size * 100))); };
        report();

        const pending = [];
        for (let pn = 1; pn <= partCount; pn++) if (!uploaded.has(pn)) pending.push(pn);

        let cursor = 0;
        let firstError = null;
        const worker = async () => {
            while (cursor < pending.length && !firstError) {
                const pn = pending[cursor++];
                try {
                    const etag = await this._uploadPart(id, file, pn, partSize);
                    uploaded.set(pn, etag);
                    uploadedBytes += partBytes(pn);
                    report();
                } catch (e) {
                    firstError = e;
                }
            }
        };
        await Promise.all(Array.from({ length: this.CONCURRENCY }, worker));
        if (firstError) throw firstError;

        const parts = Array.from(uploaded.entries())
            .sort((a, b) => a[0] - b[0])
            .map(([part_number, etag]) => ({ part_number, etag }));
        return API.uploads.complete(id, parts);
    },

    async _uploadPart(sessionId, file, partNumber, partSize) {
        const start = (partNumber - 1) * partSize;
        const blob = file.slice(start, Math.min(start + partSize, file.size));

        let lastErr;
        for (let attempt = 1; attempt <= this.MAX_PART_RETRIES; attempt++) {
            try {
                const { url } = await API.uploads.partURL(sessionId, partNumber);
                const res = await fetch(url, { method: 'PUT', body: blob });
                if (!res.ok) throw new Error(`Bölek ${partNumber} ýüklenmedi (HTTP ${res.status})`);
                const etag = res.headers.get('ETag');
                if (!etag) throw new Error(`Bölek ${partNumber}: ETag alynmady`);
                return etag;
            } catch (e) {
                lastErr = e;
                if (attempt < this.MAX_PART_RETRIES) await new Promise(r => setTimeout(r, 500 * attempt));
            }
        }
        throw lastErr;
    },
};
