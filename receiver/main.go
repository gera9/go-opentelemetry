package main

import (
	"context"
	"fmt"
	"log"
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
	tracer = tp.Tracer("ReceiverService")

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

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	var forever chan struct{}

	go func() {
		for d := range msgs {
			// Extract the trace context from the message headers.
			ctx := otel.GetTextMapPropagator().Extract(context.Background(), amqpHeadersCarrier(d.Headers))
			// get the span from the context
			//span := trace.SpanFromContext(ctx)
			ctx, span := tracer.Start(ctx, "receiveMessage")
			printMessage(ctx, d.Body)
			span.End()
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}

func printMessage(ctx context.Context, body []byte) {
	_, span := tracer.Start(ctx, "printLogMessage")
	defer span.End()
	log.Printf("Received a message: %s", body)
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
