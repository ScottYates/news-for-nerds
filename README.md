# NewsForNerds

A customizable RSS dashboard for news junkies. Create pages with draggable, resizable widgets displaying RSS feeds, iframes, or custom HTML content.

## Features

- **RSS Feeds**: Add any RSS/Atom feed with automatic favicon detection
- **Iframe Widgets**: Embed any website with optional CSS injection and scroll position offset
- **HTML Widgets**: Create custom content with a visual WYSIWYG editor (TinyMCE)
- **Drag & Drop**: Freely position widgets anywhere on the canvas
- **Resizable**: Resize widgets by dragging corners/edges
- **Multiple Pages**: Create multiple dashboard pages with custom URL slugs
- **Customizable**: Per-widget colors, backgrounds, grid snapping
- **Import/Export**: Batch import/export widget layouts as JSON
- **Keyboard Shortcuts**: Quick actions (press `?` to see all)
- **Google OAuth**: Secure authentication with automatic page migration
- **Visitor Mode**: Use the app without logging in; pages transfer to your account on login

## Requirements

- Go 1.25+
- SQLite (embedded via modernc.org/sqlite, no separate install needed)
- Google OAuth credentials (optional, for persistent accounts)

## Quick Start

### 1. Clone and Build

```bash
git clone https://github.com/ScottYates/news-for-nerds.git newsfornerds
cd newsfornerds
go build -o newsfornerds ./cmd/srv
```

### 2. Configure Environment

Copy the example environment file and fill in your Google OAuth credentials:

```bash
cp .env.example .env
```

Edit `.env` with your credentials:

```
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret
```

**To get Google OAuth credentials:**

1. Go to [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Create a new project (or select existing)
3. Go to "Credentials" → "Create Credentials" → "OAuth 2.0 Client IDs"
4. Set application type to "Web application"
5. Add authorized redirect URI: `https://your-domain.com/auth/callback`
6. Copy the Client ID and Client Secret to your `.env` file

> **Note:** The app works without OAuth — visitors get a cookie-based identity and can create/manage pages. OAuth lets users persist their account across devices.

### 3. Run the Server

```bash
./newsfornerds -listen :8000
```

The server will automatically create `db.sqlite3` and run migrations on first start.

Visit `http://localhost:8000` to use the app.

## Running as a Systemd Service

For production deployment, create a systemd service:

### 1. Create the Service File

```bash
sudo tee /etc/systemd/system/newsfornerds.service > /dev/null <<EOF
[Unit]
Description=NewsForNerds RSS Dashboard
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$(pwd)
EnvironmentFile=$(pwd)/.env
ExecStart=$(pwd)/newsfornerds -listen :8000
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
```

### 2. Enable and Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now newsfornerds
```

### 3. Check Status

```bash
systemctl status newsfornerds
journalctl -u newsfornerds -f  # View logs
```

### 4. Restart After Updates

```bash
go build -o newsfornerds ./cmd/srv
sudo systemctl restart newsfornerds
```

## Deploying on exe.dev

```bash
git clone https://github.com/ScottYates/news-for-nerds.git newsfornerds
cd newsfornerds
go build -o newsfornerds ./cmd/srv
cp .env.example .env
# Edit .env with your Google OAuth credentials
# Set redirect URI to: https://your-vm-name.exe.xyz:8000/auth/callback
sudo cp newsfornerds.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now newsfornerds
```

Your app will be available at `https://your-vm-name.exe.xyz:8000/`.

## Project Structure

```
newsfornerds/
├── cmd/srv/           # Main entry point
├── srv/               # HTTP server and handlers
│   ├── auth.go        # Google OAuth & visitor identity
│   ├── server.go      # Routes, API handlers, slug management
│   ├── rss.go         # RSS/Atom feed fetching & parsing
│   ├── proxy.go       # Feed proxy support
│   ├── favicon.go     # Favicon detection
│   ├── templates/     # Go HTML templates
│   └── static/        # CSS, JavaScript
├── db/                # Database layer
│   ├── migrations/    # SQL migration files (auto-applied)
│   ├── queries/       # SQL queries for sqlc
│   ├── dbgen/         # Generated query code
│   └── sqlc.yaml      # sqlc configuration
├── .env.example       # Environment template
├── Makefile           # Build shortcuts
└── go.mod             # Go module definition
```

## Database

The app uses SQLite with automatic migrations. The database file (`db.sqlite3`) is created in the working directory on first run.

Migrations are located in `db/migrations/` and run automatically in order.

## Configuration Options

### Command Line Flags

- `-listen <addr>`: Listen address (default: `:8000`)

### Environment Variables

- `GOOGLE_CLIENT_ID`: Google OAuth client ID (optional)
- `GOOGLE_CLIENT_SECRET`: Google OAuth client secret (optional)

## Usage Tips

### Keyboard Shortcuts

Press `?` on the dashboard to see all available shortcuts:

- `N` - Create new widget
- `G` - Toggle grid
- `R` - Refresh all feeds
- `X` - Mark all as read (while hovering a widget)
- `Esc` - Close dialogs

### Widget Types

1. **RSS Feed**: Enter any RSS/Atom feed URL
2. **Iframe**: Embed websites with optional CSS injection to hide elements
3. **HTML**: Create custom content with the visual editor

### Custom Page URLs

Pages are automatically assigned a URL slug based on your name (e.g., `/page/scott-yates`). You can customize the slug in **Settings** → **Custom URL**. Live availability checking ensures your chosen slug is unique.

### Import/Export

Export your widget layout as JSON from Settings, and import it on another page or instance. Import is atomic — all widgets are created in a single transaction.

### Customization

- **Colors**: Each widget can have custom background, header, and text colors
- **Grid Snapping**: Enable grid in settings for aligned layouts
- **Lock Widgets**: Prevent accidental moves with the lock button
- **Header Size**: Adjust widget header height in page settings
- **Text Brightness**: Control feed text brightness
- **Auto Refresh**: Set automatic feed refresh intervals

## Development

### Build

```bash
go build -o newsfornerds ./cmd/srv
```

Or use the Makefile:

```bash
make build    # builds to ./srv
make test     # runs tests
```

### Regenerate SQL Code

After modifying `db/queries/*.sql`:

```bash
cd db
go tool github.com/sqlc-dev/sqlc/cmd/sqlc generate
```

### Run Tests

```bash
go test ./...
```

## License

MIT
