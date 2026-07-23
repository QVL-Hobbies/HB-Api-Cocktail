# HB-Api-Cocktail

Service Go du domaine **Hobbies** dédié aux cocktails.

## Prérequis

- Go 1.26.3

## Lancement

```bash
go build ./...
go run .
```

Le service écoute par défaut sur `http://127.0.0.1:8080`.

Vérifier qu'il répond :

```bash
curl http://127.0.0.1:8080/health
```

Réponse attendue (`200`) :

```json
{ "status": "ok" }
```

## Variables d'environnement

Chargées au démarrage, avec valeurs par défaut appliquées si absentes.

| Variable            | Description                            | Défaut                 |
| ------------------- | -------------------------------------- | ---------------------- |
| `PORT`              | Port d'écoute HTTP                     | `8080`                 |
| `BIND_ADDR`         | Adresse d'écoute                       | `127.0.0.1`            |
| `DB_PATH`           | Chemin du fichier SQLite (US2+)        | `./data/cocktail.db`   |
| `IMAGES_DIR`        | Dossier des images (US2+)              | `./data/images`        |
| `LOCAL_WRITE_TOKEN` | Token de l'endpoint d'écriture (US2+)  | _(vide)_               |

`LOCAL_WRITE_TOKEN` est un secret : il n'est ni commité ni loggé. Le service ne
charge aucun fichier automatiquement (pas de loader dotenv) : il lit uniquement
l'environnement de son process. Injecter donc les variables directement dans cet
environnement — via `export` (ou l'équivalent shell) avant le lancement, ou via un
outillage qui source les variables avant de démarrer le process.

## Base de données

Le service utilise **SQLite** via le driver pure-Go `modernc.org/sqlite` (aucune
dépendance cgo, build standard). Au démarrage, il ouvre le fichier pointé par
`DB_PATH`, crée le dossier parent si besoin, puis applique le schéma de façon
idempotente (`CREATE TABLE IF NOT EXISTS`). Les clés étrangères sont activées.

### Schéma

| Table                  | Rôle                                                         |
| ---------------------- | ------------------------------------------------------------ |
| `cocktails`            | Recette : `name`, `instructions`, `glass`, `category`, `strength`, `alcoholic`, `season`, `image_name`, `image_path` |
| `ingredients`          | Ingrédient unique par `name`                                 |
| `cocktail_ingredients` | Liaison cocktail ↔ ingrédient (`quantity`, `unit`)           |
| `cocktail_tags`        | Tags multiples par cocktail (`cocktail_id`, `tag`)           |

Relations :

- `cocktails 1—N cocktail_ingredients N—1 ingredients` (many-to-many avec quantité/unité)
- `cocktails 1—N cocktail_tags` (tags multiples, un tag = une ligne)
- Les liaisons sont supprimées en cascade avec leur cocktail (`ON DELETE CASCADE`).

Le contenu textuel des recettes est en **anglais**.

## Script de seed

Charge un jeu de recettes de test dans une base locale (cocktails, ingrédients,
liaisons avec quantités, tags).

```bash
go run ./tools/seed
```

- Lit `DB_PATH` (défaut `./data/cocktail.db`) et crée le schéma si absent.
- Lit le jeu de données depuis `SEED_FILE` (défaut `tools/seed/data.json`).
- Repart d'un état vide : purge les tables dans la transaction avant insertion,
  donc ré-exécutable sans doublon (relancer laisse le même contenu).
- Le fichier `tools/seed/data.json` est un **jeu de test non versionné**
  (gitignoré), comme le `.db` produit.

## Structure du repo

```
.
├── main.go                     Point d'entrée : config, ouverture base, serveur HTTP, arrêt gracieux
├── config.go                   Chargement de la configuration depuis l'environnement
├── health.go                   Handler GET /health
├── openapi.go                  Service de la spec OpenAPI et de la doc Redoc
├── api/openapi.yaml            Contrat OpenAPI (spec-first) de toute la surface v1
├── internal/database/          Ouverture SQLite + schéma partagés (service + seed)
├── tools/                      Scripts d'outillage local (voir tools/README.md)
│   └── seed/                   Script de chargement d'un jeu de test
├── go.mod
├── .gitignore
└── README.md
```

## Contrat OpenAPI

Le contrat de l'API est écrit en spec-first, à la main, dans `api/openapi.yaml`
(OpenAPI 3.0.3). Il décrit l'intégralité de la surface v1 : lecture publique des
cocktails, recherche par ingrédients, référentiels, service d'images et endpoint
local d'écriture (non public). La spec fait foi ; elle est embarquée dans le
binaire (`//go:embed`) et servie publiquement.

| Ressource            | URL                                   |
| -------------------- | ------------------------------------- |
| Spec brute (YAML)    | `http://127.0.0.1:8080/openapi.yaml`  |
| Documentation Redoc  | `http://127.0.0.1:8080/docs`          |

La doc Redoc est un HTML statique minimal qui charge le bundle Redoc depuis un
CDN et pointe sur `/openapi.yaml` : aucune dépendance Go ni build front ajoutés.

## Endpoints

Seuls `/health` et les routes de documentation sont réellement exposés à ce
stade. Les routes métier décrites dans `api/openapi.yaml` sont le contrat cible
et seront implémentées ultérieurement.

| Méthode | Chemin         | Description                     | Réponse                     |
| ------- | -------------- | ------------------------------- | --------------------------- |
| `GET`   | `/health`      | Sonde de vivacité               | `200 {"status":"ok"}`       |
| `GET`   | `/openapi.yaml`| Contrat OpenAPI (spec brute)    | `200` (application/yaml)    |
| `GET`   | `/docs`        | Documentation Redoc             | `200` (text/html)           |
