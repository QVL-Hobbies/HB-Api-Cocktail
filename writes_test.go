package main

import (
	"bytes"
	"database/sql"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hb-api-cocktail/internal/database"
)

const testWriteToken = "s3cr3t-local-write-token-value"

const loopbackAddr = "127.0.0.1:12345"

const publicAddr = "203.0.113.7:12345"

const validCocktailJSON = `{"name":"Test Sour","instructions":"Shake well and strain.","glass":"Coupe","category":"Signature","strength":"Medium","alcoholic":true,"season":"Autumn","tags":["citrus","brand-new-tag"],"ingredients":[{"name":"Gin","quantity":"50","unit":"ml"},{"name":"Lemon juice","quantity":"20","unit":"ml"}]}`

type createRequest struct {
	remoteAddr string
	authHeader string
	cocktail   *string
	imageName  string
	image      []byte
}

func (cr createRequest) build(t *testing.T) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if cr.cocktail != nil {
		field, err := writer.CreateFormField("cocktail")
		if err != nil {
			t.Fatalf("create cocktail part: %v", err)
		}
		if _, err := field.Write([]byte(*cr.cocktail)); err != nil {
			t.Fatalf("write cocktail part: %v", err)
		}
	}
	if cr.image != nil {
		name := cr.imageName
		if name == "" {
			name = "upload.bin"
		}
		file, err := writer.CreateFormFile("image", name)
		if err != nil {
			t.Fatalf("create image part: %v", err)
		}
		if _, err := file.Write(cr.image); err != nil {
			t.Fatalf("write image part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/cocktails", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cr.authHeader != "" {
		req.Header.Set("Authorization", cr.authHeader)
	}
	req.RemoteAddr = cr.remoteAddr
	return req
}

func nominalCreateRequest() createRequest {
	body := validCocktailJSON
	return createRequest{
		remoteAddr: loopbackAddr,
		authHeader: "Bearer " + testWriteToken,
		cocktail:   &body,
		imageName:  "client-provided.png",
		image:      nil,
	}
}

func setupWriteServer(t *testing.T, token string) (http.Handler, *sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	imagesDir := t.TempDir()
	config := Config{DBPath: dbPath, ImagesDir: imagesDir, LocalWriteToken: token}

	mux := http.NewServeMux()
	if config.LocalWriteToken != "" {
		mux.HandleFunc("POST /cocktails", requireLocalWrite(config, handleCreateCocktail(db, config)))
	}
	mux.HandleFunc("GET /cocktails", handleListCocktails(db))
	mux.HandleFunc("GET /cocktails/{id}", handleGetCocktail(db))
	mux.HandleFunc("GET /categories", handleListCategories(db))
	mux.HandleFunc("GET /tags", handleListTags(db))
	return withSecurityHeaders(mux), db, imagesDir
}

func doCreate(t *testing.T, h http.Handler, cr createRequest) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, cr.build(t))
	return rec
}

