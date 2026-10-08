package domain

import "time"

func DemoEvents(now time.Time) []Event {
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
