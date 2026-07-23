package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var errImageNotFound = errors.New("image not found")

var imageNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var imageContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
}

var imageExtensionsByContentType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

func handleGetImage(resolvedImagesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		contentType, ok := validateImageName(name)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "image not found")
			return
		}

		fullPath, err := resolveContainedImagePath(resolvedImagesDir, name)
		if err != nil {
			if errors.Is(err, errImageNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "image not found")
				return
			}
			writeInternalError(w)
			return
		}

		file, err := os.Open(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "not_found", "image not found")
				return
			}
			writeInternalError(w)
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			writeInternalError(w)
			return
		}
		if info.IsDir() {
			writeError(w, http.StatusNotFound, "not_found", "image not found")
			return
		}

		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, name, info.ModTime(), file)
	}
}

func validateImageName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if len(name) > 255 {
		return "", false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	if strings.Contains(name, "..") {
		return "", false
	}
	if !imageNamePattern.MatchString(name) {
		return "", false
	}
	contentType, ok := imageContentTypes[strings.ToLower(filepath.Ext(name))]
	return contentType, ok
}

func containedPath(dir, name string) (string, error) {
	if _, ok := validateImageName(name); !ok {
		return "", errImageNotFound
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absFull, absDir+string(os.PathSeparator)) {
		return "", errImageNotFound
	}
	return absFull, nil
}

func resolveContainedImagePath(resolvedImagesDir, name string) (string, error) {
	candidate := filepath.Join(resolvedImagesDir, name)

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errImageNotFound
		}
		return "", err
	}
	if !strings.HasPrefix(resolved, resolvedImagesDir+string(os.PathSeparator)) {
		return "", errImageNotFound
	}
	return resolved, nil
}

func storeImageFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func detectImageExtension(content []byte) (string, bool) {
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	contentType := http.DetectContentType(head)
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	ext, ok := imageExtensionsByContentType[contentType]
	return ext, ok
}
