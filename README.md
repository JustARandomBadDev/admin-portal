# captive-portal-admin

Panel admin local en Go pour gérer les tickets WiFi temporaires d'un camping.

Le projet vise une interface serveur-side simple, accessible depuis le réseau
admin local, avec PostgreSQL comme base cible. Le panel gère les emplacements,
la synchronisation FreeRADIUS, l'authentification admin et l'impression des
tickets. Les données métier admin restent séparées des tables techniques
FreeRADIUS.

## Lancement

```sh
APP_ADDR="127.0.0.1:8081" \
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
RADIUS_DATABASE_URL="postgres://radius_user:radius_dev_password@127.0.0.1:5432/radius?sslmode=disable" \
ADMIN_SESSION_TTL="12h" \
  go run ./cmd/admin-panel
```

Ou via Make :

```sh
make help
APP_ADDR="127.0.0.1:8081" \
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
RADIUS_DATABASE_URL="postgres://radius_user:radius_dev_password@127.0.0.1:5432/radius?sslmode=disable" \
ADMIN_SESSION_TTL="12h" \
make run
make build
make test
make fmt
```

## Docker

Build local :

```sh
make docker-build
```

L'image peut être nommée pour un registre externe sans changer le Makefile :

```sh
make docker-build IMAGE_NAME=ghcr.io/owner/captive-portal-admin IMAGE_TAG=dev
```

Dans le labo `../test-env`, le panel admin est exposé sur :

```text
http://127.0.0.1:8081
```

Le conteneur écoute en interne sur `0.0.0.0:8080` via `APP_ADDR`, avec le
mapping Compose `8081:8080`. La base métier admin utilisée est `admin` :

```text
postgres://admin_user:admin_password@postgres:5432/admin?sslmode=disable
```

La synchronisation FreeRADIUS utilise une connexion séparée vers `radius` :

```text
postgres://radius_user:radius_dev_password@postgres:5432/radius?sslmode=disable
```

Démarrage depuis `../test-env` :

```sh
cd ../test-env
make rebuild
curl -i http://127.0.0.1:8081/healthz
```

Image GHCR publiée par la CI :

```sh
docker pull ghcr.io/justarandombaddev/captive-portal-admin:latest
docker pull ghcr.io/justarandombaddev/captive-portal-admin:sha-<commit>
docker pull ghcr.io/justarandombaddev/captive-portal-admin:vX.Y.Z
```

Les migrations SQL admin sont versionnées dans le repo sous :

```text
migrations/
```

L'image Docker les copie dans :

```text
/app/migrations/admin
```

L'image runtime applicative ne contient pas `psql`. Le Dockerfile expose une
target `migrations`, basée sur l'image runtime, qui ajoute uniquement
`postgresql-client` pour exécuter les migrations.

Dans `camping-infra`, le service `admin-migrations` utilise cette target
`migrations` et applique les fichiers de `/app/migrations/admin` sur la base
`admin` avec `apply-migrations.sh` et `psql` avant le démarrage du panel. Aucun
framework de migration supplémentaire n'est utilisé.

L'application Go ne lance pas les migrations au démarrage. Elle suppose que la
base `admin` a déjà été préparée par l'infra.

## Variables d'environnement

| Variable              | Description                                 | Défaut  |
| --------------------- | ------------------------------------------- | ------- |
| `APP_ADDR`            | Adresse d'écoute HTTP                       | `:8080` |
| `DATABASE_URL`        | URL PostgreSQL de la base métier admin      | requis  |
| `RADIUS_DATABASE_URL` | URL PostgreSQL de la base FreeRADIUS        | requis  |
| `SESSION_SECRET`      | Secret applicatif réservé aux sessions      | vide    |
| `ADMIN_SESSION_TTL`   | Durée de validité d'une session admin       | `12h`   |
| `ADMIN_COOKIE_SECURE` | Ajoute l'attribut `Secure` au cookie admin  | `false` |

En production derrière HTTPS, définir `ADMIN_COOKIE_SECURE=true`.

## Authentification admin

Créer le premier admin après application des migrations :

```sh
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
  go run ./cmd/adminctl create-admin
```

La commande demande un identifiant et un mot de passe. Le mot de passe est hashé
avec bcrypt avant insertion dans `admin_users`; il n'est jamais stocké en clair.

Resynchroniser les tickets actifs vers FreeRADIUS, par exemple après l'ajout
ou la correction d'une politique RADIUS :

```sh
DATABASE_URL="postgres://admin_user:admin_password@127.0.0.1:5432/admin?sslmode=disable" \
RADIUS_DATABASE_URL="postgres://radius_user:radius_dev_password@127.0.0.1:5432/radius?sslmode=disable" \
  go run ./cmd/adminctl sync-radius-tickets
```

Nettoyer les anciennes réponses RADIUS `Class` devenues inutiles après migration
vers OPNsense :

```sh
RADIUS_DATABASE_URL="postgres://radius_user:radius_dev_password@127.0.0.1:5432/radius?sslmode=disable" \
  go run ./cmd/adminctl cleanup-legacy-radius-class
```

Le panel utilise une session serveur stockée dans `admin_sessions`. Le navigateur
reçoit seulement un cookie opaque `admin_session` :

- `HttpOnly`
- `SameSite=Lax`
- `Path=/`
- `Secure` selon `ADMIN_COOKIE_SECURE`

Le logout révoque la session et supprime le cookie côté navigateur.

## Routes