func countCocktails(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cocktails`).Scan(&n); err != nil {
		t.Fatalf("count cocktails: %v", err)
	}
	return n
}

func imageFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read images dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func validPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func validJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func validWEBP() []byte {
	header := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	return append(header, bytes.Repeat([]byte{0x10}, 32)...)
}

func realGIF() []byte {
	return append([]byte("GIF89a"), bytes.Repeat([]byte{0x00}, 32)...)
}

func TestCreateNonLoopbackWithValidTokenReturns403AndDoesNotCreate(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.remoteAddr = publicAddr
	cr.image = validPNG(t)

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusForbidden)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("non-loopback request must not create a cocktail, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("non-loopback request must not write a file, got %v", files)
	}
}

func TestCreateLoopbackMissingTokenReturns401(t *testing.T) {
	handler, db, _ := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.authHeader = ""

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("missing token must not create a cocktail, count=%d", n)
	}
}

func TestCreateLoopbackInvalidTokenReturns401(t *testing.T) {
	handler, db, _ := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.authHeader = "Bearer wrong-token"

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("invalid token must not create a cocktail, count=%d", n)
	}
}

func TestCreatePostWhenTokenDisabledReturns405(t *testing.T) {
	handler, db, _ := setupWriteServer(t, "")
	cr := nominalCreateRequest()
	cr.authHeader = ""

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("disabled route must not create a cocktail, count=%d", n)
	}
}

func TestCreateWithValidPngReturns201AndPersistsAndStoresFile(t *testing.T) {
	handler, _, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.image = validPNG(t)

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	created := decodeCocktail(t, rec)
	if created.ID <= 0 {
		t.Fatalf("expected a positive generated id, got %d", created.ID)
	}
	if created.Name != "Test Sour" {
		t.Fatalf("expected name Test Sour, got %q", created.Name)
	}
	if created.Image == "" {
		t.Fatalf("expected generated image name in response, got empty")
	}
	if strings.ContainsAny(created.Image, "/\\") {
		t.Fatalf("image field must be a name, not a path: %q", created.Image)
	}
	stored := filepath.Join(imagesDir, created.Image)
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("expected stored image file %q to exist: %v", stored, err)
	}

	getRec := doGet(t, handler, "/cocktails/"+itoa(created.ID))
	assertStatus(t, getRec, http.StatusOK)
	fetched := decodeCocktail(t, getRec)
	if fetched.ID != created.ID || fetched.Name != "Test Sour" {
		t.Fatalf("re-read cocktail mismatch: %+v", fetched)
	}

	catRec := doGet(t, handler, "/categories")
	if !containsString(decodeStringArray(t, catRec), "Signature") {
		t.Fatalf("new category Signature not exposed by /categories")
	}
	tagRec := doGet(t, handler, "/tags")
	if !containsString(decodeStringArray(t, tagRec), "brand-new-tag") {
		t.Fatalf("new tag brand-new-tag not exposed by /tags")
	}
}

func TestCreateWithValidJpegReturns201(t *testing.T) {
	handler, _, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.imageName = "photo.jpg"
	cr.image = validJPEG(t)

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	created := decodeCocktail(t, rec)
	if _, err := os.Stat(filepath.Join(imagesDir, created.Image)); err != nil {
		t.Fatalf("expected stored jpeg file: %v", err)
	}
}

func TestCreateWithValidWebpReturns201(t *testing.T) {
	handler, _, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.imageName = "photo.webp"
	cr.image = validWEBP()

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	created := decodeCocktail(t, rec)
	if _, err := os.Stat(filepath.Join(imagesDir, created.Image)); err != nil {
		t.Fatalf("expected stored webp file: %v", err)
	}
}

func TestCreateWithoutImageReturns201AndNoFileWritten(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	created := decodeCocktail(t, rec)
	if created.Image != "" {
		t.Fatalf("expected empty image when none uploaded, got %q", created.Image)
	}
	if n := countCocktails(t, db); n != 1 {
		t.Fatalf("expected exactly 1 cocktail created, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("no image should be written when none uploaded, got %v", files)
	}
}

func TestCreateServerGeneratesImageNameIgnoringClientFilename(t *testing.T) {
	handler, _, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.imageName = "../../pwned.png"
	cr.image = validPNG(t)

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	created := decodeCocktail(t, rec)
	if strings.Contains(created.Image, "pwned") {
		t.Fatalf("server must not reuse client filename, got %q", created.Image)
	}
	if strings.ContainsAny(created.Image, "/\\") {
		t.Fatalf("generated name must contain no path separators, got %q", created.Image)
	}
	files := imageFiles(t, imagesDir)
	if len(files) != 1 || files[0] != created.Image {
		t.Fatalf("exactly one file named as generated should exist in images dir, got %v (image=%q)", files, created.Image)
	}
}

func TestCreateMissingCocktailPartReturns400(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	cr := createRequest{
		remoteAddr: loopbackAddr,
		authHeader: "Bearer " + testWriteToken,
		imageName:  "photo.png",
		image:      validPNG(t),
	}

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("missing cocktail part must not create a row, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("missing cocktail part must not write a file, got %v", files)
	}
}

func TestCreateMissingRequiredFieldReturns400AndNoRowCreated(t *testing.T) {
	cases := map[string]string{
		"empty name":          `{"name":"","instructions":"i","glass":"g","category":"c","strength":"s","alcoholic":true}`,
		"absent instructions": `{"name":"n","glass":"g","category":"c","strength":"s","alcoholic":true}`,
		"absent glass":        `{"name":"n","instructions":"i","category":"c","strength":"s","alcoholic":true}`,
	}
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			handler, db, imagesDir := setupWriteServer(t, testWriteToken)
			body := payload
			cr := createRequest{
				remoteAddr: loopbackAddr,
				authHeader: "Bearer " + testWriteToken,
				cocktail:   &body,
			}

			rec := doCreate(t, handler, cr)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorSchema(t, rec)
			if n := countCocktails(t, db); n != 0 {
				t.Fatalf("invalid payload must not create a row, count=%d", n)
			}
			if files := imageFiles(t, imagesDir); len(files) != 0 {
				t.Fatalf("invalid payload must not write a file, got %v", files)
			}
		})
	}
}

func TestCreateInvalidJsonReturns400(t *testing.T) {
	handler, db, _ := setupWriteServer(t, testWriteToken)
	body := `{"name":"broken",`
	cr := createRequest{
		remoteAddr: loopbackAddr,
		authHeader: "Bearer " + testWriteToken,
		cocktail:   &body,
	}

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("malformed JSON must not create a row, count=%d", n)
	}
}

func TestCreateNonImageFileReturns400AndNoFileWritten(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.imageName = "note.txt"
	cr.image = []byte("this is plain text, definitely not an image at all")

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("non-image upload must not create a row, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("non-image upload must not write a file, got %v", files)
	}
}

func TestCreateGifImageReturns400(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.imageName = "anim.gif"
	cr.image = realGIF()

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorSchema(t, rec)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("gif upload must not create a row, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("gif upload must not write a file, got %v", files)
	}
}

func TestCreateImageOver2MBReturns413AndNoCreation(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	oversized := append(validPNG(t), bytes.Repeat([]byte{0x00}, 2*1024*1024+1024)...)
	cr := nominalCreateRequest()
	cr.imageName = "huge.png"
	cr.image = oversized

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusRequestEntityTooLarge)
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("oversized image must not create a row, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("oversized image must not write a file, got %v", files)
	}
}

func TestCreateSeriesOfInvalidRequestsLeavesDatabaseAndDiskClean(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)

	requests := []createRequest{
		func() createRequest {
			cr := nominalCreateRequest()
			cr.remoteAddr = publicAddr
			cr.image = validPNG(t)
			return cr
		}(),
		func() createRequest { cr := nominalCreateRequest(); cr.authHeader = "Bearer nope"; return cr }(),
		func() createRequest {
			cr := nominalCreateRequest()
			cr.imageName = "note.txt"
			cr.image = []byte("not an image")
			return cr
		}(),
		func() createRequest {
			cr := nominalCreateRequest()
			cr.image = append(validPNG(t), bytes.Repeat([]byte{0x00}, 2*1024*1024+1024)...)
			return cr
		}(),
	}
	for _, cr := range requests {
		doCreate(t, handler, cr)
	}

	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("database must stay empty after a series of invalid requests, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("no orphan file must remain after invalid requests, got %v", files)
	}
}

func TestCreateTwoCocktailsProduceTwoDistinctFiles(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)

	first := nominalCreateRequest()
	first.image = validPNG(t)
	rec1 := doCreate(t, handler, first)
	assertStatus(t, rec1, http.StatusCreated)
	c1 := decodeCocktail(t, rec1)

	second := nominalCreateRequest()
	second.image = validPNG(t)
	rec2 := doCreate(t, handler, second)
	assertStatus(t, rec2, http.StatusCreated)
	c2 := decodeCocktail(t, rec2)

	if c1.Image == "" || c2.Image == "" {
		t.Fatalf("both creations must yield a stored image name, got %q and %q", c1.Image, c2.Image)
	}
	if c1.Image == c2.Image {
		t.Fatalf("two creations must not share the same file name (overwrite risk): %q", c1.Image)
	}
	if n := countCocktails(t, db); n != 2 {
		t.Fatalf("expected 2 cocktails created, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 2 {
		t.Fatalf("expected 2 distinct files on disk, got %v", files)
	}
}

func TestCreateResponseHasNosniffHeader(t *testing.T) {
	handler, _, _ := setupWriteServer(t, testWriteToken)
	cr := nominalCreateRequest()
	cr.image = validPNG(t)

	rec := doCreate(t, handler, cr)

	assertStatus(t, rec, http.StatusCreated)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff header on create response, got %q", got)
	}
}

func TestCreateResponsesNeverContainToken(t *testing.T) {
	handler, _, _ := setupWriteServer(t, testWriteToken)

	cases := []createRequest{
		func() createRequest { cr := nominalCreateRequest(); cr.image = validPNG(t); return cr }(),
		func() createRequest { cr := nominalCreateRequest(); cr.authHeader = "Bearer wrong-token"; return cr }(),
		func() createRequest { cr := nominalCreateRequest(); cr.authHeader = ""; return cr }(),
		func() createRequest { cr := nominalCreateRequest(); cr.remoteAddr = publicAddr; return cr }(),
	}
	for _, cr := range cases {
		rec := doCreate(t, handler, cr)
		if strings.Contains(rec.Body.String(), testWriteToken) {
			t.Fatalf("response body leaked the local write token: %s", rec.Body.String())
		}
	}
}

func boundsCocktail(name, instructions, ingredients, tags string) string {
	return `{"name":"` + name + `","instructions":"` + instructions +
		`","glass":"Coupe","category":"Signature","strength":"Medium","alcoholic":true,"season":"Autumn","tags":` +
		tags + `,"ingredients":` + ingredients + `}`
}

func ingredientArrayJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `{"name":"Ing` + itoa(int64(i)) + `","quantity":"1","unit":"ml"}`
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func tagArrayJSON(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + v + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

func repeatedTagNames(n int) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = "tag" + itoa(int64(i))
	}
	return values
}

func loopbackCreateRequest(body string) createRequest {
	payload := body
	return createRequest{
		remoteAddr: loopbackAddr,
		authHeader: "Bearer " + testWriteToken,
		cocktail:   &payload,
	}
}

func assertNoWrite(t *testing.T, db *sql.DB, imagesDir string) {
	t.Helper()
	if n := countCocktails(t, db); n != 0 {
		t.Fatalf("rejected request must not create a row, count=%d", n)
	}
	if files := imageFiles(t, imagesDir); len(files) != 0 {
		t.Fatalf("rejected request must not write a file, got %v", files)
	}
}

func TestCreateIngredientsCountBound(t *testing.T) {
	validTags := tagArrayJSON([]string{"citrus"})

	t.Run("51 ingredients returns 400 and writes nothing", func(t *testing.T) {
		handler, db, imagesDir := setupWriteServer(t, testWriteToken)
		body := boundsCocktail("Bounds Sour", "Shake well.", ingredientArrayJSON(51), validTags)

		rec := doCreate(t, handler, loopbackCreateRequest(body))

		assertStatus(t, rec, http.StatusBadRequest)
		assertErrorSchema(t, rec)
		assertNoWrite(t, db, imagesDir)
	})

	t.Run("exactly 50 ingredients is accepted", func(t *testing.T) {
		handler, _, _ := setupWriteServer(t, testWriteToken)
		body := boundsCocktail("Bounds Sour", "Shake well.", ingredientArrayJSON(50), validTags)

		rec := doCreate(t, handler, loopbackCreateRequest(body))

		assertStatus(t, rec, http.StatusCreated)
	})
}

func TestCreateTagsCountBound(t *testing.T) {
	validIngredients := ingredientArrayJSON(2)

	t.Run("31 tags returns 400 and writes nothing", func(t *testing.T) {
		handler, db, imagesDir := setupWriteServer(t, testWriteToken)
		body := boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON(repeatedTagNames(31)))

		rec := doCreate(t, handler, loopbackCreateRequest(body))

		assertStatus(t, rec, http.StatusBadRequest)
		assertErrorSchema(t, rec)
		assertNoWrite(t, db, imagesDir)
	})

	t.Run("exactly 30 tags is accepted", func(t *testing.T) {
		handler, _, _ := setupWriteServer(t, testWriteToken)
		body := boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON(repeatedTagNames(30)))

		rec := doCreate(t, handler, loopbackCreateRequest(body))

		assertStatus(t, rec, http.StatusCreated)
	})
}

