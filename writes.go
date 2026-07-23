package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxImageSize    = 2 << 20
	maxRequestBody  = maxImageSize + (1 << 20)
	multipartMemory = 1 << 20
)

const (
	maxIngredients       = 50
	maxTags              = 30
	maxNameLen           = 200
	maxInstructionsLen   = 5000
	maxGlassLen          = 100
	maxCategoryLen       = 100
	maxStrengthLen       = 100
	maxSeasonLen         = 100
	maxIngredientNameLen = 100
	maxUnitLen           = 50
	maxQuantityLen       = 50
)

type cocktailIngredientInput struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type cocktailInput struct {
	Name         string                    `json:"name"`
	Instructions string                    `json:"instructions"`
	Glass        string                    `json:"glass"`
	Category     string                    `json:"category"`
	Strength     string                    `json:"strength"`
	Alcoholic    *bool                     `json:"alcoholic"`
	Season       string                    `json:"season"`
	Tags         []string                  `json:"tags"`
	Ingredients  []cocktailIngredientInput `json:"ingredients"`
}

func handleCreateCocktail(db *sql.DB, config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		if err := r.ParseMultipartForm(multipartMemory); err != nil {
			if isPayloadTooLarge(err) {
				writePayloadTooLarge(w)
				return
			}
			writeError(w, http.StatusBadRequest, "bad_request", "invalid multipart form")
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		input, ok := decodeCocktailInput(w, r)
		if !ok {
			return
		}

		imageContent, imageExt, ok := readImagePart(w, r)
		if !ok {
			return
		}

		created, ok := persistCocktail(r.Context(), w, db, config, input, imageContent, imageExt)
		if !ok {
			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

func decodeCocktailInput(w http.ResponseWriter, r *http.Request) (cocktailInput, bool) {
	raw := r.FormValue("cocktail")
	if strings.TrimSpace(raw) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing cocktail part")
		return cocktailInput{}, false
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var input cocktailInput
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid cocktail payload")
		return cocktailInput{}, false
	}
	if decoder.More() {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid cocktail payload")
		return cocktailInput{}, false
	}
	if message, valid := validateCocktailInput(input); !valid {
		writeError(w, http.StatusBadRequest, "bad_request", message)
		return cocktailInput{}, false
	}
	return input, true
}

func validateCocktailInput(in cocktailInput) (string, bool) {
	required := []struct {
		field string
		value string
		max   int
	}{
		{"name", in.Name, maxNameLen},
		{"instructions", in.Instructions, maxInstructionsLen},
		{"glass", in.Glass, maxGlassLen},
		{"category", in.Category, maxCategoryLen},
		{"strength", in.Strength, maxStrengthLen},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return field.field + " is required", false
		}
		if utf8.RuneCountInString(field.value) > field.max {
			return field.field + " is too long", false
		}
	}
	if utf8.RuneCountInString(in.Season) > maxSeasonLen {
		return "season is too long", false
	}
	if in.Alcoholic == nil {
		return "alcoholic is required", false
	}
	if len(in.Ingredients) > maxIngredients {
		return "too many ingredients", false
	}
	for _, ingredient := range in.Ingredients {
		if strings.TrimSpace(ingredient.Name) == "" {
			return "ingredient name is required", false
		}
		if utf8.RuneCountInString(ingredient.Name) > maxIngredientNameLen {
			return "ingredient name is too long", false
		}
		if utf8.RuneCountInString(ingredient.Quantity) > maxQuantityLen {
			return "ingredient quantity is too long", false
		}
		if utf8.RuneCountInString(ingredient.Unit) > maxUnitLen {
			return "ingredient unit is too long", false
		}
	}
	if len(in.Tags) > maxTags {
		return "too many tags", false
	}
	for _, tag := range in.Tags {
		if strings.TrimSpace(tag) == "" {
			return "tag must not be empty", false
		}
	}
	return "", true
}

func readImagePart(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	file, header, err := r.FormFile("image")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return nil, "", true
		}
		if isPayloadTooLarge(err) {
			writePayloadTooLarge(w)
			return nil, "", false
		}
		writeError(w, http.StatusBadRequest, "bad_request", "invalid image part")
		return nil, "", false
	}
	defer file.Close()

	if header.Size > maxImageSize {
		writePayloadTooLarge(w)
		return nil, "", false
	}

	content, err := io.ReadAll(io.LimitReader(file, maxImageSize+1))
	if err != nil {
		if isPayloadTooLarge(err) {
			writePayloadTooLarge(w)
			return nil, "", false
		}
		writeInternalError(w)
		return nil, "", false
	}
	if int64(len(content)) > maxImageSize {
		writePayloadTooLarge(w)
		return nil, "", false
	}

	ext, ok := detectImageExtension(content)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "unsupported image type")
		return nil, "", false
	}
	return content, ext, true
}

