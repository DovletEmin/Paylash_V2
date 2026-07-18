# Paylash — Локальная облачная система для архитектурной студии с Collabora Online

## Стек технологий

| Компонент | Технология | Почему |
|-----------|-----------|--------|
| Backend | **Go** | Один бинарник, встроенный веб-сервер, высокая производительность |
| Frontend | **Встроенный SPA** (HTML/CSS/JS) | Embed в Go-бинарник через `embed.FS`, нет зависимости на Node в runtime |
| БД | **PostgreSQL 16** | Многопользовательская система — нужны нормальный concurrent access, транзакции, JSON-поля |
| Хранилище файлов | **MinIO** | S3-совместимый, bucket-per-project, отлично для бинарных файлов |
| Документы | **Collabora Online (CODE)** | WOPI-протокол, совместное редактирование в реальном времени, работает оффлайн |
| Всё в Docker | **docker-compose** | PostgreSQL + MinIO + Collabora + Go-сервер — одна команда `docker-compose up` |

### Почему PostgreSQL, а не SQLite?
- Много сотрудников одновременно — SQLite блокирует всю базу на запись
- Нужны `FOREIGN KEY`, `JOIN`, полнотекстовый поиск, JSON-поля для метаданных
- Роли, проекты, права доступа — реляционная модель идеальна

### Почему MinIO, а не локальная файловая система?
- **Bucket per project** — изоляция хранилища на уровне проекта
- **Квоты** — легко ограничить объём для сотрудника/проекта
- **Versioning** — история изменений файлов бесплатно
- **S3 API** — стандартный протокол, легко мигрировать потом
- **Presigned URLs** — отдача больших файлов без нагрузки на Go-сервер

---

## Роли и пользователи

### Роли
| Роль | Описание |
|------|----------|
| **admin** | Полный доступ. Управляет проектами, участниками, квотами, пользователями |
| **user** | Обычный сотрудник. Личное хранилище + общая (Umumy) папка + доступ к назначенным проектам + шеринг |

### Регистрация (для user)
Самостоятельная регистрация — без каскадных справочников (факультет/курс/группа не используются):
1. **Доly ady** (ФИО) — необязательно
2. **Логин** (username) — уникальный, минимум 3 символа
3. **Пароль** — минимум 6 символов, хранится bcrypt-хешем

Новый сотрудник сразу получает доступ к личному хранилищу и общей папке. Доступ к папкам-проектам сотруднику **выдаёт админ** отдельно (см. ниже).

Админ создаётся при первом запуске (seed: `admin` / `admin123`).

---

## Модель доступа к файлам — три пространства

1. **Личное (`personal`)** — только у владельца (+ у тех, с кем он расшарил конкретный файл через `file_shares`).
2. **Общее (`common`, «Umumy»)** — открыто на просмотр **и** редактирование любому аутентифицированному сотруднику. Это одна общая папка на всю компанию, как раньше работало «групповое» хранилище, но без привязки к конкретной группе — доступна абсолютно всем.
3. **Проектное (`project`)** — папка-проект, которую создаёт **админ**. Доступ определяется исключительно списком участников (`project_members`), которых админ явно добавляет в проект с правом:
   - **view** — только просмотр/скачивание;
   - **edit** — загрузка, переименование, удаление, совместное редактирование.

   Сотрудник может состоять сразу в нескольких проектах одновременно (в отличие от старой модели «1 пользователь = 1 группа»). Админ имеет полный доступ ко всем проектам без явного членства.

---

## Архитектура

