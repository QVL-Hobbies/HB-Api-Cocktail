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
| `cocktails`            | Recette : `name`, `instructions`, `glass`, `category`, `strength`, `alcoholic`, `season`, `image_name` |
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
├── cocktails.go                Lecture et recherche des cocktails (list, get, search)
├── referentials.go             Référentiels de lecture (ingredients, categories, tags)
├── images.go                   Service d'images : validation du nom et containment du chemin
├── localauth.go                Garde de l'écriture locale (loopback + token bearer)
├── writes.go                   Création d'un cocktail et upload d'image (endpoint local)
├── respond.go                  Helpers de réponse JSON et format d'erreur uniforme
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

Toute la surface v1 décrite dans `api/openapi.yaml` est implémentée et exposée.
Les routes de lecture sont publiques ; l'écriture (`POST /cocktails`) n'est
montée que si `LOCAL_WRITE_TOKEN` est défini, et reste réservée aux appels
loopback authentifiés par token.

| Méthode | Chemin              | Description                                        | Réponse                     |
| ------- | ------------------- | -------------------------------------------------- | --------------------------- |
| `GET`   | `/cocktails`        | Liste paginée et filtrable des cocktails           | `200` `CocktailList`        |
| `GET`   | `/cocktails/search` | Recherche par ingrédients (`match=all\|any`)       | `200` `CocktailList`        |
| `GET`   | `/cocktails/{id}`   | Détail d'un cocktail                               | `200` `Cocktail`            |
| `GET`   | `/ingredients`      | Référentiel des ingrédients                        | `200` (array)               |
| `GET`   | `/categories`       | Catégories distinctes des cocktails                | `200` (array)               |
| `GET`   | `/tags`             | Tags distincts des cocktails                       | `200` (array)               |
| `GET`   | `/images/{name}`    | Image d'un cocktail                                | `200` (image/\*)            |
| `POST`  | `/cocktails`        | Ajout d'une recette et upload d'image (local)      | `201` `Cocktail`            |
| `GET`   | `/health`           | Sonde de vivacité                                  | `200 {"status":"ok"}`       |
| `GET`   | `/openapi.yaml`     | Contrat OpenAPI (spec brute)                       | `200` (application/yaml)    |
| `GET`   | `/docs`             | Documentation Redoc                                | `200` (text/html)           |

## Format d'erreur

Toute erreur renvoie un corps JSON uniforme avec un code stable lisible par machine
(`error`) et un message humain (`message`) :

```json
{ "error": "not_found", "message": "cocktail not found" }
```

| Code                | Statut HTTP | Quand il survient                                                                 |
| ------------------- | ----------- | --------------------------------------------------------------------------------- |
| `bad_request`       | `400`       | Paramètre de pagination/filtre invalide, corps multipart mal formé, part `cocktail` manquante, échec de validation, type d'image non supporté |
| `unauthorized`      | `401`       | Écriture locale : bearer absent ou ne correspondant pas à `LOCAL_WRITE_TOKEN`     |
| `forbidden`         | `403`       | Écriture locale : requête émise depuis une origine non loopback                   |
| `not_found`         | `404`       | Cocktail introuvable, ou nom d'image invalide / fichier absent                    |
| `payload_too_large` | `413`       | Image au-delà de 2 Mo, ou corps multipart global trop volumineux                  |
| `internal_error`    | `500`       | Erreur interne (base de données, I/O)                                             |

Le champ `message` précise la cause exacte (`invalid limit parameter`,
`invalid cocktail id`, `missing cocktail part`, `name is required`,
`unsupported image type`, `image not found`, etc.).

## Modèle de données (DTO API)

Les objets **renvoyés** par l'API sont distincts du schéma SQL. Le chemin de
stockage sur disque n'est jamais exposé : seul le **nom** du fichier image est
renvoyé, via le champ `image` (issu de la colonne `image_name`).

`Cocktail` :

| Champ          | Type              | Détail                                                            |
| -------------- | ----------------- | ----------------------------------------------------------------- |
| `id`           | entier            | Identifiant                                                       |
| `name`         | chaîne            | Nom de la recette                                                 |
| `instructions` | chaîne            | Préparation                                                       |
| `glass`        | chaîne            | Type de verre                                                     |
| `category`     | chaîne            | Catégorie                                                         |
| `strength`     | chaîne            | Force                                                             |
| `alcoholic`    | booléen           | Alcoolisé ou non                                                  |
| `season`       | chaîne            | Saison (peut être vide)                                          |
| `image`        | chaîne            | Nom de fichier image (vide si absente), à utiliser sur `/images/{name}` — jamais le chemin disque |
| `tags`         | tableau de chaînes | Tags triés par ordre alphabétique                                |
| `ingredients`  | tableau de `CocktailIngredient` | Ingrédients triés par nom                          |

`CocktailIngredient` :

| Champ      | Type   | Détail                        |
| ---------- | ------ | ----------------------------- |
| `name`     | chaîne | Nom de l'ingrédient           |
| `quantity` | chaîne | Quantité (peut être vide)     |
| `unit`     | chaîne | Unité (peut être vide)        |

`CocktailList` (enveloppe de liste paginée renvoyée par `/cocktails` et `/cocktails/search`) :

