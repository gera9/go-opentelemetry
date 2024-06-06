package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
}

var tracer trace.Tracer

func main() {
	ctx := context.Background()
	exp, err := newOtlpHttpExporter(ctx)
	if err != nil {
		log.Fatalf("Failed to create the collector exporter: %v", err)
	}

	tp, err := newTraceProvider(exp)
	if err != nil {
		log.Fatalf("Failed to create the trace provider: %v", err)
	}

	defer func() {
		fmt.Printf("Error shuting down trace provider: %v\n", tp.Shutdown(context.TODO()).Error())
	}()

	otel.SetTracerProvider(tp)

	// Finally, set the tracer that can be used for this package.
	tracer = tp.Tracer("SenderService")

	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	conn, err := amqp.Dial("amqp://user:password@rabbitmq:5672/")
	if err != nil {
		log.Printf("Failed to connect to RabbitMQ: %v", err)
		time.Sleep(5 * time.Second)
		conn, err = amqp.Dial("amqp://user:password@rabbitmq:5672/")
		failOnError(err, "Failed to connect to RabbitMQ")
	}
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"hello", // name
		false,   // durable
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		nil,     // arguments
	)
	failOnError(err, "Failed to declare a queue")
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "http-request")
		defer span.End()

		sendMessage(ctx, ch, q, "Hello, World!")

		span.AddEvent("Write response")
		_, _ = w.Write([]byte("Hello, World!"))
	})

	http.ListenAndServe(":3000", mux)
}

func sendMessage(ctx context.Context, ch *amqp.Channel, q amqp.Queue, body string) {
	ctx, span := tracer.Start(ctx, "sendMessage")
	defer span.End()

	// Inject the trace context into the message headers.
	headers := make(amqp.Table)
	otel.GetTextMapPropagator().Inject(ctx, amqpHeadersCarrier(headers))

	err := ch.PublishWithContext(ctx,
		"",     // exchange
		q.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			Headers:     headers,
			ContentType: "text/plain",
			Body:        []byte(body),
		})
	failOnError(err, "Failed to publish a message")
	log.Printf(" [x] Sent %s\n", body)
}

type amqpHeadersCarrier amqp.Table

func (c amqpHeadersCarrier) Get(key string) string {
	if val, ok := c[key].(string); ok {
		return val
	}
	return ""
}

func (c amqpHeadersCarrier) Set(key string, value string) {
	c[key] = value
}

func (c amqpHeadersCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
