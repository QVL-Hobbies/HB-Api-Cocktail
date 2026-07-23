package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hb-api-cocktail/internal/database"
)

type ingredientResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func setupReferentialsServer(t *testing.T, seed func(*sql.DB)) http.Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if seed != nil {
		seed(db)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ingredients", handleListIngredients(db))
	mux.HandleFunc("GET /categories", handleListCategories(db))
	mux.HandleFunc("GET /tags", handleListTags(db))
	return withSecurityHeaders(mux)
}

func insertCocktail(t *testing.T, db *sql.DB, id int64, name, category string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO cocktails (id, name, instructions, glass, category, strength, alcoholic) VALUES (?, ?, '', '', ?, '', 0)`,
		id, name, category,
	); err != nil {
		t.Fatalf("insert cocktail %q: %v", name, err)
	}
}

func insertTag(t *testing.T, db *sql.DB, cocktailID int64, tag string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO cocktail_tags (cocktail_id, tag) VALUES (?, ?)`, cocktailID, tag); err != nil {
		t.Fatalf("insert tag %q: %v", tag, err)
	}
}

func insertIngredient(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO ingredients (name) VALUES (?)`, name); err != nil {
		t.Fatalf("insert ingredient %q: %v", name, err)
	}
}

func linkIngredient(t *testing.T, db *sql.DB, cocktailID int64, name string) {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM ingredients WHERE name = ?`, name).Scan(&id); err != nil {
		t.Fatalf("lookup ingredient %q: %v", name, err)
	}
	if _, err := db.Exec(
		`INSERT INTO cocktail_ingredients (cocktail_id, ingredient_id, quantity, unit) VALUES (?, ?, '', '')`,
		cocktailID, id,
	); err != nil {
		t.Fatalf("link ingredient %q: %v", name, err)
	}
}

func decodeStringArray(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var values []string
	if err := json.Unmarshal(rec.Body.Bytes(), &values); err != nil {
		t.Fatalf("decode string array: %v (body: %s)", err, rec.Body.String())
	}
	return values
}

func ingredientNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var items []ingredientResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode ingredients: %v (body: %s)", err, rec.Body.String())
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func assertEqualSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func assertEachElementIsJSONString(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode array: %v (body: %s)", err, rec.Body.String())
	}
	for _, el := range raw {
		var s string
		if err := json.Unmarshal(el, &s); err != nil {
			t.Fatalf("expected each element to be a JSON string, got %s", string(el))
		}
	}
}

func TestListIngredientsReturnsObjectsWithIdAndName(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertIngredient(t, db, "Tequila")
		insertIngredient(t, db, "Mint")
	})

	rec := doGet(t, handler, "/ingredients")

	assertStatus(t, rec, http.StatusOK)
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode ingredient array: %v (body: %s)", err, rec.Body.String())
	}
	if len(raw) != 2 {
		t.Fatalf("expected 2 ingredients, got %d", len(raw))
	}
	for _, item := range raw {
		idField, hasID := item["id"]
		nameField, hasName := item["name"]
		if !hasID || !hasName {
			t.Fatalf("ingredient must expose both id and name, got %v", item)
		}
		var idValue int64
		if err := json.Unmarshal(idField, &idValue); err != nil {
			t.Fatalf("id must be a JSON number, got %s", string(idField))
		}
		if idValue <= 0 {
			t.Fatalf("expected a positive id, got %d", idValue)
		}
		var nameValue string
		if err := json.Unmarshal(nameField, &nameValue); err != nil {
			t.Fatalf("name must be a JSON string, got %s", string(nameField))
		}
		if nameValue == "" {
			t.Fatalf("name must be non-empty")
		}
	}
}

func TestListIngredientsSortedAscendingByName(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertIngredient(t, db, "Zebra")
		insertIngredient(t, db, "Apple")
		insertIngredient(t, db, "Mango")
	})

	rec := doGet(t, handler, "/ingredients")

	assertStatus(t, rec, http.StatusOK)
	assertEqualSlice(t, ingredientNames(t, rec), []string{"Apple", "Mango", "Zebra"})
}

