package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound     = errors.New("Событие не найдено")
	ErrUnauthorized = errors.New("Войдите в аккаунт")
	ErrConflict     = errors.New("Этот email уже зарегистрирован")
)

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }
func Invalid(message string) error      { return ValidationError{message} }

type User struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
}
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}
type AuthResult struct {
	User      User      `json:"user"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Event struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Date             string `json:"date"`
	Start            string `json:"start"`
	End              string `json:"end"`
	AllDay           bool   `json:"allDay"`
	Category         string `json:"category"`
	Description      string `json:"description"`
	Location         string `json:"location"`
	Completed        bool   `json:"completed"`
	ProcessingStatus string `json:"processingStatus,omitempty"`
	Revision         int64  `json:"-"`
}
type Job struct {
	ID       string `json:"id"`
	EventID  string `json:"eventId"`
	UserID   string `json:"userId"`
	Revision int64  `json:"revision"`
	Status   string `json:"status,omitempty"`
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func NormalizeCredentials(c *Credentials, registration bool) error {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.Name = strings.TrimSpace(c.Name)
	address, err := mail.ParseAddress(c.Email)
	if err != nil || address.Address != c.Email || len(c.Email) > 254 {
		return Invalid("Введите корректный email")
	}
	if registration && (utf8.RuneCountInString(c.Name) < 1 || utf8.RuneCountInString(c.Name) > 80) {
		return Invalid("Имя должно содержать от 1 до 80 символов")
	}
	if utf8.RuneCountInString(c.Password) < 8 || len(c.Password) > 72 {
		return Invalid("Пароль: не менее 8 символов и не более 72 байт")
	}
	return nil
}
func ValidateEvent(e *Event) error {
	e.Title = strings.TrimSpace(e.Title)
	e.Description = strings.TrimSpace(e.Description)
	e.Location = strings.TrimSpace(e.Location)
	if e.Title == "" || utf8.RuneCountInString(e.Title) > 100 {
		return Invalid("Название должно содержать от 1 до 100 символов")
	}
	if utf8.RuneCountInString(e.Description) > 2000 || utf8.RuneCountInString(e.Location) > 200 {
		return Invalid("Описание: до 2000 символов; место: до 200 символов")
	}
	d, err := time.Parse("2006-01-02", e.Date)
	if err != nil || d.Format("2006-01-02") != e.Date || d.Year() < 1900 || d.Year() > 2100 {
		return Invalid("Выберите корректную дату между 1900 и 2100 годами")
	}
	switch e.Category {
	case "work", "personal", "study", "space":
	default:
		return Invalid("Неизвестная категория события")
	}
	if e.AllDay {
		e.Start = ""
		e.End = ""
		return nil
	}
	start, sErr := time.Parse("15:04", e.Start)
	end, eErr := time.Parse("15:04", e.End)
	if sErr != nil || eErr != nil || start.Format("15:04") != e.Start || end.Format("15:04") != e.End || !end.After(start) {
		return Invalid("Время окончания должно быть позже начала в тот же день")
	}
	return nil
}
