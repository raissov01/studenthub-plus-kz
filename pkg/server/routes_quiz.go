package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/quiz"
)

// Quiz: Quizizz-style quizzes, AI generation from notes, live games over WebSocket; FEATURE_QUIZ.
func init() { register(func(mux httpx.Router) { quiz.Routes(mux) }) }
