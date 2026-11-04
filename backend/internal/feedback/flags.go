package feedback

import (
	"strings"
	"unicode"
)

// Flags. A simple keyword heuristic, deliberately conservative, mirrored by the frontend
// (src/lib/voice.ts) so the student sees the notice BEFORE sending:
//   - threat: violence, weapons, self-harm or another emergency. The ticket is held and escalated
//     to the platform admins at once; the student is told so and shown the 112 / help lines.
//   - profanity: held for an admin to review before it reaches the recipient.
const (
	FlagThreat    = "threat"
	FlagProfanity = "profanity"
)

// word starts (matched at the beginning of a word, lower-case, ё → е)
var threatRoots = []string{
	// ru
	"убью", "убьем", "убить", "убийств", "зарежу", "застрел", "пристрел", "взорв", "бомб", "теракт", "стрельб", "оружи",
	"суицид", "самоубий", "повешусь", "повеситься",
	// kk
	"өлтір", "жарып", "жарылғыш", "бомба", "атып", "қару", "асылып", "суицид",
	// en
	"kill", "bomb", "shoot", "suicide",
}

// phrases (matched anywhere, words separated by single spaces)
var threatPhrases = []string{
	"покончить с собой", "покончу с собой", "не хочу жить", "наложу на себя руки", "свести счеты с жизнью",
	"өзіме қол жұмса", "өмір сүргім келмейді", "өлгім келеді", "өзімді өлтір",
	"end my life",
}

var profanityRoots = []string{
	"хуй", "хуе", "хуё", "хуя", "пизд", "ебат", "ебан", "ебал", "ебу", "заеб", "уеб", "выеб", "бля", "сука", "суки", "мудак", "мудил",
	"долбоеб", "гандон", "пидор", "пидар", "шлюх",
	"сіктір", "сігейін", "қотақ", "амың", "амыңды",
	"fuck", "shit",
}

func normalize(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "ё", "е"), "Ё", "е"))
	var b strings.Builder
	space := true
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func hasRoot(words []string, roots []string) bool {
	for _, w := range words {
		for _, r := range roots {
			if strings.HasPrefix(w, r) {
				return true
			}
		}
	}
	return false
}

// Detect returns FlagThreat, FlagProfanity or "". Threat wins.
func Detect(text string) string {
	n := normalize(text)
	words := strings.Fields(n)
	if hasRoot(words, threatRoots) {
		return FlagThreat
	}
	padded := " " + n + " "
	for _, p := range threatPhrases {
		if strings.Contains(padded, " "+p) {
			return FlagThreat
		}
	}
	if hasRoot(words, profanityRoots) {
		return FlagProfanity
	}
	return ""
}
