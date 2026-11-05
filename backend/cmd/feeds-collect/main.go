// feeds-collect: one pass of the opportunity feed (docs/feeds.md). Production runs it every 30
// minutes from the host timer docker/systemd/studenthub-feeds.timer as a one-off container of the
// API image; it exits at once while FEATURE_FEEDS is off.
//
//	go run ./cmd/feeds-collect --dry-run --source tg-erasmus-kz,rss-opportunity-desk   # fetch + print, no DB, no AI
//	go run ./cmd/feeds-collect --no-ai           # fetch and store, posts wait for an AI run
//	go run ./cmd/feeds-collect                   # full pass
//
// Env: DATABASE_URL, FEATURE_FEEDS, OPENAI_API_KEY (+ OPENAI_BASE_URL, OPENAI_MODEL / FEEDS_AI_MODEL),
// FEEDS_AI_DAILY_LIMIT, FEEDS_AUTOPUBLISH_MIN_CONFIDENCE, FEEDS_PUBLISH_CHANNEL + BOT_TOKEN,
// FEEDS_SOURCES, and for tg_mtproto sources FEEDS_TG_SESSION + TELEGRAM_APP_ID + TELEGRAM_APP_HASH.
// Nothing secret is printed: no session bytes, keys, hashes or tokens.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds/telegram"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds/telegram/mtproto"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds/web"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

func main() {
	dry := flag.Bool("dry-run", false, "fetch and print posts as JSON lines; no database, no AI")
	noAI := flag.Bool("no-ai", false, "fetch and store only")
	only := flag.String("source", "", "comma-separated source ids (dry run: disabled ones too)")
	limit := flag.Int("limit", 5, "dry run: posts printed per source")
	path := flag.String("sources", "", "registry file (default $FEEDS_SOURCES or feeds/sources.yaml)")
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("feeds-collect: ")

	if !*dry && !features.Enabled(feeds.Feature) {
		log.Print("FEATURE_FEEDS is off, nothing to do")
		return
	}
	file := *path
	if file == "" {
		file = feeds.RegistryPath(os.Getenv)
	}
	list, err := feeds.LoadRegistry(file)
	if err != nil {
		log.Fatal(err)
	}
	if *only != "" {
		want := strings.Split(*only, ",")
		var picked []feeds.Source
		for _, s := range list {
			if slices.Contains(want, s.ID) {
				if *dry {
					s.Enabled = true
				}
				picked = append(picked, s)
			}
		}
		if len(picked) == 0 {
			log.Fatalf("no source matches %q", *only)
		}
		list = picked
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Minute) // the next timer run starts in 30
	defer cancel()

	w := feeds.NewWeb()
	c := &feeds.Collector{
		Sources: list,
		Fetchers: map[string]feeds.Fetcher{
			feeds.KindTGPublic: &telegram.Public{Web: w},
			feeds.KindRSS:      &web.RSS{Web: w},
			feeds.KindHTML:     &web.HTMLList{Web: w},
		},
		Logf: log.Printf,
	}
	c.Settings()
	if needsMTProto(list, *dry) {
		appID, _ := strconv.Atoi(config.Str("TELEGRAM_APP_ID", ""))
		r, err := mtproto.New(config.Str("FEEDS_TG_SESSION", ""), appID, config.Str("TELEGRAM_APP_HASH", ""))
		if err != nil {
			log.Printf("tg_mtproto sources skipped: %v", err)
		} else {
			c.Fetchers[feeds.KindTGMTProto] = r
		}
	}

	if *dry {
		rep := c.DryRun(ctx, os.Stdout, *limit)
		printReport(rep)
		return
	}
	if !*noAI {
		ai := feeds.NewAI(os.Getenv)
		if ai.Key == "" {
			log.Print("OPENAI_API_KEY is not set: collecting only")
		} else {
			c.AI = ai
		}
	}
	if c.Channel != "" {
		c.PostChannel = notify.Channel
	}
	rep, err := c.Run(ctx)
	printReport(rep)
	if err != nil {
		log.Fatal(err)
	}
}

func needsMTProto(list []feeds.Source, dry bool) bool {
	for _, s := range list {
		if s.Kind == feeds.KindTGMTProto && (s.Enabled || dry) {
			return true
		}
	}
	return false
}

func printReport(r feeds.Report) {
	b, _ := json.Marshal(r)
	fmt.Fprintln(os.Stderr, "report: "+string(b))
}