| Champ    | Type                  | Détail                                                  |
| -------- | --------------------- | ------------------------------------------------------- |
| `items`  | tableau de `Cocktail` | Page courante                                           |
| `total`  | entier                | Nombre total d'éléments correspondant au filtre/critère |
| `limit`  | entier                | Taille de page appliquée (défaut `20`, borne `1..100`)  |
| `offset` | entier                | Décalage appliqué (défaut `0`, borne `0..100000`)       |

Les référentiels (`/ingredients`, `/categories`, `/tags`) renvoient directement
des tableaux : `/ingredients` un tableau d'`{ id, name }`, `/categories` et
`/tags` des tableaux de chaînes.

## Exemples requête → réponse

Liste paginée et filtrée :

```bash
curl "http://127.0.0.1:8080/cocktails?category=Classic&limit=2&offset=0"
```

```json
{
  "items": [
    { "id": 1, "name": "Margarita", "category": "Classic", "strength": "Strong", "alcoholic": true, "season": "Summer", "image": "margarita.jpg", "tags": ["iba", "sour", "tequila"], "ingredients": [ { "name": "Tequila", "quantity": "50", "unit": "ml" } ] }
  ],
  "total": 2,
  "limit": 2,
  "offset": 0
}
```

Recherche par ingrédients. `match=all` (défaut) exige **tous** les ingrédients
fournis, `match=any` en exige **au moins un**. Un ingrédient inconnu réduit
simplement les correspondances (jusqu'à une liste vide) sans provoquer d'erreur :

```bash
curl "http://127.0.0.1:8080/cocktails/search?ingredients=lime%20juice,mint&match=all"
```

```json
{ "items": [ { "id": 2, "name": "Mojito" }, { "id": 3, "name": "Virgin Mojito" } ], "total": 2, "limit": 20, "offset": 0 }
```

```bash
curl "http://127.0.0.1:8080/cocktails/search?ingredients=unicorn%20tears&match=any"
```

```json
{ "items": [], "total": 0, "limit": 20, "offset": 0 }
```

Détail par identifiant :

```bash
curl "http://127.0.0.1:8080/cocktails/1"
```

```json
{ "id": 1, "name": "Margarita", "instructions": "Rub the rim of the glass with lime and dip it in salt. Shake tequila, triple sec and lime juice with ice, then strain into the glass.", "glass": "Margarita glass", "category": "Classic", "strength": "Strong", "alcoholic": true, "season": "Summer", "image": "margarita.jpg", "tags": ["iba", "sour", "tequila"], "ingredients": [ { "name": "Lime juice", "quantity": "15", "unit": "ml" }, { "name": "Salt", "quantity": "", "unit": "rim" }, { "name": "Tequila", "quantity": "50", "unit": "ml" }, { "name": "Triple sec", "quantity": "20", "unit": "ml" } ] }
```

Création multipart (écriture locale, appel loopback + bearer) :

```bash
curl -X POST "http://127.0.0.1:8080/cocktails" \
  -H "Authorization: Bearer $LOCAL_WRITE_TOKEN" \
  -F 'cocktail={"name":"Negroni","instructions":"Stir gin, Campari and sweet vermouth with ice, then strain over a large ice cube and garnish with an orange peel.","glass":"Rocks glass","category":"Classic","strength":"Strong","alcoholic":true,"season":"All year","tags":["gin","bitter","iba"],"ingredients":[{"name":"Gin","quantity":"30","unit":"ml"},{"name":"Campari","quantity":"30","unit":"ml"},{"name":"Sweet vermouth","quantity":"30","unit":"ml"}]};type=application/json' \
  -F 'image=@negroni.jpg;type=image/jpeg'
```

Réponse `201` : le `Cocktail` créé, avec `image` égal au nom généré par le
serveur (`{id}.{ext}`, ex. `6.jpg`).

## Écriture locale sécurisée

L'écriture (`POST /cocktails`) est gardée par `localauth.go` et n'est **montée
que si `LOCAL_WRITE_TOKEN` est défini** (sinon la route n'existe pas, cf.
`main.go`). Deux contrôles s'appliquent, dans cet ordre :

1. **Origine loopback** — l'adresse distante de la requête doit être une adresse
   de bouclage (`127.0.0.1`, `::1`). Toute autre origine reçoit `403 forbidden`.
   Le service se lie par défaut à `127.0.0.1` (`BIND_ADDR`), ce qui restreint
   déjà l'exposition.
2. **Token bearer** — l'en-tête `Authorization: Bearer <token>` doit correspondre
   exactement à `LOCAL_WRITE_TOKEN`. La comparaison se fait en **temps constant**
   (`crypto/subtle`) pour éviter les attaques temporelles. Un token absent,
   malformé ou incorrect reçoit `401 unauthorized`.

Configuration et appel en local :

```bash
export LOCAL_WRITE_TOKEN="un-secret-long-et-aleatoire"
go run .

curl -X POST "http://127.0.0.1:8080/cocktails" \
  -H "Authorization: Bearer $LOCAL_WRITE_TOKEN" \
  -F 'cocktail={"name":"Negroni","instructions":"...","glass":"Rocks glass","category":"Classic","strength":"Strong","alcoholic":true};type=application/json' \
  -F 'image=@negroni.jpg;type=image/jpeg'
```

Le token est un secret : il n'est ni commité ni loggé. L'image est optionnelle ;
si elle est fournie, son type réel doit être jpeg, png ou webp, et sa taille ne
doit pas dépasser 2 Mo.
