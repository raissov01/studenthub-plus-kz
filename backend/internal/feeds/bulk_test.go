package feeds

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func pendingItem(t *testing.T, n int, deadline string, confidence float64) int64 {
	t.Helper()
	var item int64
	err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		pid, _, err := InsertPost(ctxb(), tx, Post{SourceID: "src-bulk", ExternalID: fmt.Sprint(n), URL: fmt.Sprintf("https://t.me/bulk_chan/%d", n), Text: "Грант"}, "processed", "")
		if err != nil {
			return err
		}
		r := Result{IsRelevant: true, Category: "grant", TitleKK: fmt.Sprintf("Грант %d", n), Deadline: deadline, Funding: "unknown",
			SourceURL: fmt.Sprintf("https://t.me/bulk_chan/%d", n), Confidence: confidence}
		item, err = InsertItem(ctxb(), tx, NewItem{PostID: pid, Source: Source{ID: "src-bulk", Name: "Bulk", Trust: "low"}, Result: r, Status: StatusPending})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctxb(), `update feed_items set confidence = $2 where id = $1`, item, confidence)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// Bulk publish reuses the single approve path: a past deadline is skipped "expired" (stays pending), an item that
// is no longer pending is skipped "not_pending", the filter limits the set, and a second call changes nothing.
func TestBulkPublish(t *testing.T) {
	testdb.Need(t)
	exec(t, `insert into feed_sources (id, kind, handle, name) values ('src-bulk', 'tg_public', 'bulk_chan', 'Bulk') on conflict do nothing`)
	// other tests of the package read every published item: leave nothing behind
	t.Cleanup(func() { exec(t, `delete from feed_sources where id = 'src-bulk'`) })
	actor := q1[int64](t, `insert into users (telegram_id, name, role) values (99001, 'Bulk admin', 'admin') returning id`)
	good := pendingItem(t, 1, "2099-12-31", 0.95)
	low := pendingItem(t, 2, "2099-12-31", 0.5)
	old := pendingItem(t, 3, "2020-01-01", 0.9)
	done := pendingItem(t, 4, "2099-12-31", 0.99)
	exec(t, `update submissions set status = 'approved' where id = (select submission_id from feed_items where id = $1)`, done)

	items, err := matching(ctxb(), BulkFilter{All: true, MinConfidence: 0.8, Source: "src-bulk"})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, it := range items {
		ids[it.ID] = true
	}
	if !ids[good] || !ids[old] || ids[low] || !ids[done] {
		t.Fatalf("filter matched %v", ids)
	}
	// "done" is a stale row: its submission was decided elsewhere meanwhile, so Review answers not_pending
	res, err := Bulk(ctxb(), actor, items, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 1 || len(res.Failed) != 0 || len(res.Skipped) != 2 {
		t.Fatalf("result %+v", res)
	}
	reasons := map[int64]string{}
	for _, s := range res.Skipped {
		reasons[s.ID] = s.Reason
	}
	if reasons[old] != "expired" || reasons[done] != "not_pending" {
		t.Fatalf("skipped %+v", res.Skipped)
	}
	if st := q1[string](t, `select status from feed_items where id = $1`, good); st != StatusPublished {
		t.Fatalf("good item %s", st)
	}
	if by := q1[int64](t, `select decided_by from feed_items where id = $1`, good); by != actor {
		t.Fatalf("decided_by %d", by)
	}
	if st := q1[string](t, `select status from feed_items where id = $1`, old); st != StatusPending {
		t.Fatalf("expired item %s", st)
	}
	// idempotent: the same call again publishes nothing more
	res, err = Bulk(ctxb(), actor, items, true, "")
	if err != nil || res.Published != 0 {
		t.Fatalf("second call %+v %v", res, err)
	}
	// reject needs no deadline check and frees the low-confidence item
	res, err = Bulk(ctxb(), actor, []BulkItem{{ID: low, SubmissionID: q1[int64](t, `select submission_id from feed_items where id = $1`, low)}}, false, "not a grant")
	if err != nil || res.Rejected != 1 {
		t.Fatalf("reject %+v %v", res, err)
	}
	if !(BulkFilter{}).valid() == false || (BulkFilter{IDs: []int64{1}, All: true}).valid() || !(BulkFilter{IDs: []int64{1}}).valid() {
		t.Fatal("filter validation")
	}
}
