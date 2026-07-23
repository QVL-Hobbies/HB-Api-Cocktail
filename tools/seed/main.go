package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"hb-api-cocktail/internal/database"
)

type seedIngredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type seedCocktail struct {
	Name         string           `json:"name"`
	Instructions string           `json:"instructions"`
	Glass        string           `json:"glass"`
	Category     string           `json:"category"`
	Strength     string           `json:"strength"`
	Alcoholic    bool             `json:"alcoholic"`
	Season       string           `json:"season"`
	ImageName    string           `json:"imageName"`
	Ingredients  []seedIngredient `json:"ingredients"`
	Tags         []string         `json:"tags"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dbPath := envOrDefault("DB_PATH", "./data/cocktail.db")
	seedFile := envOrDefault("SEED_FILE", filepath.Join("tools", "seed", "data.json"))

	cocktails, err := loadSeedFile(seedFile)
	if err != nil {
		return err
	}

	db, err := database.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	inserted, err := insertCocktails(db, cocktails)
	if err != nil {
		return err
	}

	log.Printf("seeded %d cocktails into %s", inserted, dbPath)
	return nil
}

func loadSeedFile(path string) ([]seedCocktail, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read seed file %q: %w", path, err)
	}

	var cocktails []seedCocktail
	if err := json.Unmarshal(raw, &cocktails); err != nil {
		return nil, fmt.Errorf("parse seed file %q: %w", path, err)
	}
	return cocktails, nil
}

func insertCocktails(db *sql.DB, cocktails []seedCocktail) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := clearSeededData(tx); err != nil {
		return 0, err
	}

	for _, cocktail := range cocktails {
		if err := insertCocktail(tx, cocktail); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return len(cocktails), nil
}

func clearSeededData(tx *sql.Tx) error {
	statements := []string{
		`DELETE FROM cocktail_tags`,
		`DELETE FROM cocktail_ingredients`,
		`DELETE FROM ingredients`,
		`DELETE FROM cocktails`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("clear seeded data (%s): %w", statement, err)
		}
	}
	return nil
}

func insertCocktail(tx *sql.Tx, cocktail seedCocktail) error {
	result, err := tx.Exec(
		`INSERT INTO cocktails (name, instructions, glass, category, strength, alcoholic, season, image_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cocktail.Name, cocktail.Instructions, cocktail.Glass, cocktail.Category,
		cocktail.Strength, cocktail.Alcoholic, cocktail.Season, cocktail.ImageName,
	)
	if err != nil {
		return fmt.Errorf("insert cocktail %q: %w", cocktail.Name, err)
	}

	cocktailID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("resolve cocktail id for %q: %w", cocktail.Name, err)
	}

	for _, ingredient := range cocktail.Ingredients {
		if err := linkIngredient(tx, cocktailID, ingredient); err != nil {
			return fmt.Errorf("link ingredient for %q: %w", cocktail.Name, err)
		}
	}

	for _, tag := range cocktail.Tags {
		if err := linkTag(tx, cocktailID, tag); err != nil {
			return fmt.Errorf("link tag for %q: %w", cocktail.Name, err)
		}
	}
	return nil
}

func linkIngredient(tx *sql.Tx, cocktailID int64, ingredient seedIngredient) error {
	ingredientID, err := ensureIngredient(tx, ingredient.Name)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		`INSERT OR IGNORE INTO cocktail_ingredients (cocktail_id, ingredient_id, quantity, unit)
		 VALUES (?, ?, ?, ?)`,
		cocktailID, ingredientID, ingredient.Quantity, ingredient.Unit,
	)
	return err
}

func ensureIngredient(tx *sql.Tx, name string) (int64, error) {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO ingredients (name) VALUES (?)`, name); err != nil {
		return 0, err
	}

	var id int64
	if err := tx.QueryRow(`SELECT id FROM ingredients WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func linkTag(tx *sql.Tx, cocktailID int64, tag string) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO cocktail_tags (cocktail_id, tag) VALUES (?, ?)`,
		cocktailID, tag,
	)
	return err
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
