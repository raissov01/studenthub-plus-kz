'use client';
/* Spec 06 «Менің өтінімдерім» (/applications): every application in one list — external vacancies the student marked
   with «Өтінім бердім» and in-app applications from the server — grouped by stage, sorted by the next action, each with
   a private follow-up date and note (device-only, lib/applications.ts). The job page's «Өтінім бердім» sheet lives here. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, ConfirmSheet, Empty, Icon, SectionHead } from '@/components/ui';
import { AppStatusBadge, useMyApplications } from '@/components/views/jobs-b2b';
import { almatyYmd } from '@/lib/calendar';
import {
  byNextAction, canMove, editApp, markApplied, mergeApps, NOTE_MAX, sectionOf, STAGE_LABEL, STAGES, type AppRow, type Section, type Stage,
} from '@/lib/applications';
import type { Job } from '@/lib/types';

const STAGE_ICON: Record<Stage, 'send' | 'chat' | 'award' | 'x' | 'back'> = { sent: 'send', interview: 'chat', offer: 'award', rejected: 'x', withdrawn: 'back' };
const STAGE_TONE: Record<Stage, string> = { sent: 'b-sky', interview: 'b-sun', offer: 'b-ok', rejected: 'b-line', withdrawn: 'b-line' };

export function StagePill({ s }: { s: Stage }) {
  const { L } = useApp();
  return <span className={`badge ${STAGE_TONE[s]}`}><Icon n={STAGE_ICON[s]} s={12} /> {L(STAGE_LABEL[s])}</span>;
}

/** local marks + server applications, titles filled from the feed for rows migrated from the old marks */
export function useAppRows(): AppRow[] {
  const { S, content } = useApp();
  const { list } = useMyApplications();
  return mergeApps(S.applications, list).map(r => {
    if (r.title.kk || r.title.ru) return r;
    const [kind, id] = [r.ref.slice(0, r.ref.indexOf(':')), r.ref.slice(r.ref.indexOf(':') + 1)];
    const item = kind === 'job' ? content.jobs.find(j => j.id === id) : content.opportunities.find(o => o.id === id);
    return item ? { ...r, title: item.title, company: r.company || item.org.kk } : { ...r, title: { kk: id, ru: id } };
  });
}

export function ApplicationsScreen() {
  const { tt, L, I, go, openSheet } = useApp();
  const rows = byNextAction(useAppRows());
  const groups: [Section, string][] = [['active', tt('Жүріп жатқан', 'В процессе')], ['offer', tt('Ұсыныс', 'Офферы')], ['closed', tt('Жабылған', 'Закрытые')]];
  if (!rows.length) {
    return (
      <Empty icon="briefcase" action={<button className="btn secondary" type="button" onClick={() => go('opps:jobs')}>{tt('Жұмыс іздеу', 'Найти работу')}</button>}>
        {tt('Әзірге өтінім жоқ. Вакансия бетінде «Өтінім бердім» дегенде, ол осында сатысымен, еске салу күнімен және жазбаңызбен шығады.',
          'Откликов пока нет. Нажмите «Я откликнулся» на странице вакансии — он появится здесь со стадией, датой напоминания и заметкой.')}
      </Empty>
    );
  }
  return (
    <>
      <p className="small muted">{tt('Сыртқы сайттағы және қосымшадағы өтінімдер бір жерде. Жазба мен еске салу күні тек осы құрылғыда сақталады.',
        'Отклики на внешних сайтах и в приложении — в одном месте. Заметка и дата напоминания хранятся только на этом устройстве.')}</p>
      {groups.map(([sec, title]) => {
        const list = rows.filter(r => sectionOf(r.stage) === sec);
        if (!list.length) return null;
        return (
          <section key={sec} className="section" aria-labelledby={'apps-' + sec}>
            <SectionHead title={<span id={'apps-' + sec}>{title}</span>} more={<span className="tiny muted num">{list.length}</span>} />
            <div className="list">{list.map(r => (
              <article key={r.ref} className="card app-row" data-ref={r.ref}>
                <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 8, flexWrap: 'wrap' }}>
                  <button className="link-btn" type="button" style={{ textAlign: 'left', padding: 0, minHeight: 0, overflowWrap: 'anywhere' }} onClick={() => go(r.ref)}>{L(r.title)}</button>
                  {r.serverStatus ? <AppStatusBadge s={r.serverStatus} /> : <StagePill s={r.stage} />}
                </div>
                <span className="small muted" style={{ overflowWrap: 'anywhere' }}>
                  {[r.company, r.appliedAt ? tt(`жіберілді ${I.fDate(new Date(r.appliedAt))}`, `отправлено ${I.fDate(new Date(r.appliedAt))}`) : ''].filter(Boolean).join(' · ')}
                </span>
                {r.followUp ? <span className="small"><Icon n="bell" s={14} /> {tt(`Келесі қадам: ${I.fDate(new Date(r.followUp + 'T00:00:00'))} — хабарласу`, `Следующий шаг: ${I.fDate(new Date(r.followUp + 'T00:00:00'))} — связаться`)}</span> : null}
                {r.note ? <p className="small" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere', margin: 0 }}>{r.note}</p> : null}
                <button className="btn ghost" type="button" onClick={() => openSheet(<AppSheet row={r} />)}><Icon n="edit" s={18} /> {tt('Өзгерту', 'Изменить')}</button>
              </article>
            ))}</div>
          </section>
        );
      })}
    </>
  );
}

