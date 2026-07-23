package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secretMarker = "TOP_SECRET_CONTENT_OUTSIDE_IMAGES_DIR"

func writeImageFile(t *testing.T, dir, name string, content []byte) []byte {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("write image %q: %v", name, err)
	}
	return content
}

func setupImageServer(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	parent := t.TempDir()
	imagesDir := filepath.Join(parent, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatalf("create images dir: %v", err)
	}

	writeImageFile(t, imagesDir, "mojito.jpg", []byte("\xFF\xD8\xFF\xE0JFIF-mojito-body-bytes"))
	writeImageFile(t, imagesDir, "hot_toddy.png", []byte("\x89PNG\r\n\x1a\n-hot-toddy-bytes"))
	writeImageFile(t, imagesDir, "sample.webp", []byte("RIFF----WEBPVP8 -sample-bytes"))
	writeImageFile(t, imagesDir, "classic.jpeg", []byte("\xFF\xD8\xFF-classic-jpeg-bytes"))
	writeImageFile(t, imagesDir, "photo.PNG", []byte("\x89PNG-uppercase-ext-bytes"))
	writeImageFile(t, imagesDir, "note.txt", []byte("plain text, not an image"))
	writeImageFile(t, imagesDir, "evil.svg", []byte("<svg></svg>"))
	writeImageFile(t, imagesDir, "x.gif", []byte("GIF89a-not-allowed"))

	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte(secretMarker), 0o644); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /images/{name}", handleGetImage(imagesDir))
	return withSecurityHeaders(mux), imagesDir, parent
}

func setupImageServerMissingDir(t *testing.T) http.Handler {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /images/{name}", handleGetImage(missing))
	return withSecurityHeaders(mux)
}

func assertContentType(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, want) {
		t.Fatalf("expected Content-Type containing %q, got %q", want, ct)
	}
}

func assertNotFoundCode(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	e := assertErrorSchema(t, rec)
	if e.Error != "not_found" {
		t.Fatalf("expected error code \"not_found\", got %q (body: %s)", e.Error, rec.Body.String())
	}
}

func assertNoSecretLeak(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(rec.Body.String(), secretMarker) {
		t.Fatalf("CRITICAL: response leaked content of a file outside the images dir (body: %q)", rec.Body.String())
	}
}

func assertNoPathLeak(t *testing.T, rec *httptest.ResponseRecorder, imagesDir, parent string) {
	t.Helper()
	body := rec.Body.String()
	for _, forbidden := range []string{imagesDir, parent, filepath.ToSlash(imagesDir), filepath.ToSlash(parent)} {
		if forbidden != "" && strings.Contains(body, forbidden) {
			t.Fatalf("404 body leaks a filesystem path %q: %s", forbidden, body)
		}
	}
}

func doGetRawNullByte(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	u := &url.URL{Path: "/images/mojito.jpg\x00.txt"}
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       "example.com",
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetImageJpgReturnsFileWithJpegContentType(t *testing.T) {
	handler, imagesDir, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/mojito.jpg")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/jpeg")
	want, err := os.ReadFile(filepath.Join(imagesDir, "mojito.jpg"))
	if err != nil {
		t.Fatalf("read reference file: %v", err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("served body does not match file bytes")
	}
}

func TestGetImagePngReturnsPngContentType(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/hot_toddy.png")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/png")
}

func TestGetImageWebpReturnsWebpContentType(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/sample.webp")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/webp")
}

func TestGetImageJpegReturnsJpegContentType(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/classic.jpeg")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/jpeg")
}

func TestGetImageUppercaseExtensionIsCaseInsensitive(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/photo.PNG")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/png")
}

func TestGetImageUnknownNameReturns404(t *testing.T) {
	handler, imagesDir, parent := setupImageServer(t)

	rec := doGet(t, handler, "/images/unknown.jpg")

	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
	assertNoPathLeak(t, rec, imagesDir, parent)
}

func TestGetImageForbiddenExtensionTxtReturns404(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/note.txt")

	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
}

func TestGetImageForbiddenExtensionSvgReturns404(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/evil.svg")

	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
}

func TestGetImageForbiddenExtensionGifReturns404(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/x.gif")

	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
}

