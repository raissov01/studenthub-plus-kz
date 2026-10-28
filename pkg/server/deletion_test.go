package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/market"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

type userReference struct {
	Schema, Table, Column, Rule string
	Keys                        []string
	Rows                        []json.RawMessage
}

// Discover actual schema references, not a hand-maintained feature/table list. Snapshot
// primary keys so we prove populated rows were removed or anonymized, not just FK syntax.
func populatedUserReferences(t *testing.T, uid int64) []userReference {
	t.Helper()
	ctx := context.Background()
	refs := []userReference{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select ns.nspname, cl.relname, a.attname, c.confdeltype::text,
   array(select pa.attname::text from pg_index i join pg_attribute pa on pa.attrelid=i.indrelid and pa.attnum=any(i.indkey)
    where i.indrelid=cl.oid and i.indisprimary order by pa.attnum)
   from pg_constraint c join pg_class cl on cl.oid=c.conrelid join pg_namespace ns on ns.oid=cl.relnamespace
   join pg_attribute a on a.attrelid=c.conrelid and a.attnum=any(c.conkey)
   where c.contype='f' and c.confrelid='public.users'::regclass order by cl.relname,a.attname`)
		if err != nil {
			return err
		}
		refs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (userReference, error) {
			var r userReference
			err := row.Scan(&r.Schema, &r.Table, &r.Column, &r.Rule, &r.Keys)
			return r, err
		})
		if err != nil {
			return err
		}
		for i := range refs {
			r := &refs[i]
			if r.Rule != "c" && r.Rule != "n" {
				return fmt.Errorf("%s.%s: missing deletion rule", r.Table, r.Column)
			}
			if len(r.Keys) == 0 {
				return fmt.Errorf("%s has no primary key for deletion proof", r.Table)
			}
			rows, err := tx.Query(ctx, "select to_jsonb(t) from "+pgx.Identifier{r.Schema, r.Table}.Sanitize()+" t where "+pgx.Identifier{r.Column}.Sanitize()+"=$1", uid)
			if err != nil {
				return err
			}
			r.Rows, err = pgx.CollectRows(rows, pgx.RowTo[json.RawMessage])
			if err != nil {
				return err
			}
			if len(r.Rows) == 0 {
				return fmt.Errorf("missing populated deletion fixture for %s.%s (new schema reference?)", r.Table, r.Column)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func TestAccountDeletionPopulatesEveryUserForeignKeyAndDeletesFiles(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	uid, other, third := launchUser(t, "student"), launchUser(t, "student"), launchUser(t, "student")
	t.Setenv("UPLOAD_DIR", t.TempDir())
	if err := os.MkdirAll(market.UploadDir(), 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{strings.Repeat("a", 32) + ".jpg", strings.Repeat("b", 32) + ".jpg", strings.Repeat("c", 32) + ".jpg", strings.Repeat("d", 32) + ".jpg"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(market.UploadDir(), n), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	var company int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := tx.Exec(ctx, q, args...); err != nil {
				t.Fatalf("seed %s: %v", q, err)
			}
		}
		id := func(q string, args ...any) int64 {
			t.Helper()
			var n int64
			if err := tx.QueryRow(ctx, q, args...).Scan(&n); err != nil {
				t.Fatalf("seed %s: %v", q, err)
			}
			return n
		}
		exec(`insert into reminders(user_id,opportunity_id,title,deadline) values ($1,'launch','fixture',current_date+7),($2,'launch','control',current_date+7)`, uid, other)
		exec(`insert into bot_daily_sends(user_id,day) values ($1,current_date)`, uid)
		exec(`insert into bot_events(kind,user_id) values ('launch',$1)`, uid)
		exec(`insert into audit_log(actor_id,target_id,action) values ($1,$1,'launch')`, uid)
		event := id(`insert into events(title_kk,cat,starts_at,ends_at,university,created_by) values ('fixture','other',now(),now()+interval '1 hour','LAUNCH',$1) returning id`, uid)
		exec(`insert into event_regs(event_id,user_id,ticket) values ($1,$2,'launch-delete-ticket')`, event, uid)
		exec(`insert into event_certificates(event_id,user_id) values ($1,$2)`, event, uid)
		exec(`insert into submissions(kind,payload,submitted_by,reviewed_by) values ('opportunity','{}',$1,$1),('opportunity','{}',$2,$1)`, uid, other)
		exec(`insert into reports(target_kind,target_id,reporter_id,reason) values ('event','launch',$1,'fixture')`, uid)
		exec(`insert into ambassador_applications(user_id,university,motivation,hours_per_week,reviewed_by)
    values ($1,'LAUNCH','fixture motivation','1-2',$1),($2,'LAUNCH','fixture motivation','1-2',$1)`, uid, other)
		exec(`insert into ambassador_terms(user_id,university,term_start,term_end,hours_per_week,ref_code,cert_issued_by)
    values ($1,'LAUNCH',current_date,current_date+30,2,'launchdel1',$1),($2,'LAUNCH',current_date,current_date+30,2,'launchdel2',$1)`, uid, other)
		exec(`insert into community_content(kind,payload,author_id) values ('opportunity','{}',$1)`, uid)
		exec(`insert into mentors(user_id,university,areas) values ($1,'LAUNCH','{study}')`, uid)
		req := id(`insert into mentor_requests(user_id,university,need) values ($1,'LAUNCH','study') returning id`, uid)
		pair := id(`insert into mentor_pairs(mentor_id,mentee_id,request_id,matched_by,ended_by,university,need,ended_at)
    values ($1,$2,$3,$1,$1,'LAUNCH','study',now()) returning id`, uid, other, req)
		exec(`insert into mentor_pairs(mentor_id,mentee_id,matched_by,ended_by,university,need,ended_at)
    values ($2,$1,$1,$1,'LAUNCH','study',now()),($2,$3,$1,$1,'LAUNCH','study',now())`, uid, other, third)
		exec(`insert into mentor_pair_prompts(pair_id,user_id,kind) values ($1,$2,'check3')`, pair, uid)
		exec(`insert into user_badges(user_id,badge) values ($1,'launch')`, uid)
		item := id(`insert into market_items(user_id,title,category,condition,university,photos) values ($1,'fixture','books','good','LAUNCH',$2) returning id`, uid, names[:2])
		exec(`insert into market_photos(name,user_id,item_id,bytes) values ($1,$2,$3,7)`, names[0], uid, item)
		// names[1] is legacy array-only storage; names[2] is an unattached upload.
		exec(`insert into market_photos(name,user_id,bytes) values ($1,$2,7),($3,$4,7)`, names[2], uid, names[3], other)
		exec(`insert into market_daily(user_id,day) values ($1,current_date)`, uid)
		exec(`insert into market_rules(user_id) values ($1)`, uid)
		exec(`insert into market_bans(user_id,until,reason,created_by) values ($1,now()+interval '1 day','fixture',$1),($2,now()+interval '1 day','fixture',$1)`, uid, other)
		exec(`insert into market_season_sends(user_id,season) values ($1,'launch')`, uid)
		exec(`insert into portfolio_settings(user_id,slug) values ($1,'launchuser')`, uid)
		exec(`insert into portfolio_items(user_id,kind,title) values ($1,'skill','fixture')`, uid)
		company = id(`insert into employers(owner_user_id,name,bin,website,contact_name,verified_by,status)
    values ($1,'Launch company','123456789012','https://example.org','Company contact',$1,'verified') returning id`, uid)
		exec(`insert into employer_members(employer_id,user_id,role,added_by) values ($1,$2,'owner',$2),($1,$3,'member',$2)`, company, uid, other)
		exec(`insert into employer_invites(code,employer_id,created_by,used_by,expires_at) values ('launchinvite',$1,$2,$2,now()+interval '1 day')`, company, uid)
		job := id(`insert into jobs(employer_id,created_by,title_kk,desc_kk,city,remote,pay,per,sched_kk,apply_kk,minor_ok)
    values ($1,$2,'Fixture job','A long enough fixture description','astana',true,100,'hour','flexible','Apply here',true) returning id`, company, uid)
		exec(`insert into applications(job_id,employer_id,user_id,shared_fields) values ($1,$2,$3,'{"name":"private"}')`, job, company, uid)
		exec(`insert into university_staff(user_id,university,granted_by) values ($1,'LAUNCH',$1),($2,'LAUNCH',$1)`, uid, other)
		exec(`insert into university_posts(university,author_id,kind,payload) values ('LAUNCH',$1,'announcement','{}')`, uid)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := populatedUserReferences(t, uid)
	if len(refs) < 40 {
		t.Fatalf("unexpectedly incomplete catalog scan: %d", len(refs))
	}
	w := launchRequest(t, "DELETE", "/api/me", `{"confirm":"DELETE"}`, uid)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if launchCount(t, `select count(*) from users where id=$1`, uid) != 0 {
		t.Fatal("user survived")
	}
	for _, ref := range refs {
		table := pgx.Identifier{ref.Schema, ref.Table}.Sanitize()
		column := pgx.Identifier{ref.Column}.Sanitize()
		if launchCount(t, "select count(*) from "+table+" where "+column+"=$1", uid) != 0 {
			t.Errorf("%s.%s retained deleted id", ref.Table, ref.Column)
		}
		survivors := 0
		for _, raw := range ref.Rows {
			var full map[string]json.RawMessage
			if err := json.Unmarshal(raw, &full); err != nil {
				t.Fatal(err)
			}
			key := map[string]json.RawMessage{}
			for _, k := range ref.Keys {
				key[k] = full[k]
			}
			pk, _ := json.Marshal(key)
			exists := launchCount(t, "select count(*) from "+table+" t where to_jsonb(t) @> $1::jsonb", string(pk))
			if ref.Rule == "c" && exists != 0 {
				t.Errorf("%s.%s cascade row survived: %s", ref.Table, ref.Column, pk)
			}
			if ref.Rule == "n" && exists != 0 {
				survivors++
				if ref.Table == "employers" && ref.Column == "owner_user_id" {
					continue
				} // intentional ownership hand-over
				if launchCount(t, "select count(*) from "+table+" t where to_jsonb(t) @> $1::jsonb and "+column+" is null", string(pk)) != 1 {
					t.Errorf("%s.%s surviving row not anonymized", ref.Table, ref.Column)
				}
			}
		}
		if ref.Rule == "n" && survivors == 0 {
			t.Errorf("%s.%s: no surviving fixture proves SET NULL", ref.Table, ref.Column)
		}
	}
	for _, name := range names[:3] {
		if _, err := os.Stat(filepath.Join(market.UploadDir(), name)); !os.IsNotExist(err) {
			t.Errorf("deleted user's file survived: %s %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(market.UploadDir(), names[3])); err != nil {
		t.Fatal("another user's file was deleted", err)
	}
	if launchCount(t, `select count(*) from employers where id=$1 and owner_user_id=$2 and status='verified'`, company, other) != 1 {
		t.Fatal("company not transferred to remaining member")
	}
	if launchCount(t, `select count(*) from employer_members where employer_id=$1 and user_id=$2 and role='owner'`, company, other) != 1 {
		t.Fatal("membership not transferred")
	}
	if launchCount(t, `select count(*) from reminders where user_id=$1`, other) != 1 {
		t.Fatal("another user's data removed")
	}
	if w := launchRequest(t, "GET", "/api/me/export", "", uid); w.Code != 401 {
		t.Fatal("deleted user's session survived", w.Code)
	}
	t.Logf("Proved deletion/anonymization of populated rows across %d user foreign keys", len(refs))
}

func TestAccountDeletionRefusesPhotoFailureAndClosesOwnerlessCompany(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	uid := launchUser(t, "student")
	ctx := context.Background()
	t.Setenv("UPLOAD_DIR", t.TempDir())
	name := strings.Repeat("e", 32) + ".jpg"
	// A nonempty directory at the file path deterministically produces a removal error,
	// including when tests run as root (chmod-based tests do not).
	path := filepath.Join(market.UploadDir(), name)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocked"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	launchExec(t, `insert into market_photos(name,user_id,bytes) values ($1,$2,1)`, name, uid)
	if err := me.Delete(ctx, uid); err == nil {
		t.Fatal("deletion ignored file failure")
	}
	if launchCount(t, `select count(*) from users where id=$1`, uid) != 1 {
		t.Fatal("failed deletion committed")
	}
	if err := os.Remove(filepath.Join(path, "blocked")); err != nil {
		t.Fatal(err)
	}
	var company int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into employers(owner_user_id,name,bin,website,contact_name,status) values ($1,'Ownerless','987654321012','https://example.org','Contact','verified') returning id`, uid).Scan(&company); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into employer_members(employer_id,user_id,role) values ($1,$2,'owner')`, company, uid)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := me.Delete(ctx, uid); err != nil {
		t.Fatal("retry failed", err)
	}
	if launchCount(t, `select count(*) from employers where id=$1 and owner_user_id is null and status='closed'`, company) != 1 {
		t.Fatal("ownerless company not closed")
	}
}