```
┌─────────────────────────────────────────────────────────┐
│                       Go Binary                          │
│                                                          │
│  ┌──────────┐  ┌──────────┐  ┌─────────┐  ┌──────────┐  │
│  │ Frontend  │  │ REST API │  │  WOPI   │  │  Admin   │  │
│  │ (embed)   │  │ (user)   │  │ Server  │  │   API    │  │
│  └──────────┘  └──────────┘  └─────────┘  └──────────┘  │
│                      │            │             │         │
│              ┌───────┴────────────┴─────────────┴──┐     │
│              │          Service Layer               │     │
│              │  (auth, files, sharing, projects...) │     │
│              └───────┬────────────┬─────────────────┘     │
│                      │            │                       │
│              ┌───────┴───┐  ┌────┴────────┐              │
│              │ PostgreSQL │  │    MinIO    │              │
│              │  (метадата)│  │  (файлы)   │              │
│              └───────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────┘
         ▲                              ▲
         │ HTTP :8080                   │ WOPI
         ▼                              ▼
┌─────────────────┐          ┌──────────────────┐
│   Браузер       │          │ Collabora Online │
│   (сотрудник)   │          │  (Docker :9980)  │
└─────────────────┘          └──────────────────┘
```

---

## Модель данных (PostgreSQL)

```sql
-- Пользователи (сотрудники)
CREATE TABLE users (
    id            SERIAL PRIMARY KEY,
    username      VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255) DEFAULT '',
    role          VARCHAR(20) DEFAULT 'user',   -- 'admin' | 'user'
    quota_bytes   BIGINT DEFAULT 1073741824,    -- 1 GB личное хранилище
    avatar_url    VARCHAR(500) DEFAULT '',
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Папки-проекты (создаёт админ)
CREATE TABLE projects (
    id           SERIAL PRIMARY KEY,
    name         VARCHAR(255) NOT NULL UNIQUE,
    quota_bytes  BIGINT DEFAULT 5368709120,      -- 5 GB по умолчанию
    minio_bucket VARCHAR(255),                   -- "project-{id}"
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

-- ACL: кто из сотрудников имеет доступ к проекту и с каким правом
CREATE TABLE project_members (
    id         SERIAL PRIMARY KEY,
    project_id INT REFERENCES projects(id) ON DELETE CASCADE,
    user_id    INT REFERENCES users(id) ON DELETE CASCADE,
    permission VARCHAR(20) NOT NULL DEFAULT 'view', -- 'view' | 'edit'
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(project_id, user_id)
);

-- Папки (виртуальные, для навигации)
CREATE TABLE folders (
    id          SERIAL PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    parent_id   INT REFERENCES folders(id) ON DELETE CASCADE,
    owner_id    INT REFERENCES users(id) ON DELETE CASCADE,
    project_id  INT REFERENCES projects(id) ON DELETE CASCADE,
    scope       VARCHAR(20) NOT NULL,         -- 'personal' | 'common' | 'project'
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Файлы (метаданные в PG, содержимое в MinIO)
CREATE TABLE files (
    id            SERIAL PRIMARY KEY,
    name          VARCHAR(500) NOT NULL,
    mime_type     VARCHAR(255),
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    minio_bucket  VARCHAR(255) NOT NULL,
    minio_key     VARCHAR(1000) NOT NULL,      -- путь в MinIO
    folder_id     INT REFERENCES folders(id) ON DELETE SET NULL,
    owner_id      INT REFERENCES users(id) ON DELETE CASCADE,
    project_id    INT REFERENCES projects(id),
    scope         VARCHAR(20) NOT NULL,         -- 'personal' | 'common' | 'project'
    visibility    VARCHAR(20) NOT NULL DEFAULT 'private', -- 'private' | 'common'
    version       INT DEFAULT 1,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Точечный шеринг файлов конкретному коллеге (независимо от проектов)
CREATE TABLE file_shares (
    id          SERIAL PRIMARY KEY,
    file_id     INT REFERENCES files(id) ON DELETE CASCADE,
    shared_by   INT REFERENCES users(id) ON DELETE CASCADE, 
    shared_with INT REFERENCES users(id) ON DELETE CASCADE,
    permission  VARCHAR(20) DEFAULT 'view',    -- 'view' | 'edit'
    is_public   BOOLEAN DEFAULT FALSE,         -- доступ всем, у кого есть ссылка на шеринг
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(file_id, shared_with)
);

-- WOPI access tokens
CREATE TABLE wopi_tokens (
    id          SERIAL PRIMARY KEY,
    token       VARCHAR(255) NOT NULL UNIQUE,
    file_id     INT REFERENCES files(id) ON DELETE CASCADE,
    user_id     INT REFERENCES users(id) ON DELETE CASCADE,
    permission  VARCHAR(20) DEFAULT 'view',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Сессии
CREATE TABLE sessions (
    id          VARCHAR(255) PRIMARY KEY,      -- session token
    user_id     INT REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Настройки (квота общей папки и т.п.)
CREATE TABLE settings (
    key   VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
```

