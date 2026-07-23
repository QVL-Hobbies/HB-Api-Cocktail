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

type cocktailIngredientResponse struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type cocktailResponse struct {
	ID           int64                        `json:"id"`
	Name         string                       `json:"name"`
	Instructions string                       `json:"instructions"`
	Glass        string                       `json:"glass"`
	Category     string                       `json:"category"`
	Strength     string                       `json:"strength"`
	Alcoholic    bool                         `json:"alcoholic"`
	Season       string                       `json:"season"`
	Image        string                       `json:"image"`
	Tags         []string                     `json:"tags"`
	Ingredients  []cocktailIngredientResponse `json:"ingredients"`
}

type cocktailListResponse struct {
	Items  []cocktailResponse `json:"items"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type seedIngredient struct {
	name     string
	quantity string
	unit     string
}

type seedCocktail struct {
	id           int64
	name         string
	instructions string
	glass        string
	category     string
	strength     string
	alcoholic    int
	season       string
	imageName    string
	imagePath    string
	tags         []string
	ingredients  []seedIngredient
}

func testDataset() []seedCocktail {
	return []seedCocktail{
		{
			id: 1, name: "Margarita", instructions: "Shake with ice and strain.",
			glass: "Margarita glass", category: "Classic", strength: "Strong", alcoholic: 1,
			season: "Summer", imageName: "margarita.jpg", imagePath: "/srv/data/images/margarita.jpg",
			tags: []string{"tequila", "sour", "iba"},
			ingredients: []seedIngredient{
				{"Tequila", "50", "ml"},
				{"Triple sec", "20", "ml"},
				{"Lime juice", "15", "ml"},
			},
		},
		{
			id: 2, name: "Mojito", instructions: "Muddle mint and build over ice.",
			glass: "Highball glass", category: "Classic", strength: "Medium", alcoholic: 1,
			season: "Summer", imageName: "mojito.jpg", imagePath: "/srv/data/images/mojito.jpg",
			tags: []string{"rum", "refreshing"},
			ingredients: []seedIngredient{
				{"White rum", "40", "ml"},
				{"Mint", "6", "leaves"},
				{"Lime juice", "20", "ml"},
			},
		},
		{
			id: 3, name: "Virgin Mojito", instructions: "Muddle mint and top with soda.",
			glass: "Highball glass", category: "Mocktail", strength: "Soft", alcoholic: 0,
			season: "Summer", imageName: "", imagePath: "",
			tags: []string{"mocktail", "refreshing"},
			ingredients: []seedIngredient{
				{"Mint", "6", "leaves"},
				{"Lime juice", "20", "ml"},
				{"Soda", "100", "ml"},
			},
		},
		{
			id: 4, name: "Hot Toddy", instructions: "Combine and stir with hot water.",
			glass: "Mug", category: "Warmer", strength: "Strong", alcoholic: 1,
			season: "Winter", imageName: "hot_toddy.png", imagePath: "/srv/data/images/hot_toddy.png",
			tags: []string{"whisky", "warm"},
			ingredients: []seedIngredient{
				{"Whisky", "40", "ml"},
				{"Honey", "15", "ml"},
			},
		},
		{
			id: 5, name: "Shirley Temple", instructions: "Build over ice and add grenadine.",
			glass: "Highball glass", category: "Mocktail", strength: "Soft", alcoholic: 0,
			season: "Winter", imageName: "", imagePath: "",
			tags: []string{"mocktail", "sweet"},
			ingredients: []seedIngredient{
				{"Ginger ale", "120", "ml"},
				{"Grenadine", "10", "ml"},
			},
		},
		{
			id: 6, name: "Plain Water", instructions: "Pour water.",
			glass: "Glass", category: "Other", strength: "None", alcoholic: 0,
			season: "", imageName: "", imagePath: "",
			tags:        []string{},
			ingredients: []seedIngredient{},
		},
	}
}

func seedTestData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, c := range testDataset() {
		_, err := db.Exec(
			`INSERT INTO cocktails (id, name, instructions, glass, category, strength, alcoholic, season, image_name, image_path)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.id, c.name, c.instructions, c.glass, c.category, c.strength, c.alcoholic, c.season, c.imageName, c.imagePath,
		)
		if err != nil {
			t.Fatalf("seed cocktail %q: %v", c.name, err)
		}
		for _, tag := range c.tags {
			if _, err := db.Exec(`INSERT INTO cocktail_tags (cocktail_id, tag) VALUES (?, ?)`, c.id, tag); err != nil {
				t.Fatalf("seed tag %q: %v", tag, err)
			}
		}
		for _, ing := range c.ingredients {
			if _, err := db.Exec(`INSERT OR IGNORE INTO ingredients (name) VALUES (?)`, ing.name); err != nil {
				t.Fatalf("seed ingredient %q: %v", ing.name, err)
			}
			var ingredientID int64
			if err := db.QueryRow(`SELECT id FROM ingredients WHERE name = ?`, ing.name).Scan(&ingredientID); err != nil {
				t.Fatalf("lookup ingredient %q: %v", ing.name, err)
			}
			if _, err := db.Exec(
				`INSERT INTO cocktail_ingredients (cocktail_id, ingredient_id, quantity, unit) VALUES (?, ?, ?, ?)`,
				c.id, ingredientID, ing.quantity, ing.unit,
			); err != nil {
				t.Fatalf("seed cocktail ingredient %q: %v", ing.name, err)
			}
		}
	}
}

func setupTestServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	seedTestData(t, db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /cocktails", handleListCocktails(db))
	mux.HandleFunc("GET /cocktails/{id}", handleGetCocktail(db))
	return withSecurityHeaders(mux), dbPath
}

func doGet(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) cocktailListResponse {
	t.Helper()
	var list cocktailListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode CocktailList: %v (body: %s)", err, rec.Body.String())
	}
	return list
}

func decodeCocktail(t *testing.T, rec *httptest.ResponseRecorder) cocktailResponse {
	t.Helper()
	var c cocktailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode Cocktail: %v (body: %s)", err, rec.Body.String())
	}
	return c
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("expected status %d, got %d (body: %s)", want, rec.Code, rec.Body.String())
	}
}

func assertErrorSchema(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON error content-type, got %q", ct)
	}
	var e errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode Error: %v (body: %s)", err, rec.Body.String())
	}
	if e.Error == "" {
		t.Fatalf("Error schema requires a non-empty 'error' field, got: %s", rec.Body.String())
	}
	return e
}

func assertNoLeak(t *testing.T, rec *httptest.ResponseRecorder, dbPath, echoedInput string) {
	t.Helper()
	body := rec.Body.String()
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"sql", "sqlite", "syntax", "no such column", "no such table", "database"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("error body leaks internal detail %q: %s", forbidden, body)
		}
	}
	if strings.Contains(body, dbPath) {
		t.Fatalf("error body leaks database path: %s", body)
	}
	if echoedInput != "" && strings.Contains(body, echoedInput) {
		t.Fatalf("error body echoes raw input %q: %s", echoedInput, body)
	}
}

func TestListCocktailsWithoutFilterReturnsPaginatedList(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails")

	assertStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
	list := decodeList(t, rec)
	if list.Total != 6 {
		t.Fatalf("expected total 6, got %d", list.Total)
	}
	if len(list.Items) != 6 {
		t.Fatalf("expected 6 items, got %d", len(list.Items))
	}
	if list.Limit != 20 {
		t.Fatalf("expected default limit 20, got %d", list.Limit)
	}
	if list.Offset != 0 {
		t.Fatalf("expected default offset 0, got %d", list.Offset)
	}
}

func TestListCocktailsItemsAreCompleteAndArraysNeverNull(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	for _, item := range list.Items {
		if item.Tags == nil {
			t.Fatalf("item %q exposes tags as null instead of []", item.Name)
		}
		if item.Ingredients == nil {
			t.Fatalf("item %q exposes ingredients as null instead of []", item.Name)
		}
	}

	var margarita *cocktailResponse
	for i := range list.Items {
		if list.Items[i].ID == 1 {
			margarita = &list.Items[i]
		}
	}
	if margarita == nil {
		t.Fatalf("expected Margarita in the list")
	}
	if len(margarita.Tags) != 3 {
		t.Fatalf("expected Margarita list item to carry 3 tags, got %d", len(margarita.Tags))
	}
	if len(margarita.Ingredients) != 3 {
		t.Fatalf("expected Margarita list item to carry 3 ingredients, got %d", len(margarita.Ingredients))
	}
}

