package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/points"
)

// Gamification «Ұпайлар / Баллы»: XP, streak, coins ledger, rewards shop and its admin queue;
// FEATURE_POINTS.
func init() { register(func(mux httpx.Router) { points.Routes(mux) }) }
