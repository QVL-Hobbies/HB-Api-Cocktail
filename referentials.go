package main

import (
	"context"
	"database/sql"
	"net/http"
)

type ingredient struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func handleListIngredients(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), "SELECT id, name FROM ingredients ORDER BY name")
		if err != nil {
			writeInternalError(w)
			return
		}
		defer rows.Close()

		ingredients := []ingredient{}
		for rows.Next() {
			var ing ingredient
			if err := rows.Scan(&ing.ID, &ing.Name); err != nil {
				writeInternalError(w)
				return
			}
			ingredients = append(ingredients, ing)
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w)
			return
		}

		writeJSON(w, http.StatusOK, ingredients)
	}
}

func handleListCategories(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categories, err := queryStrings(r.Context(), db, "SELECT DISTINCT category FROM cocktails ORDER BY category")
		if err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, categories)
	}
}

func handleListTags(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tags, err := queryStrings(r.Context(), db, "SELECT DISTINCT tag FROM cocktail_tags ORDER BY tag")
		if err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

func queryStrings(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}
