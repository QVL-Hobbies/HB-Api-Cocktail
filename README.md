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
├── internal/database/          Ouverture SQLite + schéma partagés (service + seed)
├── tools/                      Scripts d'outillage local (voir tools/README.md)
│   └── seed/                   Script de chargement d'un jeu de test
├── go.mod
├── .gitignore
└── README.md
```

## Endpoints

| Méthode | Chemin    | Description        | Réponse            |
| ------- | --------- | ------------------ | ------------------ |
| `GET`   | `/health` | Sonde de vivacité  | `200 {"status":"ok"}` |