func TestGetImageRejectsPathTraversalAndNeverLeaksOutsideContent(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	payloads := []string{
		"/images/../secret.txt",
		"/images/../../secret.txt",
		"/images/..%2fsecret.txt",
		"/images/..%2f..%2fsecret.txt",
		"/images/%2e%2e%2fsecret.txt",
		"/images/%252e%252e%252fsecret.txt",
		"/images/..%5c..%5csecret.txt",
		"/images/..\\secret.txt",
		"/images/%2fetc%2fpasswd",
		"/images/C:%5cWindows%5cwin.ini",
	}

	for _, target := range payloads {
		t.Run(target, func(t *testing.T) {
			rec := doGet(t, handler, target)
			t.Logf("payload %q -> status %d, content-type %q", target, rec.Code, rec.Header().Get("Content-Type"))
			assertNoSecretLeak(t, rec)
			if rec.Code == http.StatusOK {
				t.Fatalf("CRITICAL: traversal payload %q returned 200", target)
			}
		})
	}
}

func TestGetImageNullByteInNameReturns404(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGetRawNullByte(t, handler)

	t.Logf("null-byte payload -> status %d", rec.Code)
	assertNoSecretLeak(t, rec)
	if rec.Code == http.StatusOK {
		t.Fatalf("CRITICAL: null-byte payload returned 200")
	}
}

func TestGetImageMissingImagesDirReturns404(t *testing.T) {
	handler := setupImageServerMissingDir(t)

	rec := doGet(t, handler, "/images/mojito.jpg")

	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
}

func TestGetImage404DoesNotLeakFilesystemPath(t *testing.T) {
	handler, imagesDir, parent := setupImageServer(t)

	rec := doGet(t, handler, "/images/note.txt")

	assertStatus(t, rec, http.StatusNotFound)
	assertNoPathLeak(t, rec, imagesDir, parent)
}

func TestSecurityHeaderPresentOnImageResponse(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/mojito.jpg")

	assertStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on image response, got %q", got)
	}
}

func TestSecurityHeaderPresentOnImage404(t *testing.T) {
	handler, _, _ := setupImageServer(t)

	rec := doGet(t, handler, "/images/unknown.jpg")

	assertStatus(t, rec, http.StatusNotFound)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on image 404, got %q", got)
	}
}

func createSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink (insufficient privileges on this environment): %v", err)
	}
}

func TestGetImageOutboundSymlinkReturns404AndNeverLeaksTargetContent(t *testing.T) {
	handler, imagesDir, parent := setupImageServer(t)
	createSymlinkOrSkip(t, filepath.Join(parent, "secret.txt"), filepath.Join(imagesDir, "link_out.jpg"))

	rec := doGet(t, handler, "/images/link_out.jpg")

	assertNoSecretLeak(t, rec)
	if rec.Code == http.StatusOK {
		t.Fatalf("CRITICAL: outbound symlink /images/link_out.jpg was served (status 200)")
	}
	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
	assertNoPathLeak(t, rec, imagesDir, parent)
}

func TestGetImageInternalSymlinkToLegitImageReturns200(t *testing.T) {
	handler, imagesDir, _ := setupImageServer(t)
	createSymlinkOrSkip(t, filepath.Join(imagesDir, "mojito.jpg"), filepath.Join(imagesDir, "link_in.jpg"))

	rec := doGet(t, handler, "/images/link_in.jpg")

	assertStatus(t, rec, http.StatusOK)
	assertContentType(t, rec, "image/jpeg")
	want, err := os.ReadFile(filepath.Join(imagesDir, "mojito.jpg"))
	if err != nil {
		t.Fatalf("read reference file: %v", err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("served body does not match target image bytes")
	}
}

func TestGetImageNameLongerThan255Returns404WithoutLeak(t *testing.T) {
	handler, imagesDir, parent := setupImageServer(t)
	name := strings.Repeat("a", 300) + ".jpg"

	rec := doGet(t, handler, "/images/"+name)

	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("name > 255 chars produced 500 instead of a clean 404 (body: %s)", rec.Body.String())
	}
	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
	assertNoPathLeak(t, rec, imagesDir, parent)
}

func TestGetImageNameAtLengthLimitReturnsCleanNotFoundNot500(t *testing.T) {
	handler, imagesDir, parent := setupImageServer(t)
	name := strings.Repeat("a", 251) + ".jpg"

	rec := doGet(t, handler, "/images/"+name)

	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("name at length limit produced 500 instead of a clean 404 (body: %s)", rec.Body.String())
	}
	assertStatus(t, rec, http.StatusNotFound)
	assertNotFoundCode(t, rec)
	assertNoPathLeak(t, rec, imagesDir, parent)
}
