package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

func newHandler(store *Store, web fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.list())
	})
	mux.HandleFunc("POST /api/events", func(w http.ResponseWriter, r *http.Request) {
		saveEvent(w, r, store, "")
	})
	mux.HandleFunc("PUT /api/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		saveEvent(w, r, store, r.PathValue("id"))
	})
	mux.HandleFunc("DELETE /api/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.delete(r.PathValue("id")); err != nil {
			storeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="orbita.ics"`)
		fmt.Fprint(w, calendarICS(store.list(), time.Now()))
	})
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Маршрут API не найден")
	})
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					writeError(w, http.StatusForbidden, "Запрос с другого сайта запрещён")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, http.StatusForbidden, "Запрос с другого сайта запрещён")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func saveEvent(w http.ResponseWriter, r *http.Request, store *Store, id string) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Ожидается application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var e Event
	if err := decoder.Decode(&e); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Слишком большой запрос")
		} else {
			writeError(w, http.StatusBadRequest, "Некорректный JSON или неизвестное поле")
		}
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "Ожидается один JSON-объект")
		return
	}
	if err := validateEvent(&e); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	e, err := store.save(e, id)
	if err != nil {
		storeError(w, err)
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, e)
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	log.Printf("Ошибка хранилища: %v", err)
	writeError(w, http.StatusInternalServerError, "Не удалось сохранить данные. Попробуйте ещё раз")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func escapeICS(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", ";", "\\;", ",", "\\,").Replace(value)
}

// RFC 5545 folds lines at 75 octets, without cutting through a UTF-8 character.
func foldICS(line string) string {
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = 74
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}

func calendarICS(events []Event, now time.Time) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Orbita//Space Calendar//RU", "CALSCALE:GREGORIAN", "X-WR-CALNAME:Орбита"}
	for _, e := range events {
		date := strings.ReplaceAll(e.Date, "-", "")
		lines = append(lines, "BEGIN:VEVENT", "UID:"+escapeICS(e.ID)+"@orbita.local", "DTSTAMP:"+now.UTC().Format("20060102T150405Z"))
		if e.AllDay {
			d, _ := time.Parse("2006-01-02", e.Date)
			lines = append(lines, "DTSTART;VALUE=DATE:"+date, "DTEND;VALUE=DATE:"+d.AddDate(0, 0, 1).Format("20060102"))
		} else {
			// Floating times follow the importing calendar's local time zone.
			lines = append(lines, "DTSTART:"+date+"T"+strings.ReplaceAll(e.Start, ":", "")+"00", "DTEND:"+date+"T"+strings.ReplaceAll(e.End, ":", "")+"00")
		}
		lines = append(lines, "SUMMARY:"+escapeICS(e.Title), "DESCRIPTION:"+escapeICS(e.Description), "LOCATION:"+escapeICS(e.Location), "CATEGORIES:"+e.Category, "END:VEVENT")
	}
	lines = append(lines, "END:VCALENDAR")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(foldICS(line))
	}
	return b.String()
}
