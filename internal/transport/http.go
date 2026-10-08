package transport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/usecase"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type identity struct {
	User    domain.User
	Session domain.Session
}
type identityKey struct{}
type Handler struct {
	service usecase.Calendar
	logger  *slog.Logger
	secure  bool
}

func New(service usecase.Calendar, web fs.FS, metrics http.Handler, logger *slog.Logger, secureCookie bool) http.Handler {
	h := &Handler{service, logger, secureCookie}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Test(r.Context())
		if err != nil {
			h.failure(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, value)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := service.Ready(ctx); err != nil {
			writeError(w, 503, "Хранилище недоступно")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	for _, prefix := range []string{"/api/auth", "/auth"} {
		mux.HandleFunc("POST "+prefix+"/register", h.register)
		mux.HandleFunc("POST "+prefix+"/login", h.login)
		mux.Handle("POST "+prefix+"/logout", h.auth(http.HandlerFunc(h.logout)))
		mux.Handle("GET "+prefix+"/me", h.auth(http.HandlerFunc(h.me)))
	}
	mux.Handle("POST /dbtest", h.auth(http.HandlerFunc(h.probe)))
	mux.Handle("GET /api/events", h.auth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/events", h.auth(http.HandlerFunc(h.save)))
	mux.Handle("PUT /api/events/{id}", h.auth(http.HandlerFunc(h.save)))
	mux.Handle("DELETE /api/events/{id}", h.auth(http.HandlerFunc(h.delete)))
	mux.Handle("GET /api/export", h.auth(http.HandlerFunc(h.export)))
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "Маршрут API не найден")
	})
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					writeError(w, 403, "Запрос с другого сайта запрещён")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, "Запрос с другого сайта запрещён")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		header := r.Header.Get("Authorization")
		if header != "" {
			parts := strings.Fields(header)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, 401, "Некорректный токен")
				return
			}
			token = parts[1]
		} else if cookie, err := r.Cookie("orbita_session"); err == nil {
			token = cookie.Value
		}
		user, session, err := h.service.Authenticate(r.Context(), token)
		if err != nil {
			h.failure(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{user, session})))
	})
}
func current(r *http.Request) identity { return r.Context().Value(identityKey{}).(identity) }
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var c domain.Credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	result, err := h.service.Register(r.Context(), c)
	if err != nil {
		h.failure(w, err)
		return
	}
	h.setCookie(w, result)
	writeJSON(w, 201, result)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var c domain.Credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	result, err := h.service.Login(r.Context(), c)
	if err != nil {
		h.failure(w, err)
		return
	}
	h.setCookie(w, result)
	writeJSON(w, 200, result)
}
func (h *Handler) setCookie(w http.ResponseWriter, result domain.AuthResult) {
	http.SetCookie(w, &http.Cookie{Name: "orbita_session", Value: result.Token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode, Expires: result.ExpiresAt, MaxAge: int(time.Until(result.ExpiresAt).Seconds())})
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Logout(r.Context(), current(r).Session.ID); err != nil {
		h.failure(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "orbita_session", Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(204)
}
func (h *Handler) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, current(r).User) }
func (h *Handler) probe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 413, "Строка слишком длинная")
		return
	}
	if err = h.service.Probe(r.Context(), current(r).User.ID, string(body)); err != nil {
		h.failure(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"status": "saved"})
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.List(r.Context(), current(r).User.ID)
	if err != nil {
		h.failure(w, err)
		return
	}
	writeJSON(w, 200, events)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var e domain.Event
	if !decodeJSON(w, r, &e) {
		return
	}
	saved, err := h.service.Save(r.Context(), current(r).User.ID, e, r.PathValue("id"))
	if err != nil {
		h.failure(w, err)
		return
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	writeJSON(w, status, saved)
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), current(r).User.ID, r.PathValue("id")); err != nil {
		h.failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.List(r.Context(), current(r).User.ID)
	if err != nil {
		h.failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orbita.ics"`)
	io.WriteString(w, calendarICS(events, time.Now()))
}
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(w, 415, "Ожидается application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeError(w, 413, "Слишком большой запрос")
		} else {
			writeError(w, 400, "Некорректный JSON или неизвестное поле")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "Ожидается один JSON-объект")
		return false
	}
	return true
}
func (h *Handler) failure(w http.ResponseWriter, err error) {
	var invalid domain.ValidationError
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(w, 401, "Неверные данные входа или истекшая сессия")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, 404, err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeError(w, 409, err.Error())
	case errors.As(err, &invalid):
		writeError(w, 422, invalid.Message)
	default:
		h.logger.Error("request failed", "error", err)
		writeError(w, 500, "Не удалось сохранить данные. Попробуйте ещё раз")
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