---

## Структура проекта

```
Paylash/
├── main.go                      # Точка входа
├── go.mod
├── go.sum
│
├── internal/
│   ├── config/
│   │   └── config.go            # Конфигурация (env vars)
│   │
│   ├── server/
│   │   ├── server.go            # HTTP-сервер, роутинг
│   │   └── middleware.go        # Auth middleware, CORS, logging, seed admin
│   │
│   ├── models/
│   │   └── models.go            # Все структуры: User, Project, ProjectMember, File, Folder...
│   │
│   ├── authutil/
│   │   └── authutil.go          # bcrypt-хеширование, извлечение юзера из контекста
│   │
│   ├── db/
│   │   ├── db.go                # PostgreSQL подключение + миграции
│   │   ├── users.go              # Запросы: users
│   │   ├── projects.go           # Запросы: projects, project_members, dashboard
│   │   ├── files.go              # Запросы: files, folders
│   │   └── shares.go             # Запросы: file_shares, CanAccessFile (проверка прав)
│   │
│   ├── api/
│   │   ├── handler.go            # Общий Handler + JSON-хелперы
│   │   ├── auth.go               # POST /api/auth/register, /login, /logout, профиль, аватар
│   │   ├── files.go               # CRUD файлов/папок, canEditScope
│   │   ├── shares.go              # Шеринг файлов, список проектов юзера, Collabora URL
│   │   └── admin.go               # Админ: dashboard, projects, project members, users
│   │
│   ├── wopi/
│   │   └── handler.go            # WOPI CheckFileInfo, GetFile, PutFile
│   │
│   └── storage/
│       └── minio.go              # MinIO клиент: upload, download, delete, bucket-хелперы
│
├── web/                          # Frontend (embed в бинарник)
│   ├── index.html                # SPA entry point
│   ├── css/
│   │   └── style.css             # Все стили + анимации
│   ├── js/
│   │   ├── app.js                # Роутер SPA, инициализация, сайдбар (личное/общее/проекты)
│   │   ├── api.js                # HTTP-клиент
│   │   ├── auth.js               # Экраны логина / регистрации
│   │   ├── files.js               # Файловый менеджер
│   │   ├── editor.js              # Collabora iframe
│   │   ├── preview.js             # Просмотр медиа
│   │   ├── shares.js              # Страница "Поделиться" / "Мне поделились"
│   │   ├── admin.js               # Админ-панель (проекты + участники, пользователи)
│   │   └── components.js          # Toast, Modal, Dropdown, Breadcrumbs...
│   └── assets/
│       └── icons/                # SVG-иконки
│
├── samples/
│   └── users_template.csv        # Шаблон для массового импорта сотрудников
│
├── docker-compose.yml            # PostgreSQL + MinIO + Collabora + Caddy + Go app
├── Dockerfile                    # Multi-stage build для Go
├── Caddyfile                     # Reverse-proxy + автоматический HTTPS (внутренний CA)
├── PLAN.md
└── README.md
```

---

## Цветовая палитра

| Роль | Цвет | HEX |
|------|-------|-----|
| Primary | Индиго | `#6366F1` |
| Primary Hover | Тёмный индиго | `#4F46E5` |
| Background | Тёмно-серый | `#0F0F11` |
| Surface | Антрацит | `#18181B` |
| Surface Hover | Серый | `#27272A` |
| Border | Мягкий серый | `#3F3F46` |
| Text Primary | Белый | `#FAFAFA` |
| Text Secondary | Приглушённый | `#A1A1AA` |
| Accent / Success | Изумруд | `#10B981` |
| Danger | Коралл | `#EF4444` |
| Warning | Янтарь | `#F59E0B` |
| Admin accent | Пурпур | `#A855F7` |