- `GET /` : dashboard placeholder.
- `GET /login` : formulaire de connexion admin.
- `POST /api/admin/auth/login` : création de session admin.
- `POST /api/admin/auth/logout` : révocation de session admin.
- `GET /api/admin/auth/me` : admin courant en JSON.
- `GET /tickets` : liste des tickets WiFi.
- `GET /tickets/new` : formulaire de création d'un ticket.
- `POST /tickets` : création d'un ticket temporaire.
- `POST /tickets/{id}/revoke` : révocation d'un ticket.
- `GET /pitches` : liste des emplacements.
- `GET /pitches/new` : formulaire de création d'un emplacement.
- `POST /pitches` : création d'un emplacement.
- `POST /pitches/{id}/disable` : désactivation d'un emplacement.
- `POST /pitches/{id}/enable` : réactivation d'un emplacement.
- `GET /healthz` : vérifie PostgreSQL et retourne `OK`.

Toutes les routes sont protégées par authentification sauf `/healthz`, `/login`
et `/api/admin/auth/login`.

## Organisation backend

- `cmd/admin-panel` : point d'entrée.
- `internal/app` : assemblage de l'application.
- `internal/config` : configuration par variables d'environnement.
- `internal/http` : routeur et handlers HTTP.
- `internal/database` : connexion PostgreSQL via `pgxpool`.
- `internal/tickets` : service métier tickets WiFi.
- `internal/pitches` : service métier emplacements.
- `internal/radius` : service de synchronisation FreeRADIUS futur.
- `internal/adminauth` : authentification admin, sessions et repository.
- `internal/templates` : vues HTML serveur-side.

## Modèles métier

Le panel admin gère deux agrégats principaux, sans ORM lourd et avec des
repositories PostgreSQL explicites.

`Ticket` représente un accès WiFi temporaire :

- identifiant UUID
- username unique
- mot de passe temporaire en clair pour impression et synchronisation RADIUS
- emplacement associé
- statut `active`, `expired` ou `revoked`
- dates de validité
- informations de création et de révocation
- horodatage de synchronisation FreeRADIUS futur

`Pitch` représente un emplacement du camping :

- identifiant UUID
- code ou numéro unique
- libellé optionnel
- activation ou désactivation
- timestamps de création et mise à jour

Les packages `tickets` et `pitches` exposent chacun une interface `Repository`
et une implémentation PostgreSQL explicite basée sur `pgxpool`. `internal/app`
injecte ces repositories SQL au démarrage. Le serveur refuse de démarrer si
`DATABASE_URL`, `RADIUS_DATABASE_URL` ou PostgreSQL sont indisponibles.

## Synchronisation FreeRADIUS

`internal/radius` expose une interface `Syncer` pour isoler le flux :

```text
admin panel -> RadiusSync -> FreeRADIUS DB
```

Le service tickets appelle cette interface après création, expiration ou
révocation d'un ticket. L'implémentation PostgreSQL ouvre une connexion dédiée
vers `radius` via `RADIUS_DATABASE_URL` et ne réutilise jamais la connexion
`admin`.

Création d'un ticket :

- crée ou réactive l'entrée `radius_users` ;
- remplace les check items `radcheck` du ticket ;
- ajoute `Cleartext-Password := <mot de passe>` ;
- ajoute `Simultaneous-Use := 4` pour limiter un ticket à quatre connexions
  simultanées ;
- ajoute `Expiration := <date de fin>` pour que FreeRADIUS refuse le ticket
  après `valid_until`.

La valeur `Expiration` est générée en UTC au format texte attendu par
FreeRADIUS 3.x. L'interface admin calcule la validité selon l'heure locale du
camping, puis la synchronisation RADIUS convertit le même instant en UTC afin
d'éviter les ambiguïtés de fuseau horaire et de changement d'heure.

Révocation ou expiration :

- supprime les credentials `radcheck` ;
- supprime les réponses/groupes éventuels du username ;
- désactive `radius_users`.

Lors de la création, si le provisioning FreeRADIUS échoue, le ticket admin tout
juste créé est supprimé et l'erreur est remontée. Les opérations RADIUS sont
exécutées dans une transaction sur la base `radius`. Le champ
`wifi_tickets.radius_synced_at` est mis à jour uniquement après une sync
réussie.

L'architecture actuelle est `OPNsense Captive Portal -> FreeRADIUS ->
PostgreSQL`. Le panel admin écrit les comptes et politiques RADIUS dans
PostgreSQL; OPNsense authentifie les clients via FreeRADIUS et envoie
l'Accounting RADIUS.

La limite `Simultaneous-Use` est appliquée par FreeRADIUS à partir de cet
Accounting. OPNsense doit donc envoyer les requêtes d'accounting, notamment avec
l'option `Always send accounting requests` activée. Le port d'accounting
standard est `1813`, les sessions actives sont stockées dans `radacct`, et des
sessions fantômes sans `acctstoptime` peuvent empêcher une nouvelle connexion.
Configurer un `Interim-Update` côté OPNsense si disponible facilite la détection
des sessions obsolètes.

## Migrations PostgreSQL

Les migrations dans `migrations/` concernent uniquement la base métier du panel
admin (`admin`). La migration initiale crée :

- `admin_users` : comptes administrateurs.
- `pitches` : emplacements du camping.
- `wifi_tickets` : tickets WiFi temporaires liés aux emplacements.

La migration d'authentification ajoute :

- `admin_sessions` : sessions serveur des administrateurs.

FreeRADIUS conserve ses propres tables techniques dans la base RADIUS
(`radcheck`, `radreply`, `radacct`, `radpostauth`, etc.). Avec OPNsense Captive
Portal, `radacct` est la source primaire des sessions RADIUS et des connexions
observées par FreeRADIUS. Le panel admin ne crée pas et ne purge pas ces logs.
