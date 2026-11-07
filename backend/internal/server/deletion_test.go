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
	"github.com/raissov01/studenthub-plus-kz/backend/internal/community"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/market"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/portfolio"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/quiz"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
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
	lfFile := filepath.Join(community.PhotoDir(), strings.Repeat("e", 32)+".jpg")
	if err := os.MkdirAll(community.PhotoDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lfFile, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	quizFile := strings.Repeat("f", 32) + ".jpg"
	if err := os.MkdirAll(quiz.UploadDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quiz.UploadDir(), quizFile), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	certFile := strings.Repeat("e", 32) + ".pdf"
	if err := os.MkdirAll(portfolio.CertDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(portfolio.CertDir(), certFile), []byte("%PDF-1.4 fixture"), 0600); err != nil {
		t.Fatal(err)
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
		exec(`insert into portfolio_certificates(user_id,title,file_name,mime,bytes) values ($1,'fixture',$2,'application/pdf',16)`, uid, certFile)
		company = id(`insert into employers(owner_user_id,name,bin,website,contact_name,verified_by,status)
    values ($1,'Launch company','123456789012','https://example.org','Company contact',$1,'verified') returning id`, uid)
		exec(`insert into employer_members(employer_id,user_id,role,added_by) values ($1,$2,'owner',$2),($1,$3,'member',$2)`, company, uid, other)
		exec(`insert into employer_invites(code,employer_id,created_by,used_by,expires_at) values ('launchinvite',$1,$2,$2,now()+interval '1 day')`, company, uid)
		job := id(`insert into jobs(employer_id,created_by,title_kk,desc_kk,city,remote,pay,per,sched_kk,apply_kk,minor_ok)
    values ($1,$2,'Fixture job','A long enough fixture description','astana',true,100,'hour','flexible','Apply here',true) returning id`, company, uid)
		exec(`insert into applications(job_id,employer_id,user_id,shared_fields) values ($1,$2,$3,'{"name":"private"}')`, job, company, uid)
		exec(`insert into university_staff(user_id,university,granted_by) values ($1,'LAUNCH',$1),($2,'LAUNCH',$1)`, uid, other)
		exec(`insert into university_posts(university,author_id,kind,payload) values ('LAUNCH',$1,'announcement','{}')`, uid)
		exec(`insert into admin_content(kind,record,status,created_by,updated_by) values ('jobs','{}','hidden',$1,$1)`, uid)
		exec(`insert into feed_sources(id,kind,handle,name) values ('del-fixture','tg_public','del_fixture','Fixture')`)
		feedPost := id(`insert into feed_posts(source_id,external_id,url,text) values ('del-fixture','1','https://t.me/del_fixture/1','fixture') returning id`)
		exec(`insert into feed_items(post_id,source_id,status,category,data,title_norm,decided_by) values ($1,'del-fixture','rejected','other','{}','fixture',$2)`, feedPost, uid)
		exec(`insert into notify_prefs(user_id) values ($1)`, uid)
		exec(`insert into notify_outbox(user_id,category,text,not_before) values ($1,'events','fixture',now())`, uid)
		exec(`insert into notify_sends(user_id,category) values ($1,'events')`, uid)
		exec(`insert into notifications(user_id,category,text,link) values ($1,'events','fixture','/event/1'),($2,'events','control',null)`, uid, other)
		exec(`insert into push_subscriptions(user_id,endpoint,p256dh,auth) values ($1,'https://push.example/launch',repeat('a',87),repeat('b',22))`, uid)
		exec(`insert into mentor_meeting_notices(event_id,user_id) values ($1,$2)`, event, uid)
		exec(`insert into discount_checks(discount_id,user_id,worked) values ('d1',$1,true)`, uid)
		exec(`insert into revoked_sessions(sid,user_id,expires_at) values ('fixture-sid-1',$1,now()+interval '1 day')`, uid)
		exec(`update users set email='launch-delete@example.kz', email_verified_at=now(), google_sub='launch-delete-google-sub' where id=$1`, uid)
		exec(`insert into user_passwords(user_id,hash) values ($1,'$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA')`, uid)
		exec(`insert into auth_codes(user_id,purpose,email,code_hash,expires_at) values ($1,'reset_password','launch-delete@example.kz','\x00',now()+interval '15 minutes')`, uid)
		qz := id(`insert into quizzes(user_id,title,share_code) values ($1,'fixture','launchquiz') returning id`, uid)
		exec(`insert into quiz_attempts(quiz_id,user_id,score,max_score,percentage,correct_count,total) values ($1,$2,1,1,100,1,1)`, qz, uid)
		exec(`insert into quiz_media(name,user_id,quiz_id,kind,bytes) values ($1,$2,$3,'image',7)`, quizFile, uid, qz)
		exec(`insert into quiz_ai_usage(user_id,day,units) values ($1,current_date,1)`, uid)
		live := id(`insert into quiz_live_sessions(code,quiz_id,host_id,status) values ('123456',$1,$2,'finished') returning id`, qz, uid)
		exec(`insert into quiz_live_results(session_id,rank,nickname,user_id,score,correct) values ($1,1,'fixture',$2,10,1)`, live, uid)
		exec(`insert into points_wallets(user_id,coins,xp) values ($1,5,10),($2,5,0)`, uid, other)
		exec(`insert into xp_events(user_id,delta,reason,ref_type,ref_id,day) values ($1,10,'quiz','quiz','1',current_date)`, uid)
		exec(`insert into coin_ledger(user_id,delta,reason,ref_type,ref_id,actor_id,note,day)
    values ($1,5,'admin_adjust','admin_adjust','delete-fixture-1',$1,'fixture',current_date),($2,5,'admin_adjust','admin_adjust','delete-fixture-2',$1,'fixture',current_date)`, uid, other)
		exec(`insert into event_exhibitors(event_id,employer_id,booth,qr_secret,added_by) values ($1,$2,'A1',decode('00112233445566778899aabbccddeeff','hex'),$3)`, event, company, uid)
		exec(`insert into booth_checkins(event_id,employer_id,user_id) values ($1,$2,$3)`, event, company, uid)
		reward := id(`insert into rewards(kind,title_kk,title_ru,cost,created_by) values ('merch','Fixture','Fixture',5,$1) returning id`, uid)
		exec(`insert into reward_redemptions(user_id,reward_id,cost,idem_key,status,decided_by) values ($1,$3,5,'deletefixture1','approved',$1),($2,$3,5,'deletefixture2','approved',$1)`, uid, other, reward)
		cafe := id(`insert into cafes(name_kk,name_ru,created_by) values ('Fixture café','Fixture café',$1) returning id`, uid)
		exec(`insert into cafe_staff(cafe_id,user_id,role,added_by) values ($1,$2,'manager',$2),($1,$3,'barista',$2)`, cafe, uid, other)
		order := id(`insert into cafe_orders(cafe_id,user_id,items,total,pickup_at,pickup_code,payment_method,idem_key)
    values ($1,$2,'[{"itemId":1}]',50000,now(),'123','pickup','deletefixture1') returning id`, cafe, uid)
		exec(`insert into cafe_order_events(order_id,actor_id,actor_kind,kind) values ($1,$2,'student','created')`, order, uid)
		exec(`insert into cafe_kaspi_sessions(cafe_id,ciphertext,phone_hash,created_by) values ($1,'v1:x:y',repeat('a',64),$2)`, cafe, uid)
		// print shop: staff, a job with its file and audit, the shop's Kaspi session
		shop := id(`insert into print_shops(name_kk,name_ru,created_by) values ('Fixture print','Fixture print',$1) returning id`, uid)
		exec(`insert into print_staff(shop_id,user_id,role,added_by) values ($1,$2,'manager',$2),($1,$3,'operator',$2)`, shop, uid, other)
		pjob := id(`insert into print_jobs(shop_id,user_id,lines,total,pickup_code,payment_method,idem_key)
    values ($1,$2,'[{"kind":"goods"}]',30000,'123','pickup','deletefixture1') returning id`, shop, uid)
		exec(`insert into print_files(user_id,job_id,file_name,display_name,kind,bytes,sha256) values ($1,$2,$3,'fixture.pdf','pdf',10,repeat('a',64))`, uid, pjob, strings.Repeat("f", 32)+".pdf")
		exec(`insert into print_job_events(job_id,actor_id,actor_kind,kind) values ($1,$2,'student','created')`, pjob, uid)
		exec(`insert into print_kaspi_sessions(shop_id,ciphertext,phone_hash,created_by) values ($1,'v1:x:y',repeat('a',64),$2)`, shop, uid)
		// university voice: surveys, feedback tickets, wellbeing contacts
		survey := id(`insert into surveys(university,title,questions,marker_salt,created_by,anonymous) values ('LAUNCH','Fixture survey','[]',decode(repeat('ab',32),'hex'),$1,false) returning id`, uid)
		exec(`with r as (insert into survey_responses(survey_id,answers) values ($1,'{}') returning id) insert into survey_response_owners(response_id,user_id) select id,$2 from r`, survey, uid)
		rec := id(`insert into feedback_recipients(university,key,name_kk,name_ru) values ('LAUNCH','dean','Деканат','Деканат') returning id`)
		exec(`insert into feedback_recipient_staff(recipient_id,user_id) values ($1,$2)`, rec, uid)
		exec(`insert into feedback_tickets(recipient_id,category,body,anonymous,author_id,sla_due_at,created_at,updated_at) values ($1,'question','Fixture signed ticket',false,$2,now(),now(),now())`, rec, uid)
		anon := id(`insert into feedback_tickets(recipient_id,category,body,anonymous,token_hash,sla_due_at,created_at,updated_at) values ($1,'question','Fixture anonymous ticket',true,decode(repeat('cd',32),'hex'),now(),now(),now()) returning id`, rec)
		exec(`insert into feedback_messages(ticket_id,from_staff,staff_id,body,created_at) values ($1,true,$2,'Fixture reply',now())`, anon, uid)
		exec(`insert into wellbeing_contacts(university,name,place,updated_by) values ('LAUNCH','Fixture psychologist','Room 1',$1)`, uid)
		// community: lost & found, course Q&A, roommates
		lf := id(`insert into lostfound_posts(user_id,kind,category,title,happened_on,university) values ($1,'found','keys','Fixture keys',current_date,'LAUNCH') returning id`, other)
		exec(`insert into lostfound_posts(user_id,kind,category,title,happened_on,university) values ($1,'lost','bag','Fixture bag',current_date,'LAUNCH')`, uid)
		exec(`insert into lostfound_claims(post_id,claimant_id,message) values ($1,$2,'Fixture claim message')`, lf, uid)
		exec(`insert into lostfound_photos(name,user_id,bytes) values ($1,$2,7)`, strings.Repeat("e", 32)+".jpg", uid)
		q := id(`insert into qa_questions(user_id,university,subject_key,subject_name,title) values ($1,'LAUNCH','fixture','Fixture','Fixture question?') returning id`, other)
		exec(`insert into qa_answers(question_id,user_id,body) values ($1,$2,'Fixture answer')`, q, uid)
		exec(`insert into qa_questions(user_id,university,subject_key,subject_name,title,anonymous) values ($1,'LAUNCH','fixture','Fixture','My fixture question?',true)`, uid)
		exec(`insert into qa_votes(user_id,target_kind,target_id) values ($1,'question',$2)`, uid, q)
		exec(`insert into roommate_genders(user_id,gender) values ($1,'f')`, uid)
		rl := id(`insert into roommate_listings(user_id,kind,housing,district,budget_min,budget_max,move_in,university) values ($1,'have_room','flat','Fixture',0,50000,current_date,'LAUNCH') returning id`, other)
		exec(`insert into roommate_listings(user_id,kind,housing,district,budget_min,budget_max,move_in,university) values ($1,'looking','flat','Fixture',0,50000,current_date,'LAUNCH')`, uid)
		exec(`insert into roommate_requests(listing_id,from_user) values ($1,$2)`, rl, uid)
		club := id(`insert into clubs(name_kk,university,created_by) values ('Fixture club','LAUNCH',$1) returning id`, uid)
		exec(`insert into club_admins(club_id,user_id,granted_by) values ($1,$2,$2),($1,$3,$2)`, club, uid, other)
		exec(`insert into club_members(club_id,user_id,status,decided_by) values ($1,$2,'member',$2),($1,$3,'member',$2)`, club, uid, other)
		exec(`insert into club_posts(club_id,author_id,body_kk,status) values ($1,$2,'fixture','published')`, club, uid)
		// admin console: a scoped role granted to and by the user, a block, a day of activity
		exec(`insert into admin_grants(user_id,scope,granted_by,reason) values ($1,'moderator',$2,'fixture grant'),($2,'moderator',$1,'fixture grant')`, uid, other)
		exec(`insert into user_blocks(user_id,reason,blocked_by) values ($1,'fixture block',$2),($2,'fixture block',$1)`, uid, other)
		exec(`insert into user_active_days(user_id,day) values ($1,current_date)`, uid)
		// admin workspace: notes about and by the user, a saved filter, the support settings, an email invite
		exec(`insert into admin_user_notes(user_id,author_id,body) values ($1,$2,'fixture note'),($2,$1,'fixture note by')`, uid, other)
		exec(`insert into admin_saved_views(owner_id,screen,name,query) values ($1,'users','fixture','role=admin')`, uid)
		exec(`insert into admin_settings(key,value,updated_by) values ('support','{}',$1) on conflict (key) do update set updated_by = excluded.updated_by`, uid)
		exec(`insert into admin_role_invites(email,grant_kind,reason,invited_by) values ('fixture.invite@example.kz','moderator','fixture',$1)`, uid)
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
	if launchCount(t, `select count(*) from users where google_sub='launch-delete-google-sub'`) != 0 {
		t.Fatal("linked Google account survived")
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
	if _, err := os.Stat(lfFile); !os.IsNotExist(err) {
		t.Errorf("deleted user's lost & found photo survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(portfolio.CertDir(), certFile)); !os.IsNotExist(err) {
		t.Errorf("deleted user's certificate file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(quiz.UploadDir(), quizFile)); !os.IsNotExist(err) {
		t.Errorf("deleted user's quiz media survived: %v", err)
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