Тёмная тема по умолчанию. Минималистичный стиль в духе Linear/Vercel.
Админ-панель использует пурпурный акцент для визуального разделения от пользовательского интерфейса.

> **Язык интерфейса: только Туркменский (Türkmen dili)**
> Все тексты, кнопки, placeholder-ы, уведомления, ошибки — на туркменском языке.

---

## API Endpoints

### Аутентификация
```
POST   /api/auth/register          # Регистрация (username, password, full_name)
POST   /api/auth/login             # Логин → session cookie
POST   /api/auth/logout            # Логаут
GET    /api/auth/me                # Текущий пользователь + роль
PATCH  /api/auth/profile           # Смена имени/пароля
POST   /api/auth/avatar            # Загрузка аватара
GET    /api/avatar/:id             # Отдача аватара
```

### Проекты, видимые текущему сотруднику
```
GET    /api/projects                # Список проектов юзера + его permission (для сайдбара)
```

### Файлы и папки (авторизация обязательна)
```
GET    /api/files?scope=personal|common|project&project_id=...&folder_id=...&sort=...
POST   /api/files/upload              # Загрузка (multipart, scope + folder_id + project_id)
POST   /api/files/create              # Создать пустой .docx/.xlsx
GET    /api/files/:id/download        # Скачивание
DELETE /api/files/:id                  # Удаление
PATCH  /api/files/:id                  # Переименование
POST   /api/folders                    # Создание папки
PATCH  /api/folders/:id                # Переименование
DELETE /api/folders/:id                # Удаление
GET    /api/search?q=...              # Поиск (личное + общее + свои проекты)
GET    /api/storage/usage             # Использование хранилища (personal/common/project)
```

### Шеринг
```
POST   /api/files/:id/share           # Поделиться файлом {user_id, permission}
DELETE /api/files/:id/share/:user_id   # Отменить шеринг
PATCH  /api/files/:id/share/:user_id   # Изменить право
PATCH  /api/files/:id/share/public     # Открыть/закрыть публичную ссылку
PATCH  /api/files/:id/visibility       # 'private' | 'common' (common — только админ)
GET    /api/shared-with-me             # Файлы, которыми поделились со мной
GET    /api/shared-by-me               # Файлы, которыми поделился я
GET    /api/files/:id/shares           # Кому расшарен файл
GET    /api/users/search               # Поиск сотрудников (для модалки шеринга)
```

### WOPI (для Collabora)
```
GET    /wopi/files/:id                 # CheckFileInfo
GET    /wopi/files/:id/contents        # GetFile
POST   /wopi/files/:id/contents        # PutFile
```

### Collabora
```
GET    /api/collabora/editor-url?file_id=...  # URL для iframe (с токеном)
```

### Админ API (только role=admin)
```
GET    /api/admin/dashboard            # Статистика: юзеры, проекты, файлы, объём

POST   /api/admin/projects             # Создать проект {name, quota_bytes}
GET    /api/admin/projects             # Список проектов
PATCH  /api/admin/projects/:id         # Переименовать / изменить квоту
DELETE /api/admin/projects/:id         # Удалить

GET    /api/admin/projects/:id/members            # Список участников проекта
POST   /api/admin/projects/:id/members            # Добавить участника {user_id, permission}
PATCH  /api/admin/projects/:id/members/:userId     # Изменить право (view/edit)
DELETE /api/admin/projects/:id/members/:userId     # Убрать участника

GET    /api/admin/users                # Список сотрудников
POST   /api/admin/users                # Создать сотрудника
PATCH  /api/admin/users/:id            # Изменить (роль, квота, имя, пароль)
DELETE /api/admin/users/:id            # Удалить
DELETE /api/admin/users/all            # Удалить всех кроме админа
POST   /api/admin/users/bulk-quota     # Квота всем сотрудникам разом
POST   /api/admin/projects/bulk-quota  # Квота всем проектам разом
POST   /api/admin/users/import         # Импорт CSV/XLSX (username,password,full_name,quota_mb)
GET    /api/admin/public-quota         # Квота общей папки
PATCH  /api/admin/public-quota         # Изменить квоту общей папки
```

