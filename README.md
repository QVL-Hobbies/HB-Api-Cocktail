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

## Structure du repo

```
.
├── main.go        Point d'entrée : chargement config, serveur HTTP, arrêt gracieux
├── config.go      Chargement de la configuration depuis l'environnement
├── health.go      Handler GET /health
├── go.mod
├── tools/         Scripts d'outillage local (voir tools/README.md)
├── .gitignore
└── README.md
```

## Endpoints

| Méthode | Chemin    | Description        | Réponse            |
| ------- | --------- | ------------------ | ------------------ |
| `GET`   | `/health` | Sonde de vivacité  | `200 {"status":"ok"}` |
