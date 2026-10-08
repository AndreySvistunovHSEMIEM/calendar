package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var errNotFound = errors.New("Событие не найдено")

type Event struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Date        string `json:"date"`
	Start       string `json:"start"`
	End         string `json:"end"`
	AllDay      bool   `json:"allDay"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Completed   bool   `json:"completed"`
}

type eventFile struct {
	Version int     `json:"version"`
	Events  []Event `json:"events"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	events []Event
}

func validateEvent(e *Event) error {
	e.Title = strings.TrimSpace(e.Title)
	e.Description = strings.TrimSpace(e.Description)
	e.Location = strings.TrimSpace(e.Location)
	if e.Title == "" || utf8.RuneCountInString(e.Title) > 100 {
		return errors.New("Название должно содержать от 1 до 100 символов")
	}
	if utf8.RuneCountInString(e.Description) > 2000 || utf8.RuneCountInString(e.Location) > 200 {
		return errors.New("Описание: до 2000 символов; место: до 200 символов")
	}
	d, err := time.Parse("2006-01-02", e.Date)
	if err != nil || d.Format("2006-01-02") != e.Date || d.Year() < 1900 || d.Year() > 2100 {
		return errors.New("Выберите корректную дату между 1900 и 2100 годами")
	}
	switch e.Category {
	case "work", "personal", "study", "space":
	default:
		return errors.New("Неизвестная категория события")
	}
	if e.AllDay {
		e.Start, e.End = "", ""
		return nil
	}
	start, sErr := time.Parse("15:04", e.Start)
	end, eErr := time.Parse("15:04", e.End)
	if sErr != nil || eErr != nil || start.Format("15:04") != e.Start || end.Format("15:04") != e.End || !end.After(start) {
		return errors.New("Время окончания должно быть позже начала в тот же день")
	}
	return nil
}

func openStore(path string, demo bool, now time.Time) (*Store, error) {
	s := &Store{path: path, events: []Event{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if demo {
			s.events = demoEvents(now)
		}
		if err := s.persist(s.events); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var file eventFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("файл данных повреждён (он не изменён): %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("неподдерживаемая версия данных: %d", file.Version)
	}
	ids := map[string]bool{}
	for _, e := range file.Events {
		if e.ID == "" || ids[e.ID] {
			return nil, errors.New("В файле обнаружены пустые или повторяющиеся ID")
		}
		ids[e.ID] = true
		if err := validateEvent(&e); err != nil {
			return nil, fmt.Errorf("событие %s: %w", e.ID, err)
		}
		s.events = append(s.events, e)
	}
	return s, nil
}

// Save to a temporary file in the same directory, then atomically replace the
// original. A failed write never changes the in-memory calendar.
func (s *Store) persist(events []Event) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".events-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(eventFile{Version: 1, Events: events}); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}

func (s *Store) list() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]Event{}, s.events...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date < result[j].Date
		}
		return result[i].Start < result[j].Start
	})
	return result
}

func (s *Store) save(e Event, id string) (Event, error) {
	if err := validateEvent(&e); err != nil {
		return Event{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]Event{}, s.events...)
	if id == "" {
		var bytes [16]byte
		if _, err := io.ReadFull(rand.Reader, bytes[:]); err != nil {
			return Event{}, err
		}
		e.ID = hex.EncodeToString(bytes[:])
		next = append(next, e)
	} else {
		index := -1
		for i := range next {
			if next[i].ID == id {
				index = i
				break
			}
		}
		if index == -1 {
			return Event{}, errNotFound
		}
		e.ID = id
		next[index] = e
	}
	if err := s.persist(next); err != nil {
		return Event{}, fmt.Errorf("ошибка сохранения: %w", err)
	}
	s.events = next
	return e, nil
}

func (s *Store) delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make([]Event, 0, len(s.events))
	found := false
	for _, e := range s.events {
		if e.ID == id {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return errNotFound
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.events = next
	return nil
}

func demoEvents(now time.Time) []Event {
	date := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	return []Event{
		{ID: "demo-1", Title: "Планирование недели", Date: date(0), Start: "10:00", End: "10:45", Category: "work", Location: "Центр управления", Description: "Демо-событие. Обсудить планы и выбрать главные задачи недели."},
		{ID: "demo-2", Title: "Время для себя", Date: date(0), Start: "18:30", End: "19:30", Category: "personal", Description: "Демо-событие. Прогулка, музыка и немного тишины."},
		{ID: "demo-3", Title: "Изучение Go", Date: date(1), Start: "14:00", End: "15:30", Category: "study", Description: "Демо-событие. Практика с net/http и тестами."},
		{ID: "demo-4", Title: "Вечер под звёздами", Date: date(2), Start: "21:00", End: "22:30", Category: "space", Location: "За городом", Description: "Демо-событие для планирования наблюдений. Проверьте погоду и условия самостоятельно."},
		{ID: "demo-5", Title: "Запуск нового проекта", Date: date(5), Start: "11:00", End: "12:00", Category: "work", Description: "Демо-событие. Всё начинается с первого шага."},
		{ID: "demo-6", Title: "День без спешки", Date: date(7), AllDay: true, Category: "personal", Description: "Демо-событие. Оставить место для новых идей."},
		{ID: "demo-7", Title: "Практика по Go", Date: date(-2), Start: "16:00", End: "17:00", Category: "study", Completed: true, Description: "Демо-событие с отметкой о завершении."},
	}
}
