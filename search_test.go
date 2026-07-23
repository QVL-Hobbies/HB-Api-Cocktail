package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"hb-api-cocktail/internal/database"
)

func setupSearchServer(t *testing.T) (http.Handler, string) {
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
	mux.HandleFunc("GET /cocktails/search", handleSearchCocktails(db))
	mux.HandleFunc("GET /cocktails/{id}", handleGetCocktail(db))
	return withSecurityHeaders(mux), dbPath
}

func doSearch(t *testing.T, h http.Handler, ingredients string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	q.Set("ingredients", ingredients)
	for k, v := range extra {
		q.Set(k, v)
	}
	return doGet(t, h, "/cocktails/search?"+q.Encode())
}

func listNames(list cocktailListResponse) []string {
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names
}

func assertSameNames(t *testing.T, list cocktailListResponse, want ...string) {
	t.Helper()
	if list.Total != len(want) {
		t.Fatalf("expected total %d, got %d (names=%v)", len(want), list.Total, listNames(list))
	}
	if len(list.Items) != len(want) {
		t.Fatalf("expected %d items, got %d (names=%v)", len(want), len(list.Items), listNames(list))
	}
	for _, name := range want {
		if !containsString(listNames(list), name) {
			t.Fatalf("expected %q in results, got %v", name, listNames(list))
		}
	}
}

func distinctIngredients(n int) string {
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = fmt.Sprintf("zztest%02d", i)
	}
	return strings.Join(names, ",")
}

func TestSearchMatchAllReturnsOnlyCocktailsWithEveryIngredient(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint,Lime juice", map[string]string{"match": "all"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito")
}

func TestSearchMatchAllWithNoCommonCocktailReturnsEmpty(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Tequila,Whisky", map[string]string{"match": "all"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("expected empty result for ingredients shared by no cocktail, got %v", listNames(list))
	}
}

func TestSearchMatchAnyReturnsCocktailsWithAtLeastOne(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint,Whisky", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito", "Hot Toddy")
}

func TestSearchDefaultMatchBehavesLikeAll(t *testing.T) {
	handler, _ := setupSearchServer(t)

	recDefault := doSearch(t, handler, "Mint,Lime juice", nil)
	recAll := doSearch(t, handler, "Mint,Lime juice", map[string]string{"match": "all"})

	assertStatus(t, recDefault, http.StatusOK)
	assertStatus(t, recAll, http.StatusOK)
	assertSameNames(t, decodeList(t, recDefault), listNames(decodeList(t, recAll))...)
}

func TestSearchDefaultMatchDiffersFromAny(t *testing.T) {
	handler, _ := setupSearchServer(t)

	recDefault := doSearch(t, handler, "Mint,Lime juice", nil)
	recAny := doSearch(t, handler, "Mint,Lime juice", map[string]string{"match": "any"})

	assertStatus(t, recDefault, http.StatusOK)
	assertStatus(t, recAny, http.StatusOK)
	assertSameNames(t, decodeList(t, recDefault), "Mojito", "Virgin Mojito")
	assertSameNames(t, decodeList(t, recAny), "Margarita", "Mojito", "Virgin Mojito")
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "MINT", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito")
}

func TestSearchTrimsWhitespaceAroundNames(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, " mint ", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito")
}

func TestSearchDuplicatesDoNotBreakMatchAll(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "mint,mint", map[string]string{"match": "all"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito")
}

func TestSearchUnknownIngredientMatchAllReturnsEmpty(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint,zzzznope", map[string]string{"match": "all"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("expected empty result when a required ingredient is unknown, got %v", listNames(list))
	}
}

func TestSearchUnknownIngredientMatchAnyIgnoresUnknown(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint,zzzznope", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	assertSameNames(t, decodeList(t, rec), "Mojito", "Virgin Mojito")
}

func TestSearchAllUnknownIngredientsReturnsEmptyNotError(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "zzzznope,qqqnope", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("expected empty result for only unknown ingredients, got %v", listNames(list))
	}
}

func TestSearchMissingIngredientsReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doGet(t, handler, "/cocktails/search")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchEmptyIngredientsReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "", nil)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchOnlyCommasAndSpacesReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, " , , ", nil)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchInvalidMatchReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"match": "foo"})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "foo")
}

func TestSearchMoreThan20DistinctNamesReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, distinctIngredients(21), nil)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchExactly20DistinctNamesAccepted(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, distinctIngredients(20), map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Items == nil {
		t.Fatalf("expected items to be [] not null")
	}
}

func TestSearchPaginationTotalIndependentOfLimit(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Lime juice,Whisky", map[string]string{"match": "any", "limit": "2"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 4 {
		t.Fatalf("expected total 4 matching cocktails regardless of limit, got %d", list.Total)
	}
	if list.Limit != 2 {
		t.Fatalf("expected limit 2, got %d", list.Limit)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items with limit 2, got %d", len(list.Items))
	}
}

func TestSearchPaginationOffsetSkipsItems(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Lime juice,Whisky", map[string]string{"match": "any", "limit": "2", "offset": "2"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Total != 4 {
		t.Fatalf("expected total 4, got %d", list.Total)
	}
	if list.Offset != 2 {
		t.Fatalf("expected offset 2, got %d", list.Offset)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items after offset 2, got %d", len(list.Items))
	}
}

func TestSearchItemsAreNeverNull(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "zzzznope", map[string]string{"match": "all"})

	assertStatus(t, rec, http.StatusOK)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw list: %v", err)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf("expected items to be [] for an empty search, got %s", string(raw["items"]))
	}
}

func TestSearchLimitBelowMinReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"limit": "0"})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchLimitAboveMaxReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"limit": "101"})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchNegativeOffsetReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"offset": "-1"})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchOffsetAboveCapReturns400(t *testing.T) {
	handler, dbPath := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"offset": "100001"})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	assertNoLeak(t, rec, dbPath, "")
}

func TestSearchRouteNotCapturedByDetailRoute(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	list := decodeList(t, rec)
	if list.Items == nil {
		t.Fatalf("search route returned a non-list payload, likely captured by /cocktails/{id}")
	}
	assertSameNames(t, list, "Mojito", "Virgin Mojito")
}

func TestSearchItemsAreCompleteCocktails(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Lime juice", map[string]string{"match": "any"})

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
		t.Fatalf("expected Margarita in results for Lime juice, got %v", listNames(list))
	}
	if margarita.Category != "Classic" || margarita.Strength != "Strong" || !margarita.Alcoholic {
		t.Fatalf("Margarita item incomplete: %+v", margarita)
	}
	if len(margarita.Tags) != 3 || len(margarita.Ingredients) != 3 {
		t.Fatalf("expected Margarita to carry 3 tags and 3 ingredients, got %d tags %d ingredients", len(margarita.Tags), len(margarita.Ingredients))
	}
	if margarita.Image != "margarita.jpg" {
		t.Fatalf("expected image name margarita.jpg, got %q", margarita.Image)
	}
}

func TestSearchImageIsNameNotPath(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "White rum", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw list: %v", err)
	}
	if len(raw.Items) != 1 {
		t.Fatalf("expected exactly Mojito for White rum, got %d items", len(raw.Items))
	}
	if _, exists := raw.Items[0]["image_path"]; exists {
		t.Fatalf("search item must not expose internal image_path field")
	}
	list := decodeList(t, rec)
	if list.Items[0].Image != "mojito.jpg" {
		t.Fatalf("expected image name mojito.jpg, got %q", list.Items[0].Image)
	}
	if strings.ContainsAny(list.Items[0].Image, "/\\") {
		t.Fatalf("image field leaks a path: %q", list.Items[0].Image)
	}
}

func TestSecurityHeaderPresentOnSearchResponse(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doSearch(t, handler, "Mint", map[string]string{"match": "any"})

	assertStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on search response, got %q", got)
	}
}

func TestSecurityHeaderPresentOnSearchErrorResponse(t *testing.T) {
	handler, _ := setupSearchServer(t)

	rec := doGet(t, handler, "/cocktails/search")

	assertStatus(t, rec, http.StatusBadRequest)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on search error response, got %q", got)
	}
}