---

## Frontend — Страницы

### 1. Логин / Регистрация
Простая форма: логин + пароль (вход), либо ФИО + логин + пароль (регистрация). Без каскадных списков.

### 2. Файловый менеджер (главный экран)
Сайдбар строится динамически:
- 🔒 Личное хранилище (всегда)
- 🌐 Общая папка (всегда, полный доступ у всех)
- 📁 / 👁 Список проектов, в которых состоит сотрудник (иконка отличается для view-only участников)

### 3. Страница «Мне поделились»
Без изменений — файлы, расшаренные лично сотруднику, плюс всё из общей папки.

### 4. Модалка «Поделиться»
Поиск коллеги → выбор права (просмотр/редактирование) → (для админа) переключатель видимости «Şahsy / Umumy» для конкретного личного файла.

### 5. Админ-панель
- 📊 Статистика
- 👥 **Таслалар (Проекты)** — CRUD проектов + управление участниками (поиск сотрудника, назначение view/edit, список участников с изменением/удалением прав)
- 👤 Işgärler (Сотрудники) — CRUD, импорт, массовая квота
- 📁 Файлы конкретного проекта (выбор проекта из списка → браузер файлов)
- 🌐 Файлы общей папки

---

## Collabora Online — Совместная работа

Не изменилось: WOPI-токен выдаётся на файл + пользователя, право на запись (`UserCanWrite`) определяется тем же `CanAccessFile`, что и для скачивания — Collabora сам объединяет сессии нескольких соавторов в одном документе.

---

## Хранилище в MinIO — Структура бакетов

```
MinIO
├── personal-{user_id}/          # Личное хранилище каждого сотрудника
├── project-{project_id}/        # Хранилище конкретного проекта (доступ по ACL)
└── common-files/                 # Общая папка, открыта всем сотрудникам
```

- Квоты контролируются на уровне приложения (проверка перед upload)

---

## Запуск

```bash
# 1. Создать .env из .env.example, при необходимости поменять PAYLASH_DOMAIN
cp .env.example .env

# 2. На каждой клиентской машине прописать домен в hosts-файл,
#    указывающий на IP сервера, например:
#    <IP сервера>  paylash.local

# 3. Поднять всю инфраструктуру (Postgres + MinIO + Collabora + Caddy + сам Go-app)
docker compose up -d --build

# Открыть в браузере: https://paylash.local
# Админ по умолчанию: admin / admin123
```

Caddy сам выпускает и обновляет TLS-сертификат для `PAYLASH_DOMAIN` через встроенный внутренний CA (директива `tls internal` в `Caddyfile`) — вручную генерировать сертификат (как раньше для nginx) не нужно. Браузер один раз покажет предупреждение "сертификату не доверяют" (самоподписанный корневой CA); чтобы убрать это предупреждение на всех клиентских машинах, можно один раз извлечь и установить корневой сертификат Caddy:
```bash
docker exec paylash-caddy cat /data/caddy/pki/authorities/local/root.crt
```
и добавить его в доверенные корневые сертификаты на клиентских машинах.

> Домен на `.local` иногда пересекается с mDNS/Bonjour-резолвингом на macOS/Linux с Avahi — если резолвинг не срабатывает через hosts-файл, попробовать другой суффикс (например `paylash.internal`) и поменять `PAYLASH_DOMAIN` в `.env`.

---

## Ограничения и заметки
- Всё запускается локально через Docker — ничего не уходит в интернет, ACME/Let's Encrypt не используется
- Совместное редактирование — нативная функция Collabora, не требует WebSocket на нашей стороне
- Go-бинарник со встроенным фронтендом = один файл для деплоя бэкенда
- Админ создаётся автоматически при первом запуске
- Сотрудник может одновременно состоять в нескольких проектах — в отличие от старой модели, где у пользователя была ровно одна группа
- Rate-limiting (который был в nginx) сознательно не переносился на Caddy — штатный образ `caddy:2-alpine` не включает плагин для этого; при необходимости можно собрать кастомный образ через `xcaddy` с `caddy-ratelimit`
