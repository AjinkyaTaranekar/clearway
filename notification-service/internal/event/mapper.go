package event

import (
	"fmt"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
)

// MappedNotification holds the title, message and type derived from a journey event.
type MappedNotification struct {
	Title   string
	Message string
	Type    string // model.TypeInfo / TypeSuccess / TypeWarning / TypeError
}

// MapEvent converts a journey event envelope into notification content.
// Returns an error only if the event type is completely unknown.
func MapEvent(env *Envelope) (*MappedNotification, error) {
	origin := env.Payload.OriginLabel
	dest := env.Payload.DestinationLabel

	switch env.EventType {
	case JourneyBooked:
		return &MappedNotification{
			Title:   "Journey Approved",
			Message: fmt.Sprintf("Your journey from %s to %s has been approved.", safe(origin), safe(dest)),
			Type:    model.TypeSuccess,
		}, nil

	case JourneyRejected:
		reason := env.Payload.Reason
		if reason == "" {
			reason = "capacity constraints"
		}
		return &MappedNotification{
			Title:   "Journey Rejected",
			Message: fmt.Sprintf("Your journey from %s to %s was rejected due to %s.", safe(origin), safe(dest), reason),
			Type:    model.TypeError,
		}, nil

	case JourneyCancelled:
		return &MappedNotification{
			Title:   "Journey Cancelled",
			Message: fmt.Sprintf("Your journey from %s to %s has been cancelled.", safe(origin), safe(dest)),
			Type:    model.TypeWarning,
		}, nil

	case JourneyActivated:
		return &MappedNotification{
			Title:   "Journey Started",
			Message: "Your journey has started. Drive safe!",
			Type:    model.TypeInfo,
		}, nil

	case JourneyCompleted:
		return &MappedNotification{
			Title:   "Journey Completed",
			Message: fmt.Sprintf("Your journey from %s to %s has been completed successfully.", safe(origin), safe(dest)),
			Type:    model.TypeSuccess,
		}, nil

	case JourneyExpired:
		return &MappedNotification{
			Title:   "Journey Expired",
			Message: "Your journey booking has expired because it was not activated on time.",
			Type:    model.TypeWarning,
		}, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", env.EventType)
	}
}

// safe returns a fallback string when a label is empty.
func safe(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}
