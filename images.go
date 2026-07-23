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

func handleGetImage(imagesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		contentType, ok := validateImageName(name)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "image not found")
			return
		}

		fullPath, err := resolveContainedImagePath(imagesDir, name)
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

func resolveContainedImagePath(imagesDir, name string) (string, error) {
	fullPath := filepath.Join(imagesDir, name)

	absDir, err := filepath.Abs(imagesDir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absFull, absDir+string(os.PathSeparator)) {
		return "", errImageNotFound
	}

	resolved, err := filepath.EvalSymlinks(absFull)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errImageNotFound
		}
		return "", err
	}
	if !strings.HasPrefix(resolved, absDir+string(os.PathSeparator)) {
		return "", errImageNotFound
	}
	return resolved, nil
}
