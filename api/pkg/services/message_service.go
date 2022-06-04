package services

import (
	"context"
	"fmt"
	"time"

	"github.com/NdoleStudio/http-sms-manager/pkg/events"
	"github.com/NdoleStudio/http-sms-manager/pkg/repositories"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/palantir/stacktrace"

	"github.com/NdoleStudio/http-sms-manager/pkg/entities"
	"github.com/NdoleStudio/http-sms-manager/pkg/telemetry"
)

// MessageService is handles message requests
type MessageService struct {
	logger          telemetry.Logger
	tracer          telemetry.Tracer
	eventDispatcher *EventDispatcher
	repository      repositories.MessageRepository
}

// NewMessageService creates a new MessageService
func NewMessageService(
	logger telemetry.Logger,
	tracer telemetry.Tracer,
	repository repositories.MessageRepository,
	eventDispatcher *EventDispatcher,
) (s *MessageService) {
	return &MessageService{
		logger:          logger.WithService(fmt.Sprintf("%T", s)),
		tracer:          tracer,
		repository:      repository,
		eventDispatcher: eventDispatcher,
	}
}

// SendMessage a new message
func (service *MessageService) SendMessage(ctx context.Context, params MessageSendParams) (*entities.Message, error) {
	ctx, span := service.tracer.Start(ctx)
	defer span.End()

	ctxLogger := service.tracer.CtxLogger(service.logger, span)

	eventPayload := events.MessageAPISentPayload{
		ID:                uuid.New(),
		From:              params.From,
		To:                params.To,
		RequestReceivedAt: params.RequestReceivedAt,
		Content:           params.Content,
	}

	ctxLogger.Info(fmt.Sprintf("creating cloud event for message with ID [%s]", eventPayload.ID))

	event, err := service.createMessageAPISentEvent(params.Source, eventPayload)
	if err != nil {
		msg := fmt.Sprintf("cannot create %T from payload with message id [%s]", event)
		return nil, service.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	ctxLogger.Info(fmt.Sprintf("created event [%s] with id [%s] and message id [%s]", event.Type(), event.ID(), eventPayload.ID))

	if err = service.eventDispatcher.Dispatch(ctx, event); err != nil {
		msg := fmt.Sprintf("cannot dispatch event type [%s] and id [%s]", event.Type(), event.ID())
		return nil, service.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	ctxLogger.Info(fmt.Sprintf("event [%s] dispatched succesfully", event.ID()))

	message, err := service.repository.Load(ctx, eventPayload.ID)
	if err != nil {
		msg := fmt.Sprintf("cannot load message with ID [%s] in the repository", eventPayload.ID)
		return nil, service.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	ctxLogger.Info(fmt.Sprintf("fetched message with id [%s] from the repository", message.ID))

	return message, nil
}

// StoreMessage a new message
func (service *MessageService) StoreMessage(ctx context.Context, params MessageStoreParams) (*entities.Message, error) {
	ctx, span := service.tracer.Start(ctx)
	defer span.End()

	ctxLogger := service.tracer.CtxLogger(service.logger, span)

	message := &entities.Message{
		ID:                params.ID,
		From:              params.From,
		To:                params.To,
		Content:           params.Content,
		Type:              entities.MessageTypeMobileTerminated,
		Status:            entities.MessageStatusPending,
		RequestReceivedAt: params.RequestReceivedAt,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
		OrderTimestamp:    params.RequestReceivedAt,
		SendDuration:      nil,
		LastAttemptedAt:   nil,
		SentAt:            nil,
		ReceivedAt:        nil,
	}

	if err := service.repository.Save(ctx, message); err != nil {
		msg := fmt.Sprintf("cannot save message with id [%s]", params.ID)
		return nil, service.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	ctxLogger.Info(fmt.Sprintf("message saved with id [%s] in the repository", message.ID))
	return message, nil
}

func (service *MessageService) createMessageAPISentEvent(source string, payload events.MessageAPISentPayload) (event cloudevents.Event, err error) {
	event = cloudevents.NewEvent()

	event.SetSource(source)
	event.SetType(events.EventTypeMessageAPISent)
	event.SetTime(time.Now().UTC())
	event.SetID(uuid.New().String())

	if err = event.SetData(cloudevents.ApplicationJSON, payload); err != nil {
		msg := fmt.Sprintf("cannot encode %T [%#+v] as JSON", payload, payload)
		return event, stacktrace.Propagate(err, msg)
	}

	return event, nil
}
