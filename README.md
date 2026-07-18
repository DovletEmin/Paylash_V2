# Paýlaş

Local cloud storage for an architecture studio — personal/common/project spaces, real-time
document co-editing via Collabora Online, and a role-based admin panel. Ships as a single Go
binary with the frontend embedded; runs fully offline via Docker Compose (Postgres + MinIO +
Collabora + Caddy).

For the full design rationale (data model, access model, API reference), see [PLAN.md](PLAN.md).

## Features

- Personal / common / project file spaces with per-project ACLs (view/edit)
- Real-time document co-editing (Collabora Online, WOPI)
- File versioning — every save keeps a retrievable history (90-day retention)
- Trash — soft-deleted files/folders are recoverable for 30 days before permanent purge
- Resumable large-file uploads (100GB+) — presigned multipart direct to MinIO, survives a
  dropped connection or page reload
- Point-to-point and company-wide file sharing
- Move files/folders between locations (metadata-only — instant regardless of size)
- Admin panel: projects, employees, quotas, bulk CSV/XLSX import, audit log, active-upload
  visibility
- Login rate-limiting, forced password change on first login for admin-created accounts
- Self-registration can be turned off (`PAYLASH_ALLOW_REGISTRATION=false`) once employees are
  onboarded via the admin panel

## Quickstart

```bash
cp .env.example .env
# edit .env if needed (PAYLASH_DOMAIN, PAYLASH_MINIO_PUBLIC_ENDPOINT)

docker compose up -d --build
```

Then, on every client machine, point `PAYLASH_DOMAIN` (default `paylash.local`) at the server's
IP via `/etc/hosts` (or local DNS). Open `https://paylash.local` in a browser.

Default admin: `admin` / `admin123` — you'll be forced to change this password on first login.

### Large-file uploads

Resumable uploads need the browser to reach MinIO's S3 API port directly (bypassing the app
server for the actual bytes). Set `PAYLASH_MINIO_PUBLIC_ENDPOINT` in `.env` to the server's LAN
address on MinIO's published port (`9000` by default) — see the comment in `.env.example`. Leave
it empty to disable resumable uploads; regular uploads still work up to Caddy's body-size limit.

## Development

No Go toolchain required to just run the app (it's built inside Docker), but if you have one
locally:

```bash
go build ./...
go vet ./...
go test ./...
```

The frontend (`web/`) is plain HTML/CSS/JS, no build step — it's embedded into the Go binary via
`go:embed` and served directly.

## Project layout

See the "Структура проекта" section in [PLAN.md](PLAN.md) for a full breakdown of
`internal/{api,db,server,storage,wopi,janitor}` and `web/`.