func persistCocktail(ctx context.Context, w http.ResponseWriter, db *sql.DB, config Config, in cocktailInput, imageContent []byte, imageExt string) (cocktail, bool) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w)
		return cocktail{}, false
	}
	defer tx.Rollback()

	id, err := insertCocktailRow(ctx, tx, in)
	if err != nil {
		writeInternalError(w)
		return cocktail{}, false
	}

	for _, ingredient := range in.Ingredients {
		if err := linkCocktailIngredient(ctx, tx, id, ingredient); err != nil {
			writeInternalError(w)
			return cocktail{}, false
		}
	}
	for _, tag := range in.Tags {
		if err := linkCocktailTag(ctx, tx, id, tag); err != nil {
			writeInternalError(w)
			return cocktail{}, false
		}
	}

	storagePath := ""
	if imageContent != nil {
		imageName := strconv.FormatInt(id, 10) + imageExt
		storagePath, err = containedPath(config.ImagesDir, imageName)
		if err != nil {
			writeInternalError(w)
			return cocktail{}, false
		}
		if _, err := tx.ExecContext(ctx, "UPDATE cocktails SET image_name = ?, image_path = ? WHERE id = ?", imageName, storagePath, id); err != nil {
			writeInternalError(w)
			return cocktail{}, false
		}
		if err := storeImageFile(storagePath, imageContent); err != nil {
			writeInternalError(w)
			return cocktail{}, false
		}
	}

	if err := tx.Commit(); err != nil {
		if storagePath != "" {
			_ = os.Remove(storagePath)
		}
		writeInternalError(w)
		return cocktail{}, false
	}

	created, err := loadCocktail(ctx, db, id)
	if err != nil {
		writeInternalError(w)
		return cocktail{}, false
	}
	return created, true
}

func insertCocktailRow(ctx context.Context, tx *sql.Tx, in cocktailInput) (int64, error) {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO cocktails (name, instructions, glass, category, strength, alcoholic, season)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Instructions, in.Glass, in.Category, in.Strength, *in.Alcoholic, in.Season,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func linkCocktailIngredient(ctx context.Context, tx *sql.Tx, cocktailID int64, ingredient cocktailIngredientInput) error {
	ingredientID, err := ensureIngredientID(ctx, tx, ingredient.Name)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO cocktail_ingredients (cocktail_id, ingredient_id, quantity, unit)
		 VALUES (?, ?, ?, ?)`,
		cocktailID, ingredientID, ingredient.Quantity, ingredient.Unit,
	)
	return err
}

func ensureIngredientID(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ingredients (name) VALUES (?)`, name); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM ingredients WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func linkCocktailTag(ctx context.Context, tx *sql.Tx, cocktailID int64, tag string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO cocktail_tags (cocktail_id, tag) VALUES (?, ?)`,
		cocktailID, tag,
	)
	return err
}

func isPayloadTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func writePayloadTooLarge(w http.ResponseWriter) {
	writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request payload too large")
}