/** stage, follow-up date and note of one row; an in-app row's stage is the server's */
function AppSheet({ row }: { row: AppRow }) {
  const { tt, L, update, closeSheet, closeSheetDom, openSheet, toast } = useApp();
  const [stage, setStage] = useState<Stage>(row.stage);
  const [followUp, setFollowUp] = useState(row.followUp);
  const [note, setNote] = useState(row.note);
  const local = canMove(row);
  const save = () => {
    update(s => { s.applications = editApp(s.applications, row, { stage, followUp, note }); });
    closeSheet();
    toast(tt('Сақталды', 'Сохранено'));
  };
  const remove = () => openSheet(<ConfirmSheet msg={tt('Өтінімді тізімнен алып тастау керек пе?', 'Убрать отклик из списка?')} yesLabel={tt('Иә, алып тастау', 'Да, убрать')}
    yes={() => {
      update(s => {
        s.applications = s.applications.filter(a => a.ref !== row.ref);
        if (row.ref.startsWith('job:')) delete s.applied[row.ref.slice(4)];
      });
      toast(tt('Тізімнен алынды', 'Убрано из списка'));
    }} />);
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); save(); }}>
      <h2 style={{ overflowWrap: 'anywhere' }}>{L(row.title)}</h2>
      {local ? (
        <fieldset className="field">
          <legend>{tt('Сатысы', 'Стадия')}</legend>
          <div className="chips wrap" id="appStage">{STAGES.map(s => <Chip key={s} pressed={stage === s} onClick={() => setStage(s)}>{L(STAGE_LABEL[s])}</Chip>)}</div>
        </fieldset>
      ) : (
        <p className="small">{tt('Сатысын жұмыс беруші жаңартады:', 'Стадию обновляет работодатель:')} {row.serverStatus ? <AppStatusBadge s={row.serverStatus} /> : null}</p>
      )}
      <div className="field">
        <label htmlFor="appFollow">{tt('Қашан хабарласу керек', 'Когда связаться')}</label>
        <input className="input" id="appFollow" type="date" value={followUp} onChange={e => setFollowUp(e.target.value)} />
        <span className="tiny muted">{tt('Бұл күн күнтізбеде көрінеді.', 'Эта дата появится в календаре.')}</span>
      </div>
      <div className="field">
        <label htmlFor="appNote">{tt('Жазба', 'Заметка')}</label>
        <textarea className="input" id="appNote" rows={3} maxLength={NOTE_MAX} value={note} onChange={e => setNote(e.target.value)}
          placeholder={tt('мысалы: HR Айгүл, 3 күннен кейін хабарласу', 'например: HR Айгуль, написать через 3 дня')} />
        <span className="tiny muted num">{note.length}/{NOTE_MAX} · {tt('тек осы құрылғыда', 'только на этом устройстве')}</span>
      </div>
      <div className="btn-row">
        <button className="btn" type="submit">{tt('Сақтау', 'Сохранить')}</button>
        <button className="btn ghost" type="button" onClick={closeSheetDom}>{tt('Болдырмау', 'Отмена')}</button>
      </div>
      {local ? <button className="btn danger" type="button" onClick={remove}><Icon n="trash" s={18} /> {tt('Тізімнен алып тастау', 'Убрать из списка')}</button> : null}
    </form>
  );
}

/** job page «Өтінім бердім» (spec 06 P2): the date (today) and an optional note, then the job joins the list */
export function MarkAppliedSheet({ j }: { j: Job }) {
  const { tt, L, update, closeSheet, toast, go } = useApp();
  const [today] = useState(() => almatyYmd(Date.now()));
  const [date, setDate] = useState(today);
  const [note, setNote] = useState('');
  const save = () => {
    const at = date && date !== today ? Date.parse(date + 'T12:00:00+05:00') : Date.now();
    update(s => {
      s.applications = markApplied(s.applications, { ref: 'job:' + j.id, title: j.title, company: L(j.org) }, at, { note });
      s.applied[j.id] = at;
    });
    closeSheet();
    toast(tt('Өтінімдер тізіміне қосылды', 'Добавлено в мои отклики'), { label: tt('Ашу', 'Открыть'), run: () => go('applications') });
  };
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); save(); }}>
      <h2>{tt('Өтінім бердім', 'Я откликнулся')}</h2>
      <p className="small muted" style={{ overflowWrap: 'anywhere' }}>{L(j.title)} · {L(j.org)}</p>
      <div className="field">
        <label htmlFor="applyDate">{tt('Қашан', 'Когда')}</label>
        <input className="input" id="applyDate" type="date" value={date} max={today} onChange={e => setDate(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="applyNote">{tt('Жазба (міндетті емес)', 'Заметка (необязательно)')}</label>
        <textarea className="input" id="applyNote" rows={2} maxLength={NOTE_MAX} value={note} onChange={e => setNote(e.target.value)} />
      </div>
      <button className="btn" type="submit"><Icon n="check" s={18} /> {tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}