func TestCreateFieldTooLongReturns400(t *testing.T) {
	validIngredients := ingredientArrayJSON(2)
	validTags := tagArrayJSON([]string{"citrus"})
	longIngredient := `[{"name":"` + strings.Repeat("a", 101) + `","quantity":"1","unit":"ml"}]`

	cases := map[string]string{
		"name of 201 runes":        boundsCocktail(strings.Repeat("a", 201), "Shake well.", validIngredients, validTags),
		"instructions over 5000":   boundsCocktail("Bounds Sour", strings.Repeat("a", 5001), validIngredients, validTags),
		"ingredient name over 100": boundsCocktail("Bounds Sour", "Shake well.", longIngredient, validTags),
	}
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			handler, db, imagesDir := setupWriteServer(t, testWriteToken)

			rec := doCreate(t, handler, loopbackCreateRequest(payload))

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorSchema(t, rec)
			assertNoWrite(t, db, imagesDir)
		})
	}
}

func TestCreateNameAtMaxLengthIsAccepted(t *testing.T) {
	handler, _, _ := setupWriteServer(t, testWriteToken)
	body := boundsCocktail(strings.Repeat("a", 200), "Shake well.", ingredientArrayJSON(2), tagArrayJSON([]string{"citrus"}))

	rec := doCreate(t, handler, loopbackCreateRequest(body))

	assertStatus(t, rec, http.StatusCreated)
}

