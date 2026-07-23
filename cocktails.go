package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type cocktailIngredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type cocktail struct {
	ID           int64                `json:"id"`
	Name         string               `json:"name"`
	Instructions string               `json:"instructions"`
	Glass        string               `json:"glass"`
	Category     string               `json:"category"`
	Strength     string               `json:"strength"`
	Alcoholic    bool                 `json:"alcoholic"`
	Season       string               `json:"season"`
	Image        string               `json:"image"`
	Tags         []string             `json:"tags"`
	Ingredients  []cocktailIngredient `json:"ingredients"`
}

type cocktailList struct {
	Items  []cocktail `json:"items"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

const selectCocktailColumns = "SELECT id, name, instructions, glass, category, strength, alcoholic, season, image_name FROM cocktails"

func handleListCocktails(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		var conditions []string
		var args []any

		if v := query.Get("category"); v != "" {
			conditions = append(conditions, "category = ?")
			args = append(args, v)
		}
		if v := query.Get("strength"); v != "" {
			conditions = append(conditions, "strength = ?")
			args = append(args, v)
		}
		if v := query.Get("season"); v != "" {
			conditions = append(conditions, "season = ?")
			args = append(args, v)
		}
		if v := query.Get("tag"); v != "" {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM cocktail_tags WHERE cocktail_id = cocktails.id AND tag = ?)")
			args = append(args, v)
		}
		if query.Has("alcoholic") {
			value, ok := parseAlcoholic(query.Get("alcoholic"))
			if !ok {
				writeError(w, http.StatusBadRequest, "bad_request", "invalid alcoholic filter")
				return
			}
			conditions = append(conditions, "alcoholic = ?")
			args = append(args, value)
		}

		limit, ok := parseLimit(query.Get("limit"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid limit parameter")
			return
		}
		offset, ok := parseOffset(query.Get("offset"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid offset parameter")
			return
		}

		where := ""
		if len(conditions) > 0 {
			where = " WHERE " + strings.Join(conditions, " AND ")
		}

		ctx := r.Context()

		var total int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cocktails"+where, args...).Scan(&total); err != nil {
			writeInternalError(w)
			return
		}

		pageArgs := append(append([]any{}, args...), limit, offset)
		rows, err := db.QueryContext(ctx, selectCocktailColumns+where+" ORDER BY id LIMIT ? OFFSET ?", pageArgs...)
		if err != nil {
			writeInternalError(w)
			return
		}
		defer rows.Close()

		items := []cocktail{}
		ids := []int64{}
		index := map[int64]int{}
		for rows.Next() {
			c, err := scanCocktail(rows)
			if err != nil {
				writeInternalError(w)
				return
			}
			index[c.ID] = len(items)
			items = append(items, c)
			ids = append(ids, c.ID)
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w)
			return
		}

		if err := attachTags(ctx, db, items, ids, index); err != nil {
			writeInternalError(w)
			return
		}
		if err := attachIngredients(ctx, db, items, ids, index); err != nil {
			writeInternalError(w)
			return
		}

		writeJSON(w, http.StatusOK, cocktailList{
			Items:  items,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func handleGetCocktail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid cocktail id")
			return
		}

		c, err := loadCocktail(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "cocktail not found")
				return
			}
			writeInternalError(w)
			return
		}

		writeJSON(w, http.StatusOK, c)
	}
}

func loadCocktail(ctx context.Context, db *sql.DB, id int64) (cocktail, error) {
	row := db.QueryRowContext(ctx, selectCocktailColumns+" WHERE id = ?", id)
	c, err := scanCocktail(row)
	if err != nil {
		return cocktail{}, err
	}

	items := []cocktail{c}
	ids := []int64{c.ID}
	index := map[int64]int{c.ID: 0}
	if err := attachTags(ctx, db, items, ids, index); err != nil {
		return cocktail{}, err
	}
	if err := attachIngredients(ctx, db, items, ids, index); err != nil {
		return cocktail{}, err
	}
	return items[0], nil
}

func handleSearchCocktails(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		names, ok := parseIngredientNames(query.Get("ingredients"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid ingredients parameter")
			return
		}
		mode, ok := parseMatchMode(query.Get("match"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid match parameter")
			return
		}
		limit, ok := parseLimit(query.Get("limit"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid limit parameter")
			return
		}
		offset, ok := parseOffset(query.Get("offset"))
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid offset parameter")
			return
		}

		ctx := r.Context()
		matching, matchingArgs := matchingCocktailsQuery(mode, names)

		var total int
		countArgs := append([]any{}, matchingArgs...)
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+matching+")", countArgs...).Scan(&total); err != nil {
			writeInternalError(w)
			return
		}

		pageArgs := append(append([]any{}, matchingArgs...), limit, offset)
		rows, err := db.QueryContext(ctx, selectCocktailColumns+" WHERE id IN ("+matching+") ORDER BY id LIMIT ? OFFSET ?", pageArgs...)
		if err != nil {
			writeInternalError(w)
			return
		}
		defer rows.Close()

		items := []cocktail{}
		ids := []int64{}
		index := map[int64]int{}
		for rows.Next() {
			c, err := scanCocktail(rows)
			if err != nil {
				writeInternalError(w)
				return
			}
			index[c.ID] = len(items)
			items = append(items, c)
			ids = append(ids, c.ID)
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w)
			return
		}

		if err := attachTags(ctx, db, items, ids, index); err != nil {
			writeInternalError(w)
			return
		}
		if err := attachIngredients(ctx, db, items, ids, index); err != nil {
			writeInternalError(w)
			return
		}

		writeJSON(w, http.StatusOK, cocktailList{
			Items:  items,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func matchingCocktailsQuery(mode string, names []string) (string, []any) {
	placeholders := sqlPlaceholders(len(names))
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	from := "FROM cocktail_ingredients ci JOIN ingredients i ON i.id = ci.ingredient_id WHERE LOWER(i.name) IN (" + placeholders + ")"
	if mode == matchAll {
		return "SELECT ci.cocktail_id " + from + " GROUP BY ci.cocktail_id HAVING COUNT(DISTINCT LOWER(i.name)) = ?", append(args, len(names))
	}
	return "SELECT DISTINCT ci.cocktail_id " + from, args
}

func scanCocktail(s rowScanner) (cocktail, error) {
	var c cocktail
	var alcoholic int64
	if err := s.Scan(&c.ID, &c.Name, &c.Instructions, &c.Glass, &c.Category, &c.Strength, &alcoholic, &c.Season, &c.Image); err != nil {
		return cocktail{}, err
	}
	c.Alcoholic = alcoholic != 0
	c.Tags = []string{}
	c.Ingredients = []cocktailIngredient{}
	return c, nil
}

func attachTags(ctx context.Context, db *sql.DB, items []cocktail, ids []int64, index map[int64]int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders, args := inPlaceholders(ids)
	rows, err := db.QueryContext(ctx, "SELECT cocktail_id, tag FROM cocktail_tags WHERE cocktail_id IN ("+placeholders+") ORDER BY cocktail_id, tag", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cocktailID int64
		var tag string
		if err := rows.Scan(&cocktailID, &tag); err != nil {
			return err
		}
		if i, ok := index[cocktailID]; ok {
			items[i].Tags = append(items[i].Tags, tag)
		}
	}
	return rows.Err()
}

func attachIngredients(ctx context.Context, db *sql.DB, items []cocktail, ids []int64, index map[int64]int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders, args := inPlaceholders(ids)
	rows, err := db.QueryContext(ctx, "SELECT ci.cocktail_id, i.name, ci.quantity, ci.unit FROM cocktail_ingredients ci JOIN ingredients i ON i.id = ci.ingredient_id WHERE ci.cocktail_id IN ("+placeholders+") ORDER BY ci.cocktail_id, i.name", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cocktailID int64
		var ing cocktailIngredient
		if err := rows.Scan(&cocktailID, &ing.Name, &ing.Quantity, &ing.Unit); err != nil {
			return err
		}
		if i, ok := index[cocktailID]; ok {
			items[i].Ingredients = append(items[i].Ingredients, ing)
		}
	}
	return rows.Err()
}

func sqlPlaceholders(n int) string {
	marks := make([]string, n)
	for i := range marks {
		marks[i] = "?"
	}
	return strings.Join(marks, ", ")
}

func inPlaceholders(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return sqlPlaceholders(len(ids)), args
}

const (
	matchAll           = "all"
	matchAny           = "any"
	maxIngredientNames = 20
)

func parseIngredientNames(raw string) ([]string, bool) {
	seen := map[string]struct{}{}
	names := []string{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 || len(names) > maxIngredientNames {
		return nil, false
	}
	return names, true
}

func parseMatchMode(raw string) (string, bool) {
	switch raw {
	case "":
		return matchAll, true
	case matchAll, matchAny:
		return raw, true
	default:
		return "", false
	}
}

func parseAlcoholic(value string) (int, bool) {
	switch value {
	case "true":
		return 1, true
	case "false":
		return 0, true
	default:
		return 0, false
	}
}

func parseLimit(value string) (int, bool) {
	if value == "" {
		return 20, true
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 || n > 100 {
		return 0, false
	}
	return int(n), true
}

const maxOffset = 100000

func parseOffset(value string) (int, bool) {
	if value == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 || n > maxOffset {
		return 0, false
	}
	return int(n), true
}
