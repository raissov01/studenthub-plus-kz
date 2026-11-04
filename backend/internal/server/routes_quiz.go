package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/quiz"
)

// Quiz: Quizizz-style quizzes, AI generation from notes, live games over WebSocket; FEATURE_QUIZ.
func init() { register(func(mux httpx.Router) { quiz.Routes(mux) }) }