func TestCreateResidualDataAfterCocktailObjectReturns400(t *testing.T) {
	cases := map[string]string{
		"trailing empty object":  validCocktailJSON + "{}",
		"trailing garbage text":  validCocktailJSON + "garbage",
		"trailing second object": validCocktailJSON + `{"name":"Second"}`,
	}
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			handler, db, imagesDir := setupWriteServer(t, testWriteToken)

			rec := doCreate(t, handler, loopbackCreateRequest(payload))

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorSchema(t, rec)
			assertNoWrite(t, db, imagesDir)
		})
	}
}

func TestCreateEmptyOrBlankTagReturns400(t *testing.T) {
	validIngredients := ingredientArrayJSON(2)

	cases := map[string]string{
		"empty tag":             boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON([]string{""})),
		"blank tag":             boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON([]string{"   "})),
		"blank tag among valid": boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON([]string{"citrus", "   "})),
	}
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			handler, db, imagesDir := setupWriteServer(t, testWriteToken)

			rec := doCreate(t, handler, loopbackCreateRequest(payload))

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorSchema(t, rec)
			assertNoWrite(t, db, imagesDir)
		})
	}
}

func TestCreateSeriesOfBoundViolationsLeavesDatabaseAndDiskClean(t *testing.T) {
	handler, db, imagesDir := setupWriteServer(t, testWriteToken)
	validIngredients := ingredientArrayJSON(2)
	validTags := tagArrayJSON([]string{"citrus"})

	payloads := []string{
		boundsCocktail("Bounds Sour", "Shake well.", ingredientArrayJSON(51), validTags),
		boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON(repeatedTagNames(31))),
		boundsCocktail(strings.Repeat("a", 201), "Shake well.", validIngredients, validTags),
		boundsCocktail("Bounds Sour", strings.Repeat("a", 5001), validIngredients, validTags),
		validCocktailJSON + "{}",
		boundsCocktail("Bounds Sour", "Shake well.", validIngredients, tagArrayJSON([]string{"   "})),
	}
	for _, payload := range payloads {
		cr := loopbackCreateRequest(payload)
		cr.imageName = "photo.png"
		cr.image = validPNG(t)
		rec := doCreate(t, handler, cr)
		assertStatus(t, rec, http.StatusBadRequest)
	}

	assertNoWrite(t, db, imagesDir)
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
