'use client';
/* «Сабақ кестесі» (/timetable): the student enters their own weekly timetable — subject, weekday, time, room — and it
   stays on this device (AppState.timetable). It feeds the home «Келесі сабақ» card, the calendar «Сабақтар» layer and
   the «Пәндер» subjects. Pure helpers: lib/timetable.ts. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Empty, Icon, SectionHead } from '@/components/ui';
import { ScheduleIcsButton } from '@/components/views/home';
import { KIND_MAX, makeEntry, NAME_MAX, ROOM_MAX, sortTimetable, weekdayOf } from '@/lib/timetable';
import type { TimetableEntry } from '@/lib/types';

const DAYS = [0, 1, 2, 3, 4, 5, 6];

export function TimetableScreen() {
  const { S, tt, wd, go, openSheet, update, toast } = useApp();
  const [edit, setEdit] = useState<TimetableEntry | null>(null);
  const list = S.timetable;
  const dayName = (d: number) => wd[(d + 1) % 7];
  const del = (e: TimetableEntry) => openSheet(<ConfirmSheet msg={tt(`«${e.name}» сабағын кестеден өшіру керек пе?`, `Удалить «${e.name}» из расписания?`)} yesLabel={tt('Өшіру', 'Удалить')}
    yes={() => { update(s => { s.timetable = s.timetable.filter(x => x.id !== e.id); }); if (edit?.id === e.id) setEdit(null); toast(tt('Сабақ өшірілді', 'Пара удалена')); }} />);
  return (
    <>
      {list.length ? (
        <div id="timetableList">
          {DAYS.filter(d => list.some(e => e.wd === d)).map(d => (
            <section key={d} className="section" aria-label={dayName(d)}>
              <SectionHead title={dayName(d)} />
              <ul className="group">
                {list.filter(e => e.wd === d).map(e => (
                  <li key={e.id} className="row tt-row">
                    <span className="up-when"><b className="num">{e.t}</b><span className="num">{e.end}</span></span>
                    <span className="row-main"><span className="row-title">{e.name}</span>
                      {e.kind || e.room ? <span className="row-sub">{[e.kind, e.room ? tt('аудитория', 'аудитория') + ' ' + e.room : ''].filter(Boolean).join(', ')}</span> : null}</span>
                    <button className="icon-btn" type="button" onClick={() => setEdit(e)} aria-label={tt('Өзгерту: ', 'Изменить: ') + e.name}><Icon n="edit" s={20} /></button>
                    <button className="icon-btn" type="button" onClick={() => del(e)} aria-label={tt('Өшіру: ', 'Удалить: ') + e.name}><Icon n="trash" s={20} /></button>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      ) : (
        <Empty icon="timetable">{tt('Кестеңіз әзірге бос. Төменнен әр сабақты бір рет қосыңыз — ол әр апта қайталанады.', 'Расписание пока пустое. Добавьте каждую пару один раз ниже — она будет повторяться каждую неделю.')}</Empty>
      )}
      <TimetableForm key={edit?.id ?? 'new'} edit={edit} onDone={() => setEdit(null)} />
      {list.length ? (
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => go('calendar')}><Icon n="calendar" s={18} /> {tt('Күнтізбеде көру', 'Посмотреть в календаре')}</button>
          <button className="btn ghost" type="button" onClick={() => go('subjects')}><Icon n="folder" s={18} /> {tt('Пәндер', 'Предметы')}</button>
        </div>
      ) : null}
      <ScheduleIcsButton />
      <p className="tiny muted">{tt('Кесте тек осы құрылғыда сақталады. Университеттің ресми кестесі қосылса, ол автоматты түрде шығады.', 'Расписание хранится только на этом устройстве. Когда подключат официальное расписание университета, оно появится автоматически.')}</p>
    </>
  );
}

function TimetableForm({ edit, onDone }: { edit: TimetableEntry | null; onDone: () => void }) {
  const { tt, wd, update, toast } = useApp();
  const [err, setErr] = useState('');
  return (
    <form className="card form" id="timetableForm" noValidate onSubmit={e => {
      e.preventDefault();
      const f = e.currentTarget;
      const v = (id: string) => f.querySelector<HTMLInputElement | HTMLSelectElement>('#' + id)?.value ?? '';
      const entry = makeEntry({ name: v('ttName'), wd: Number(v('ttDay')), t: v('ttStart'), end: v('ttEnd'), room: v('ttRoom'), kind: v('ttKind') },
        edit?.id ?? 'tt' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6));
      if (entry === 'name') { setErr(tt('Пән атауын жазыңыз.', 'Укажите название предмета.')); f.querySelector<HTMLInputElement>('#ttName')?.focus(); return; }
      if (entry === 'time') { setErr(tt('Басталу уақытын көрсетіңіз; аяқталуы одан кейін болуы керек.', 'Укажите время начала; окончание должно быть позже.')); f.querySelector<HTMLInputElement>('#ttStart')?.focus(); return; }
      setErr('');
      update(s => { s.timetable = sortTimetable([...s.timetable.filter(x => x.id !== entry.id), entry]); });
      toast(edit ? tt('Сабақ сақталды', 'Пара сохранена') : tt('Сабақ кестеге қосылды', 'Пара добавлена в расписание'));
      if (edit) onDone();
      else { const day = v('ttDay'); f.reset(); const sel = f.querySelector<HTMLSelectElement>('#ttDay'); if (sel) sel.value = day; }
    }}>
      <h2>{edit ? tt('Сабақты өзгерту', 'Изменить пару') : tt('Сабақ қосу', 'Добавить пару')}</h2>
      <div className="field"><label htmlFor="ttName">{tt('Пән', 'Предмет')}</label>
        <input className="input" id="ttName" required maxLength={NAME_MAX} autoComplete="off" defaultValue={edit?.name ?? ''} placeholder={tt('мысалы, Статистика', 'например, Статистика')} onChange={() => setErr('')} /></div>
      <div className="field"><label htmlFor="ttDay">{tt('Апта күні', 'День недели')}</label>
        <select className="input" id="ttDay" defaultValue={String(edit?.wd ?? Math.min(weekdayOf(new Date()), 5))}>
          {DAYS.map(d => <option key={d} value={d}>{wd[(d + 1) % 7]}</option>)}
        </select></div>
      <div className="cal-form-row cal-time-row">
        <div className="field"><label htmlFor="ttStart">{tt('Басталуы', 'Начало')}</label>
          <input className="input" id="ttStart" type="time" required defaultValue={edit?.t ?? ''} onChange={() => setErr('')} /></div>
        <div className="field"><label htmlFor="ttEnd">{tt('Аяқталуы', 'Окончание')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
          <input className="input" id="ttEnd" type="time" defaultValue={edit?.end ?? ''} onChange={() => setErr('')} /></div>
      </div>
      <div className="cal-form-row">
        <div className="field"><label htmlFor="ttRoom">{tt('Аудитория', 'Аудитория')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
          <input className="input" id="ttRoom" maxLength={ROOM_MAX} autoComplete="off" defaultValue={edit?.room ?? ''} placeholder="4-214" /></div>
        <div className="field"><label htmlFor="ttKind">{tt('Түрі', 'Тип')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
          <input className="input" id="ttKind" maxLength={KIND_MAX} autoComplete="off" list="ttKinds" defaultValue={edit?.kind ?? ''} />
          <datalist id="ttKinds">{[tt('Дәріс', 'Лекция'), tt('Практика', 'Практика'), tt('Семинар', 'Семинар'), tt('Зертханалық', 'Лабораторная')].map(k => <option key={k} value={k} />)}</datalist></div>
      </div>
      {err ? <p className="field-err" id="ttErr" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn" type="submit"><Icon n={edit ? 'check' : 'plus'} s={18} /> {edit ? tt('Сақтау', 'Сохранить') : tt('Қосу', 'Добавить')}</button>
        {edit ? <button className="btn ghost" type="button" onClick={onDone}>{tt('Болдырмау', 'Отмена')}</button> : null}
      </div>
    </form>
  );
}
