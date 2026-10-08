package broker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"sync"
	"time"
)

const (
	NewEvents   = "orbita.events.new"
	Statuses    = "orbita.events.status"
	DeadLetters = "orbita.events.dead"
)

type Rabbit struct {
	mu         sync.Mutex
	url        string
	connection *amqp.Connection
	closed     bool
	logger     *slog.Logger
}

func New(url string, logger *slog.Logger) *Rabbit { return &Rabbit{url: url, logger: logger} }
func (r *Rabbit) connect() (*amqp.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("broker closed")
	}
	if r.connection != nil && !r.connection.IsClosed() {
		return r.connection, nil
	}
	conn, err := amqp.DialConfig(r.url, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer ch.Close()
	if _, err = ch.QueueDeclare(DeadLetters, true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	args := amqp.Table{"x-dead-letter-exchange": "", "x-dead-letter-routing-key": DeadLetters}
	for _, q := range []string{NewEvents, Statuses} {
		if _, err = ch.QueueDeclare(q, true, false, false, false, args); err != nil {
			conn.Close()
			return nil, err
		}
	}
	r.connection = conn
	return conn, nil
}
func (r *Rabbit) Ready(ctx context.Context) error { _, err := r.connect(); return err }
func (r *Rabbit) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.connection != nil {
		r.connection.Close()
	}
}
func (r *Rabbit) PublishStatus(ctx context.Context, job domain.Job) error {
	return r.Send(ctx, Statuses, job)
}
func (r *Rabbit) Send(ctx context.Context, queue string, job domain.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := r.connect()
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.Confirm(false); err != nil {
		return err
	}
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, "", queue, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: job.ID, Body: body})
	if err != nil {
		return err
	}
	ok, err := confirm.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("broker rejected publication")
	}
	select {
	case <-returns:
		return errors.New("message has no destination queue")
	default:
	}
	return nil
}

// Listen uses bounded concurrency and acknowledges a message only after the
// handler has persisted its result or received a publisher confirmation.
func (r *Rabbit) Listen(ctx context.Context, queue string, workers int, handler func(context.Context, domain.Job) error) {
	for ctx.Err() == nil {
		err := r.consume(ctx, queue, workers, handler)
		if ctx.Err() != nil {
			return
		}
		r.logger.Error("queue consumer reconnecting", "queue", queue, "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (r *Rabbit) consume(ctx context.Context, queue string, workers int, handler func(context.Context, domain.Job) error) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.Qos(workers, 0, false); err != nil {
		return err
	}
	tag, err := domain.NewID()
	if err != nil {
		return err
	}
	messages, err := ch.Consume(queue, tag, false, false, false, false, nil)
	if err != nil {
		return err
	}
	slots := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var ackMu sync.Mutex
	defer wg.Wait()
	for {
		select {
		case <-ctx.Done():
			ch.Cancel(tag, false)
			return nil
		case delivery, ok := <-messages:
			if !ok {
				return errors.New("delivery channel closed")
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				delivery.Nack(false, true)
				ch.Cancel(tag, false)
				return nil
			}
			wg.Add(1)
			go func(d amqp.Delivery) {
				defer wg.Done()
				defer func() { <-slots }()
				var job domain.Job
				err := json.Unmarshal(d.Body, &job)
				if err == nil && (job.ID == "" || job.EventID == "" || job.UserID == "" || job.Revision < 1) {
					err = domain.Invalid("invalid job")
				}
				retry := false
				if err == nil {
					workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err = handler(workCtx, job)
					cancel()
					retry = err != nil
					var invalid domain.ValidationError
					if errors.As(err, &invalid) {
						retry = false
					}
				}
				ackMu.Lock()
				defer ackMu.Unlock()
				if err != nil {
					r.logger.Error("job failed", "queue", queue, "job", job.ID, "error", err)
					d.Nack(false, retry)
				} else {
					d.Ack(false)
				}
			}(delivery)
		}
	}
}
