package main

import "github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"

type Event = domain.Event

func testEvent() Event {
	return Event{Title: "Тестовая встреча", Date: "2026-10-08", Start: "10:00", End: "11:00", Category: "work"}
}
