package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/clubs"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/events"
)

// Clubs × events: a club admin creates and manages their club's events through the normal
// events flow (QR check-in, calendar, points). RLS mirror: events_club_* policies.
func init() { events.ClubManager = clubs.ManagedUniversity }
