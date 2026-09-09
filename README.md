# admin-portal

`admin-portal` is a small Go administration panel for managing temporary visitor Wi-Fi access in a hospitality or accommodation network.
It provides a French server-rendered interface for managing locations, creating printable Wi-Fi tickets, and synchronizing their credentials with the FreeRADIUS database used by an OPNsense captive portal.

The application is designed to run as an internal service alongside [`camping-infra`](https://github.com/JustARandomBadDev/camping-infra).

## What This Project Is

- An internal administration panel for temporary Wi-Fi access
- A server-rendered Go application with embedded HTML, CSS, JavaScript, and static assets
- A business layer for locations and Wi-Fi tickets
- A synchronization boundary between the administration database and FreeRADIUS
- A small containerized service intended to be deployed behind the administration network

## What This Project Is Not

- Not the captive portal displayed to guests
- Not a FreeRADIUS server
- Not an OPNsense configuration tool
- Not a general-purpose RADIUS administration interface
- Not a standalone application with an embedded database
- Not intended to be exposed directly to the public Internet

## Preview

![Administration dashboard](doc/images/dashboard.png)

## Features

### Administration

- Administrator login and logout
- Password hashing with bcrypt
- Server-side sessions stored in PostgreSQL
- Random session tokens stored only as SHA-256 hashes
- Configurable session lifetime and secure cookie support

### Dashboard

- Tickets created today
- Currently active tickets
- Tickets expiring today
- Tickets revoked today
- Five most recently created tickets

### Locations

- Create locations with a unique code and optional label
- List all locations
- Enable or disable locations without deleting them
- Restrict ticket creation to active locations

### Wi-Fi Tickets

- Create temporary access for a selected location
- Preset durations from 6 hours to 7 days
- Custom durations expressed in days
- Automatic username and password generation
- Active, expired, and revoked lifecycle states
- Search by username, pitch code, or pitch label
- Filter by status, duration, and creation period
- Revoke active tickets
- Select and print multiple active tickets
- A4 and 80 mm thermal receipt layouts

Generated usernames use the numeric part of the location code followed by four random letters.
For example, a location named `A12` produces an identifier such as `12-AbCd`.

Passwords contain seven random characters and omit visually ambiguous characters.

## Architecture

The application separates HTTP handling, business rules, persistence, and RADIUS synchronization.

```mermaid
flowchart TD
    Browser[Admin Browser] --> Panel[Go Admin Panel]
    Panel --> AdminDB[Admin PostgreSQL]
    Panel --> RadiusDB[RADIUS PostgreSQL]
    OPNsense[OPNsense Captive Portal] --> FreeRADIUS
    FreeRADIUS --> RadiusDB
```

### Admin Panel

- Owns the HTTP server and the server-rendered interface
- Validates administrator sessions
- Coordinates ticket and pitch operations
- Exposes a health endpoint for both database connections

### Admin Database

- Owns administrator accounts and sessions
- Owns locations
- Owns the business lifecycle of Wi-Fi tickets
- Remains separate from the technical FreeRADIUS tables

### RADIUS Database

- Stores the credentials and access policies consumed by FreeRADIUS
- Receives ticket provisioning and removal operations from the admin panel
- Stores accounting data written by FreeRADIUS from requests sent by OPNsense
- Is accessed through a dedicated database connection

The admin panel does not create, modify, or purge RADIUS accounting records such as `radacct`.

## Ticket Lifecycle

### Creation

When an administrator creates a ticket, the application:

1. validates the selected pitch and validity period
2. generates the username and password
3. creates the business record in the admin database
4. provisions the corresponding RADIUS user inside a RADIUS database transaction
5. marks the ticket as synchronized

The RADIUS policy contains:

- `Cleartext-Password`
- `Expiration`, formatted in UTC for FreeRADIUS 3.x
- `Simultaneous-Use := 4`

If RADIUS provisioning fails during creation, the newly created admin ticket is removed and the error is returned.

### Expiration and Revocation

Expired tickets are marked when ticket data is read by the panel.
Their RADIUS credentials are then removed.

Revoking a ticket:

- changes its admin-side status to `revoked`
- removes its `radcheck`, `radreply`, and `radusergroup` entries
- clears and disables the corresponding `radius_users` entry

RADIUS cleanup failures after expiration or revocation are logged without reverting the business-side state.

## Project Structure

```text
cmd/
├── admin-panel/       HTTP application entry point
└── adminctl/          Administration and maintenance commands

internal/
├── adminauth/         Administrators and database-backed sessions
├── app/               Application assembly and dependency wiring
├── config/            Environment configuration
├── database/          PostgreSQL connection handling
├── http/              Router and HTTP handlers
├── pitches/           Pitch domain and PostgreSQL repository
├── radius/            FreeRADIUS synchronization
├── static/            Embedded static assets
├── templates/         Embedded server-rendered interface
└── tickets/           Ticket domain and PostgreSQL repository

migrations/            Admin database schema
```

Repositories are defined behind small interfaces so the services remain independent from their PostgreSQL implementations.
Application dependencies are assembled explicitly in `internal/app`.

## Requirements

- Go 1.26.1
- PostgreSQL
- A FreeRADIUS-compatible PostgreSQL schema
- The additional `radius_users` table provided by the surrounding infrastructure

The application requires two separate PostgreSQL connections:

- one for its own business data
- one for the FreeRADIUS data

## Database Setup

The migrations in this repository only target the admin database.

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/001_admin_schema.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/002_admin_auth.sql
```

They create:

- `admin_users`
- `admin_sessions`
- `pitches`
- `wifi_tickets`

The application does not apply migrations automatically at startup.
The FreeRADIUS schema and the `radius_users` table are managed by [`camping-infra`](https://github.com/JustARandomBadDev/camping-infra).

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `APP_ADDR` | HTTP listen address | `:8080` |
| `DATABASE_URL` | Admin PostgreSQL connection URL | required |
| `RADIUS_DATABASE_URL` | FreeRADIUS PostgreSQL connection URL | required |
| `ADMIN_SESSION_TTL` | Administrator session lifetime | `12h` |
| `ADMIN_COOKIE_SECURE` | Adds the `Secure` attribute to the session cookie | `false` |

Set `ADMIN_COOKIE_SECURE=true` when the panel is served through HTTPS.

The legacy `SESSION_SECRET` variable is still loaded by the configuration package but is not used by the current database-backed session implementation.

## Build and Run

Build the application:

```bash
make build
```

Run it locally after preparing both databases:

```bash
APP_ADDR="127.0.0.1:8081" \
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
RADIUS_DATABASE_URL="postgres://radius_user:radius_password@127.0.0.1:5432/radius?sslmode=disable" \
go run ./cmd/admin-panel
```

The server refuses to start if either PostgreSQL database is unavailable.

Available Make targets:

```bash
make help
make run
make build
make test
make fmt
make docker-build
```

## Initial Administrator

After applying the admin migrations, create the first administrator interactively:

```bash
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
go run ./cmd/adminctl create-admin
```

The password is read without terminal echo when supported and stored as a bcrypt hash.

## Maintenance Commands

Resynchronize every active ticket with FreeRADIUS:

```bash
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
RADIUS_DATABASE_URL="postgres://radius_user:radius_password@127.0.0.1:5432/radius?sslmode=disable" \
go run ./cmd/adminctl sync-radius-tickets
```

Remove the legacy `Class = WIFI_GUEST` replies left by the previous network integration:

```bash
RADIUS_DATABASE_URL="postgres://radius_user:radius_password@127.0.0.1:5432/radius?sslmode=disable" \
go run ./cmd/adminctl cleanup-legacy-radius-class
```

## Docker

Build the runtime image:

```bash
docker build -t admin-portal .
```

The Dockerfile uses a multi-stage build and runs the final application as a non-root user.
Templates, styles, and static assets are embedded into the Go binary.
Admin migrations are also copied to `/app/migrations/admin`.

A separate `migrations` target adds `postgresql-client` for infrastructure-managed migration jobs:

```bash
docker build --target migrations -t admin-portal:migrations .
```

On every push to `main`, GitHub Actions publishes the runtime image to GHCR with two tags:

```text
ghcr.io/justarandombaddev/admin-portal:latest
ghcr.io/justarandombaddev/admin-portal:<commit-sha>
```

The complete Compose deployment, database initialization, and service networking are managed in [`camping-infra`](https://github.com/JustARandomBadDev/camping-infra).

## Health Check

```http
GET /healthz
```

The endpoint returns `200 OK` only when both the admin and RADIUS databases respond successfully.

## Testing

Run the unit tests with:

```bash
make test
```

The current test suite covers:

- ticket validation and credential generation
- ticket expiration and revocation behavior
- RADIUS provisioning policies and UTC expiration formatting
- administrator authentication and session validation
- pitch validation
- ticket filters, durations, and print selection helpers
- required database configuration

## Current Limitations

- Ticket expiration and cleanup are currently triggered by panel reads instead of a background worker.
- RADIUS operations are synchronous and do not use a persistent retry queue.
- `Simultaneous-Use` depends on correct RADIUS accounting from OPNsense.
- The admin and RADIUS database schemas are deployed externally; this repository is not a complete standalone environment.
- Ticket passwords remain available in clear text in the admin database because they must be printed and resynchronized with FreeRADIUS.
- The interface is intentionally small and intended for a trusted administration network.

## License

This project is licensed under the GNU General Public License v2.0.