func TestListIngredientsIncludesIngredientNotLinkedToCocktail(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertCocktail(t, db, 1, "Mojito", "Classic")
		insertIngredient(t, db, "Mint")
		insertIngredient(t, db, "Orphan Bitters")
		linkIngredient(t, db, 1, "Mint")
	})

	rec := doGet(t, handler, "/ingredients")

	assertStatus(t, rec, http.StatusOK)
	names := ingredientNames(t, rec)
	if !containsString(names, "Mint") {
		t.Fatalf("expected linked ingredient Mint, got %v", names)
	}
	if !containsString(names, "Orphan Bitters") {
		t.Fatalf("expected unlinked ingredient Orphan Bitters to be listed, got %v", names)
	}
}

func TestListCategoriesReturnsDistinctSortedStrings(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertCocktail(t, db, 1, "A", "Zebra")
		insertCocktail(t, db, 2, "B", "Apple")
		insertCocktail(t, db, 3, "C", "Apple")
		insertCocktail(t, db, 4, "D", "Mango")
	})

	rec := doGet(t, handler, "/categories")

	assertStatus(t, rec, http.StatusOK)
	assertEachElementIsJSONString(t, rec)
	assertEqualSlice(t, decodeStringArray(t, rec), []string{"Apple", "Mango", "Zebra"})
}

func TestListTagsReturnsDistinctSortedStrings(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertCocktail(t, db, 1, "A", "Classic")
		insertCocktail(t, db, 2, "B", "Classic")
		insertTag(t, db, 1, "sour")
		insertTag(t, db, 1, "iba")
		insertTag(t, db, 2, "sour")
		insertTag(t, db, 2, "mint")
	})

	rec := doGet(t, handler, "/tags")

	assertStatus(t, rec, http.StatusOK)
	assertEachElementIsJSONString(t, rec)
	assertEqualSlice(t, decodeStringArray(t, rec), []string{"iba", "mint", "sour"})
}

func TestCategoryAndTagAreDistinctNotions(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertCocktail(t, db, 1, "A", "classic")
		insertTag(t, db, 1, "classic")
	})

	catRec := doGet(t, handler, "/categories")
	assertStatus(t, catRec, http.StatusOK)
	categories := decodeStringArray(t, catRec)
	if !containsString(categories, "classic") {
		t.Fatalf("expected 'classic' among categories, got %v", categories)
	}

	tagRec := doGet(t, handler, "/tags")
	assertStatus(t, tagRec, http.StatusOK)
	tags := decodeStringArray(t, tagRec)
	if !containsString(tags, "classic") {
		t.Fatalf("expected 'classic' among tags, got %v", tags)
	}
}

func TestReferentialsEmptyDatabaseReturnEmptyArrayNotNull(t *testing.T) {
	handler := setupReferentialsServer(t, nil)

	for _, route := range []string{"/ingredients", "/categories", "/tags"} {
		rec := doGet(t, handler, route)
		assertStatus(t, rec, http.StatusOK)
		body := strings.TrimSpace(rec.Body.String())
		if body != "[]" {
			t.Fatalf("%s: expected [] for empty database, got %s", route, body)
		}
	}
}

func TestReferentialResponsesUseJSONAndSecurityHeader(t *testing.T) {
	handler := setupReferentialsServer(t, func(db *sql.DB) {
		insertCocktail(t, db, 1, "Mojito", "Classic")
		insertTag(t, db, 1, "refreshing")
		insertIngredient(t, db, "Mint")
	})

	for _, route := range []string{"/ingredients", "/categories", "/tags"} {
		rec := doGet(t, handler, route)
		assertStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("%s: expected JSON content-type, got %q", route, ct)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: expected X-Content-Type-Options nosniff, got %q", route, got)
		}
	}
}
