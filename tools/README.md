# tools/

Dossier des scripts maison (outillage DevOps / dev) du service **HB-Api-Cocktail**.

## Vocation

Regrouper les scripts utilitaires liés au cycle de vie local du service :
seed de données, helpers de lancement/arrêt, tâches de maintenance ponctuelles.

Ce dossier ne contient **pas** de code métier du service (les sources Go vivent
à la racine / dans les paquets applicatifs). Il est destiné à accueillir, au fil
des US, des outils dédiés — par exemple le script de seed prévu à l'US2.

## Contenu

### `seed/`

Script de chargement d'un jeu de recettes de test dans une base SQLite locale
(cocktails, ingrédients, liaisons avec quantités, tags).

```bash
go run ./tools/seed
```

- `DB_PATH` (défaut `./data/cocktail.db`) : base cible, schéma créé si absent.
- `SEED_FILE` (défaut `tools/seed/data.json`) : jeu de données à charger.

Le seed charge un dataset complet et repart d'un état vide : les tables
(`cocktails`, `ingredients` et leurs liaisons) sont purgées au sein de la
transaction avant insertion. Il est donc ré-exécutable sans créer de doublon —
relancer la commande laisse exactement le même contenu.

`tools/seed/data.json` est un **jeu de test non versionné** (gitignoré). Le
fichier `.db` produit reste également hors git.
