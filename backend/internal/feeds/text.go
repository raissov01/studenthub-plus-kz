package feeds

import (
	"hash/fnv"
	"math/bits"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---------- prefilter ----------

// keywords: stems in kk / ru / en. A post must contain at least one (cheap, before any AI).
var keywords = []string{
	// grants, scholarships
	"грант", "стипенд", "scholarship", "fellowship", "bursary", "болашақ", "bolashak", "chevening", "fulbright",
	"daad", "erasmus", "эразмус", "mext", "gks", "csc", "türkiye burs", "stipendium",
	// mobility, exchange, programs
	"мобильност", "ұтқырлық", "mobility", "exchange", "обмен", "алмасу", "summer school", "winter school",
	"летн", "зимн", "жазғы мектеп", "магистратур", "master", "phd", "докторантур", "bachelor", "бакалавриат",
	// startups, hackathons, competitions
	"хакатон", "hackathon", "стартап", "startup", "акселерат", "accelerat", "инкубат", "incubat", "pitch", "питч",
	"конкурс", "байқау", "competition", "contest", "challenge", "олимпиад", "olympiad", "чемпионат", "championship",
	"кубок", "турнир", "prize", "приз", "жүлде",
	// internships, jobs
	"стажир", "тағылымдама", "internship", "intern ", "практик", "вакансия", "vacancy", "карьер", "career",
	"трудоустр", "жұмысқа", "junior",
	// events
	"конференц", "conference", "форум", "forum", "summit", "саммит", "семинар", "workshop", "воркшоп", "митап", "meetup",
	"тренинг", "training", "bootcamp", "буткемп", "курс", "course", "мастер-класс", "masterclass", "webinar", "вебинар",
	// volunteering
	"волонт", "volunteer", "ерікті",
	// calls to apply
	"дедлайн", "deadline", "өтінім", "заявк", "apply", "registration", "регистрац", "тіркел", "қабылдау", "приём", "прием",
	"набор", "admission", "отбор", "іріктеу", "участ", "қатыс",
}

// negative: obvious ads and noise; any hit drops the post unless it also names a grant/scholarship.
var negative = []string{"#реклама", "реклама", "промокод", "скидк", "розыгрыш", "giveaway", "казино", "ставки", "букмекер", "airdrop", "криптовалют", "крипто-"}

var strong = []string{"грант", "стипенд", "scholarship", "fellowship", "хакатон", "hackathon", "erasmus", "стажир", "internship"}

// Prefilter decides whether a post is worth an AI call; reason explains a rejection.
func Prefilter(p Post, s Source) (ok bool, reason string) {
	text := strings.ToLower(p.Text)
	// our own channel posts (FEEDS_PUBLISH_CHANNEL) link back to /opportunity/feed-<n>: never re-collect them
	if strings.Contains(text, "/opportunity/"+IDPrefix) {
		return false, "own feed post"
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Text)) < 40 {
		return false, "too short"
	}
	hasStrong := containsAny(text, strong)
	if containsAny(text, negative) && !hasStrong {
		return false, "advertising"
	}
	if s.Relevant || hasStrong || containsAny(text, keywords) {
		return true, ""
	}
	return false, "no keywords"
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// ---------- normalization, simhash ----------

// NormTitle folds case, punctuation and spacing for the title+deadline dedupe key.
func NormTitle(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "ё", "е"))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return b.String()
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"'«»()\[\]]+`)

// URLsIn lists the http(s) URLs written in text (trailing punctuation trimmed).
func URLsIn(text string) []string {
	var out []string
	for _, u := range urlRe.FindAllString(text, -1) {
		out = append(out, strings.TrimRight(u, ".,;:!?…"))
	}
	return out
}

// CanonicalURL drops the scheme, www., fragment, tracking parameters and a trailing slash, so
// the same page linked from two channels compares equal. "" for anything that is not http(s).
func CanonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "ref" || lk == "si" {
			q.Del(k)
		}
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	out := host + path
	if enc := q.Encode(); enc != "" {
		out += "?" + enc
	}
	return out
}

// Simhash is a 64-bit simhash over word 3-shingles of the normalized text; near-duplicate
// posts (the same announcement reposted with an extra line) differ in a few bits.
func Simhash(text string) uint64 {
	words := strings.Fields(NormTitle(urlRe.ReplaceAllString(text, " ")))
	if len(words) == 0 {
		return 0
	}
	var v [64]int
	add := func(sh string) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(sh))
		x := h.Sum64()
		for i := 0; i < 64; i++ {
			if x&(1<<uint(i)) != 0 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	if len(words) < 3 {
		add(strings.Join(words, " "))
	} else {
		for i := 0; i+3 <= len(words); i++ {
			add(words[i] + " " + words[i+1] + " " + words[i+2])
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if v[i] > 0 {
			out |= 1 << uint(i)
		}
	}
	return out
}

// Hamming is the number of differing bits.
func Hamming(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// NearDuplicate: simhash distance threshold for "the same post". A repost with an emoji and a
// hashtag line differs in 8-9 bits, two different announcements from one template in ~24.
const NearDuplicate = 12