func TestListCocktailsFilterByCategory(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?category=Classic")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("expected 2 Classic cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.Category != "Classic" {
			t.Fatalf("category filter returned %q", item.Category)
		}
	}
}

func TestListCocktailsFilterByStrength(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?strength=Soft")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("expected 2 Soft cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.Strength != "Soft" {
			t.Fatalf("strength filter returned %q", item.Strength)
		}
	}
}

func TestListCocktailsFilterByAlcoholicTrueReturnsOnlyAlcoholic(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?alcoholic=true")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("expected 3 alcoholic cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if !item.Alcoholic {
			t.Fatalf("alcoholic=true returned a non-alcoholic cocktail %q", item.Name)
		}
	}
}

func TestListCocktailsFilterByAlcoholicFalseReturnsOnlyMocktails(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?alcoholic=false")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("expected 3 non-alcoholic cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.Alcoholic {
			t.Fatalf("alcoholic=false returned an alcoholic cocktail %q", item.Name)
		}
	}
}

func TestListCocktailsFilterBySeason(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?season=Winter")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("expected 2 Winter cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.Season != "Winter" {
			t.Fatalf("season filter returned %q", item.Season)
		}
	}
}

func TestListCocktailsFilterByTag(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?tag=refreshing")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("expected 2 refreshing cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if !containsString(item.Tags, "refreshing") {
			t.Fatalf("tag filter returned %q without the refreshing tag", item.Name)
		}
	}
}

func TestListCocktailsCombinedFilters(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?category=Classic&alcoholic=true")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("expected 2 Classic alcoholic cocktails, got total=%d items=%d", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.Category != "Classic" || !item.Alcoholic {
			t.Fatalf("combined filter returned %q (category=%q alcoholic=%v)", item.Name, item.Category, item.Alcoholic)
		}
	}
}

func TestListCocktailsCombinedFiltersSeasonAndAlcoholic(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?season=Summer&alcoholic=false")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected 1 Summer non-alcoholic cocktail, got total=%d items=%d", list.Total, len(list.Items))
	}
	if list.Items[0].Name != "Virgin Mojito" {
		t.Fatalf("expected Virgin Mojito, got %q", list.Items[0].Name)
	}
}

func TestListCocktailsCombinedFiltersTagAndSeason(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?tag=mocktail&season=Winter")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected 1 winter mocktail, got total=%d items=%d", list.Total, len(list.Items))
	}
	if list.Items[0].Name != "Shirley Temple" {
		t.Fatalf("expected Shirley Temple, got %q", list.Items[0].Name)
	}
}

func TestListCocktailsUnknownCategoryValueReturnsEmptyList(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?category=Inexistante")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 0 {
		t.Fatalf("expected total 0 for unknown category, got %d", list.Total)
	}
	if list.Items == nil {
		t.Fatalf("expected items to be [] not null")
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected empty items for unknown category, got %d", len(list.Items))
	}
}

func TestListCocktailsUnknownTagValueReturnsEmptyList(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?tag=zzz")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("expected empty list for unknown tag, got total=%d items=%d", list.Total, len(list.Items))
	}
}

func TestListCocktailsLimitBoundaryOne(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=1")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Limit != 1 {
		t.Fatalf("expected limit 1, got %d", list.Limit)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list.Items))
	}
	if list.Total != 6 {
		t.Fatalf("expected total 6 regardless of limit, got %d", list.Total)
	}
}

func TestListCocktailsLimitBoundaryHundred(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=100")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Limit != 100 {
		t.Fatalf("expected limit 100, got %d", list.Limit)
	}
	if len(list.Items) != 6 {
		t.Fatalf("expected all 6 items, got %d", len(list.Items))
	}
}

func TestListCocktailsOffsetSkipsItems(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=100&offset=2")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Offset != 2 {
		t.Fatalf("expected offset 2, got %d", list.Offset)
	}
	if list.Total != 6 {
		t.Fatalf("expected total 6, got %d", list.Total)
	}
	if len(list.Items) != 4 {
		t.Fatalf("expected 4 items after offset 2, got %d", len(list.Items))
	}
}

func TestListCocktailsMalformedAlcoholicWordReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?alcoholic=oui")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "oui")
}

func TestListCocktailsMalformedAlcoholicNumericReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?alcoholic=1")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestListCocktailsMalformedLimitNotIntegerReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=abc")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "abc")
}

func TestListCocktailsLimitBelowMinReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=0")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestListCocktailsLimitAboveMaxReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=101")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestListCocktailsOffsetAtCapIsAccepted(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?offset=100000")

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Offset != 100000 {
		t.Fatalf("expected offset 100000 to be accepted, got %d", list.Offset)
	}
	if list.Total != 6 {
		t.Fatalf("expected total 6, got %d", list.Total)
	}
}

func TestListCocktailsOffsetAboveCapReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?offset=100001")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSecurityHeaderPresentOnCocktailsResponse(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails")

	assertStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on list response, got %q", got)
	}
}

func TestSecurityHeaderPresentOnErrorResponse(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?limit=abc")

	assertStatus(t, rec, http.StatusBadRequest)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on error response, got %q", got)
	}
}

func TestListCocktailsNegativeOffsetReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?offset=-1")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestListCocktailsOffsetNotIntegerReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails?offset=abc")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "abc")
}

func TestGetCocktailByIdReturnsFullDetail(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/1")

	assertStatus(t, rec, http.StatusOK)
	c := decodeCocktail(t, rec)
	if c.ID != 1 {
		t.Fatalf("expected id 1, got %d", c.ID)
	}
	if c.Name != "Margarita" {
		t.Fatalf("expected Margarita, got %q", c.Name)
	}
	if c.Category != "Classic" || c.Strength != "Strong" || c.Season != "Summer" {
		t.Fatalf("unexpected referential fields: category=%q strength=%q season=%q", c.Category, c.Strength, c.Season)
	}
	if !c.Alcoholic {
		t.Fatalf("expected Margarita to be alcoholic")
	}
	if c.Instructions == "" || c.Glass == "" {
		t.Fatalf("expected instructions and glass to be populated")
	}
	if len(c.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(c.Tags))
	}
	if len(c.Ingredients) != 3 {
		t.Fatalf("expected 3 ingredients, got %d", len(c.Ingredients))
	}
	var tequila *cocktailIngredientResponse
	for i := range c.Ingredients {
		if c.Ingredients[i].Name == "Tequila" {
			tequila = &c.Ingredients[i]
		}
	}
	if tequila == nil {
		t.Fatalf("expected Tequila ingredient")
	}
	if tequila.Quantity != "50" || tequila.Unit != "ml" {
		t.Fatalf("expected Tequila 50 ml, got %q %q", tequila.Quantity, tequila.Unit)
	}
}

func TestGetCocktailByIdImageIsNameNotPath(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/1")

	assertStatus(t, rec, http.StatusOK)
	c := decodeCocktail(t, rec)
	if c.Image != "margarita.jpg" {
		t.Fatalf("expected image name margarita.jpg, got %q", c.Image)
	}
	if strings.ContainsAny(c.Image, "/\\") {
		t.Fatalf("image field leaks a path: %q", c.Image)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw cocktail: %v", err)
	}
	if _, exists := raw["image_path"]; exists {
		t.Fatalf("response must not expose internal image_path field")
	}
}

func TestGetCocktailByIdEmptyArraysNeverNull(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/6")

	assertStatus(t, rec, http.StatusOK)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw cocktail: %v", err)
	}
	if string(raw["tags"]) != "[]" {
		t.Fatalf("expected tags to be [] for a cocktail without tags, got %s", string(raw["tags"]))
	}
	if string(raw["ingredients"]) != "[]" {
		t.Fatalf("expected ingredients to be [] for a cocktail without ingredients, got %s", string(raw["ingredients"]))
	}
}

func TestGetCocktailAlcoholicIsJSONBoolean(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/1")

	assertStatus(t, rec, http.StatusOK)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw cocktail: %v", err)
	}
	value := string(raw["alcoholic"])
	if value != "true" && value != "false" {
		t.Fatalf("alcoholic must be a JSON boolean, got %s", value)
	}
}

func TestGetCocktailMalformedIdNonNumericReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/abc")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "abc")
}

func TestGetCocktailMalformedIdZeroReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/0")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestGetCocktailMalformedIdNegativeReturns400(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/-1")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestGetCocktailNonexistentIdReturns404(t *testing.T) {
	handler, dbPath := setupTestServer(t)

	rec := doGet(t, handler, "/cocktails/999999")

	assertStatus(t, rec, http.StatusNotFound)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
