'use client';
/* Profile and its sub-pages, the student card of the discounts category, settings and onboarding. The profile and photo stay on this device only. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Empty, Icon, Row, SectionHead, Seg } from '@/components/ui';
import { AddCertSheet, StudentQrSheet } from '@/components/views/sheets';
import { ADMISSION, CITIES, DOCS, GRADES, HOME_CITIES, LEVELS, SKILLS, SPECS } from '@/lib/data/static';
import { certificates, profileFill, studentCode } from '@/lib/domain';
import { ConfirmSheet, Icon, Note, Row, SectionHead, Seg } from '@/components/ui';
import { StudentQrSheet } from '@/components/views/sheets';
import { CertificatesSection } from '@/components/views/cert-files';
import { ADMISSION, CITIES, DOCS, EVENTS, GRADES, HOME_CITIES, LEVELS, PAYMENT, SKILLS, SPECS, TIERS } from '@/lib/data/static';
import { profileFill, studentCode } from '@/lib/domain';
import { dOff, money } from '@/lib/format';
import { demoProfile } from '@/lib/defaults';
import { getState, resetState, update } from '@/lib/store';
import { DeadlineSheet } from '@/components/views/calendar';
import { track } from '@/features/analytics/track';
import { useTelegram } from '@/lib/telegram';
import { AccountSection } from '@/components/views/account';
import { NotifySection } from '@/components/views/notify';
import { RoadmapProgress } from '@/components/views/roadmap';
import { ProfileCommunity } from '@/components/views/entries';
import { VerifiedAttendance } from '@/components/views/events';
import { PublicPortfolioSection } from '@/components/views/portfolio';
import type { Profile as ProfileT } from '@/lib/types';
import { useFeature } from '@/components/views/common';

/* ---------- profile form (edit page and onboarding) ---------- */
function ProfileFields({ compact, blank }: { compact?: boolean; blank?: boolean }) {
  const { S, tt, L } = useApp();
  const p = S.profile;
  const [skills, setSkills] = useState<string[]>(p.skills);
  return (
    <>
      <div className="field"><label htmlFor="fName">{tt('Аты-жөні', 'Имя и фамилия')}</label><input className="input" id="fName" defaultValue={blank ? '' : p.name} placeholder={tt('Атыңыз бен тегіңіз', 'Ваше имя и фамилия')} autoComplete="name" required /></div>
      <div className="field"><label htmlFor="fSpec">{tt('Факультет / мамандық', 'Факультет / специальность')}</label>
        <select className="input" id="fSpec" required defaultValue={blank ? '' : p.spec}>
          {blank ? <option value="" disabled>{tt('Таңдаңыз', 'Выберите')}</option> : null}
          {Object.entries(SPECS).map(([k, s]) => <option key={k} value={k}>{L(s)}</option>)}
        </select></div>
      <div className="grid-2">
        <div className="field"><label htmlFor="fCourse">{tt('Курс', 'Курс')}</label><select className="input" id="fCourse" required defaultValue={p.course ? String(p.course) : ''}><option value="" disabled>{tt('Таңдаңыз', 'Выберите')}</option>{[1, 2, 3, 4].map(n => <option key={n}>{n}</option>)}</select></div>
        <div className="field"><label htmlFor="fGpa">{tt("GPA (0–4, міндетті емес)", "GPA (0–4, необязательно)")}</label><input className="input num" id="fGpa" type="text" inputMode="decimal" defaultValue={blank || p.gpa == null ? '' : String(p.gpa)} placeholder={tt('мысалы, 3.2', 'например, 3.2')} /></div>
      </div>
      <div className="grid-2">
        <div className="field"><label htmlFor="fEn">{tt('Ағылшын деңгейі', 'Уровень английского')}</label><select className="input" id="fEn" defaultValue={p.en}><option value="">{tt('Көрсетілмеген', 'Не указано')}</option>{LEVELS.map(l => <option key={l}>{l}</option>)}</select></div>
        <div className="field"><label htmlFor="fCity">{tt('Қала', 'Город')}</label><select className="input" id="fCity" defaultValue={p.city}><option value="">{tt('Көрсетілмеген', 'Не указано')}</option>{HOME_CITIES.map(c => <option key={c} value={c}>{L(CITIES[c])}</option>)}</select></div>
      </div>
      {compact ? null : (
        <>
          <div className="field"><span className="lbl">{tt('Дағдылар', 'Навыки')}</span>
            <div className="chips wrap" id="fSkills">{Object.entries(SKILLS).map(([k, s]) => (
              <button key={k} className="chip" type="button" aria-pressed={skills.includes(k)} data-skill={k}
                onClick={() => setSkills(x => (x.includes(k) ? x.filter(y => y !== k) : [...x, k]))}>{L(s)}</button>
            ))}</div></div>
          <div className="field"><label htmlFor="fCredits">{tt('Жиналған кредиттер', 'Набрано кредитов')}</label><input className="input num" id="fCredits" type="number" min="0" max="300" defaultValue={p.credits} /></div>
          <div className="field"><label htmlFor="fExp">{tt('Тәжірибе (әр жолға бір)', 'Опыт (по одному на строку)')}</label><textarea className="input" id="fExp" defaultValue={p.exp}></textarea></div>
          <div className="field"><label htmlFor="fAbout">{tt('Өзім туралы (түйіндеме үшін)', 'О себе (для резюме)')}</label><textarea className="input" id="fAbout" placeholder={tt('Бос қалса, үлгі мәтін қойылады', 'Если оставить пустым, подставим пример')} defaultValue={p.about}></textarea></div>
          <div className="grid-2">
            <div className="field"><label htmlFor="fPhone">{tt('Телефон (міндетті емес)', 'Телефон (необязательно)')}</label><input className="input" id="fPhone" type="tel" defaultValue={p.phone} placeholder="+7 7__ ___ __ __" /></div>
            <div className="field"><label htmlFor="fEmail">Email</label><input className="input" id="fEmail" type="email" defaultValue={p.email} /></div>
          </div>
        </>
      )}
    </>
  );
}

/** read the form into the profile (legacy readProfileForm); marks the profile as the student's own */
function readProfileForm(root: HTMLElement) {
  const v = (id: string) => root.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('#' + id)?.value;
  const has = (id: string) => !!root.querySelector('#' + id);
  update(s => {
    const p: ProfileT = s.profile;
    p.name = (v('fName') || '').trim() || p.name;
    p.spec = v('fSpec') || p.spec;
    p.course = parseInt(v('fCourse') || '', 10) || p.course;
    const g = parseFloat(String(v('fGpa')).replace(',', '.'));
    p.gpa = Number.isFinite(g) ? Math.max(0, Math.min(4, g)) : null;
    p.en = v('fEn') || '';
    p.city = v('fCity') || '';
    const sk = root.querySelector('#fSkills');
    if (sk) p.skills = [...sk.querySelectorAll<HTMLElement>('[aria-pressed="true"]')].map(b => b.dataset.skill || '').filter(Boolean);
    if (has('fCredits')) p.credits = Math.max(0, parseInt(v('fCredits') || '', 10) || 0);
    if (has('fExp')) p.exp = v('fExp') || '';
    if (has('fAbout')) p.about = v('fAbout') || '';
    if (has('fPhone')) p.phone = (v('fPhone') || '').trim();
    if (has('fEmail')) p.email = (v('fEmail') || '').trim();
    delete p.demo;
  });
}

export function Edit() {
  const { tt, toast, back } = useApp();
  return (
    <form className="form" id="profForm" onSubmit={e => { e.preventDefault(); readProfileForm(e.currentTarget); toast(tt('Профиль сақталды', 'Профиль сохранён')); back(); }}>
      <ProfileFields />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

export function Onboarding() {
  const { S, tt, closeSheet } = useApp();
  return (
    <>
      <Seg items={[['kk', 'Қазақша'], ['ru', 'Русский']]} cur={S.lang} onPick={v => update(s => { s.lang = v === 'ru' ? 'ru' : 'kk'; })} />
      <h1>{tt('Қош келдіңіз!', 'Добро пожаловать!')}</h1>
      <p className="small muted">{tt('Мамандығыңыз бен курсыңызды көрсетіңіз. Жұмыс, тағылымдама, курстар мен жеңілдіктерді соған қарай іріктейміз. Кейін профильден өзгертуге болады.', 'Укажите специальность и курс — подберём работу, стажировки, курсы и скидки под вас. Потом это можно изменить в профиле.')}</p>
      <form className="form" id="obForm" onSubmit={e => {
        e.preventDefault(); readProfileForm(e.currentTarget); update(s => { s.onboarded = true; }); closeSheet();
        // category keys only: never the name, GPA or phone
        const p = getState().profile;
        track('onboarding_complete', { faculty: p.spec, course: p.course, city: p.city });
      }}>
        <ProfileFields compact blank />
        <button className="btn" type="submit">{tt('Бастау', 'Начать')}</button>
      </form>
    </>
  );
}

/* ---------- profile ---------- */
export function Profile() {
  const { S, tt, L, I, toast, go } = useApp();
  const points = useFeature('points');
  const p = S.profile, fill = profileFill(S, I);
  const onPhoto = (f: File | undefined) => {
    if (!f) return;
    if (!/^image\//.test(f.type)) { toast(tt('Сурет файлын таңдаңыз', 'Выберите файл изображения')); return; }
    const img = new Image(), url = URL.createObjectURL(f);
    img.onload = () => {
      const side = Math.min(img.naturalWidth, img.naturalHeight), size = 320;
      const c = document.createElement('canvas');
      c.width = c.height = size;
      c.getContext('2d')?.drawImage(img, (img.naturalWidth - side) / 2, (img.naturalHeight - side) / 2, side, side, 0, 0, size, size);
      URL.revokeObjectURL(url);
      const photo = c.toDataURL('image/jpeg', 0.85);
      update(s => { s.photo = photo; });
      toast(tt('Сурет сақталды', 'Фото сохранено'));
    };
    img.onerror = () => { URL.revokeObjectURL(url); toast(tt('Суретті ашу мүмкін болмады', 'Не удалось открыть изображение')); };
    img.src = url;
  };
  return (
    <>
      <section className="me">
        <label className="me-photo" title={tt('Сурет қою', 'Поставить фото')}>
          {/* eslint-disable-next-line @next/next/no-img-element -- a local data: URL from this device */}
          <span className="me-avatar">{S.photo ? <img src={S.photo} alt="" /> : (p.name || 'S').trim().charAt(0).toUpperCase()}</span>
          <span className="me-edit" aria-hidden="true"><Icon n="edit" s={16} /></span>
          <input type="file" id="photoIn" accept="image/*" aria-label={tt('Профиль суретін таңдау', 'Выбрать фото профиля')} onChange={e => { onPhoto(e.target.files?.[0]); e.target.value = ''; }} />
        </label>
        <h1>{p.name}</h1>
        <p className="muted">{tt('Студент · Сейфуллин университеті', 'Студент · Университет им. С. Сейфуллина')}</p>
        <p className="muted small">{L(SPECS[p.spec])}, {tt(`${p.course}-курс`, `${p.course} курс`)}, GPA {(p.gpa == null ? tt('көрсетілмеген', 'не указан') : p.gpa.toFixed(2))}</p>
        {S.photo ? <button className="link-btn" type="button" onClick={() => { update(s => { delete s.photo; }); toast(tt('Сурет өшірілді', 'Фото удалено')); }}>{tt('Суретті өшіру', 'Удалить фото')}</button> : null}
      </section>
      <button className="card fill" type="button" onClick={() => go('edit')}>
        <span className="fill-head"><span className="row-title">{tt('Профиль толықтығы', 'Заполненность профиля')}</span><b className="num">{fill}%</b></span>
        <span className="meter" role="progressbar" aria-valuenow={fill} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${fill}%` }}></i></span>
        {fill < 100 ? <span className="row-sub">{tt('Тәжірибе мен өзіңіз туралы толтырсаңыз, түйіндеме мен іріктеме дәлірек болады.', 'Заполните опыт и «о себе», чтобы резюме и подборка были точнее.')}</span> : null}
      </button>
      <RoadmapProgress />
      <div className="group">
        <Row go="edit" icon="user" title={tt('Жеке ақпарат', 'Личные данные')} />
        <Row go="gpa" icon="cap" title={tt('Білім туралы', 'Об учёбе')} />
        <Row go="portfolio" icon="award" title={tt('Жетістіктер', 'Достижения')} />
        {points ? <Row go="points" icon="trophy" title={tt('Ұпайлар', 'Баллы')} sub={tt('Деңгей, серия, монеталар және сыйлықтар', 'Уровень, серия, монеты и награды')} /> : null}
        <Row go="mine" icon="clock" title={tt('Іс-әрекет тарихы', 'История действий')} />
        <Row go="saved" icon="bookmark" title={tt('Сақталғандар', 'Сохранённое')} />
        <Row go="settings" icon="gear" title={tt('Параметрлер', 'Параметры')} />
      </div>
      <ProfileCommunity />
      <p className="note">{tt('Деректеріңіз бен суретіңіз тек осы құрылғыда сақталады.', 'Ваши данные и фото хранятся только на этом устройстве.')}</p>
    </>
  );
}

/* ---------- discounts ---------- */
/** «Цифрлық студенттік» card at the top of the discounts category; opens the QR */
export function StudentCardHero() {
  const { S, tt, L, openSheet } = useApp();
  const p = S.profile;
  return (
    <>
      <button className="hero" type="button" onClick={() => openSheet(<StudentQrSheet />)} style={{ border: 0, textAlign: 'left', width: '100%' }}>
        <div className="hero-top" style={{ justifyContent: 'space-between' }}>
          <div style={{ minWidth: 0 }}><span className="eyebrow" style={{ color: 'var(--head-muted)' }}>{tt('Цифрлық студенттік', 'Цифровой студенческий')}</span><h2>{p.name}</h2><p className="small muted">{L(SPECS[p.spec])} · {tt(`${p.course}-курс`, `${p.course} курс`)}</p></div>
          <span className="avatar" style={{ borderRadius: 14 }}><Icon n="qr" s={26} /></span>
        </div>
        <span className="code">{studentCode(p)}</span>
      </button>
    </>
  );
}

export function Gpa() {
  const { S, ui, tt, setUi } = useApp();
  const p = S.profile;
  const pts = (g: string) => (GRADES.find(x => x[0] === g) || ['', 0])[1];
  const plan = S.gpaPlan;
  const addCr = plan.reduce((s, x) => s + (+x.cr || 0), 0);
  const addPts = plan.reduce((s, x) => s + (+x.cr || 0) * pts(x.g), 0);
  const proj = ((p.gpa ?? 0) * p.credits + addPts) / Math.max(1, p.credits + addCr);
  const risk = plan.filter(x => pts(x.g) < 2.67);
  const target = ui.target || 3.5, nextCr = ui.nextCr || 60;
  const need = (target * (p.credits + nextCr) - (p.gpa ?? 0) * p.credits) / nextCr;
  const needLetter = need <= 4 ? (GRADES.slice().reverse().find(x => x[1] >= need - 1e-9) || GRADES[0])[0] : null;
  // inputs keep what the student types; the numbers recompute when the field is committed (legacy: on change)
  const [draft, setDraft] = useState<{ target: string; next: string }>({ target: String(target), next: String(nextCr) });
  const setRow = (i: number, f: 'n' | 'cr' | 'g', value: string) => update(s => {
    const r = s.gpaPlan[i];
    if (!r) return;
    if (f === 'cr') r.cr = Math.max(0, parseInt(value, 10) || 0); else r[f] = value;
  });
  const riskNames = risk.map(x => x.n).join(', ');
  return (
    <>
      <div className="card">
        <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
          <div><span className="eyebrow">{tt('Ағымдағы GPA', 'Текущий GPA')}</span><div className="big-num">{p.gpa == null ? '—' : p.gpa.toFixed(2)}</div></div>
          <div style={{ textAlign: 'right' }}><span className="eyebrow">{tt('Сессиядан кейін', 'После сессии')}</span><div className="big-num" style={{ color: 'var(--sky)' }}>{p.gpa == null ? '—' : proj.toFixed(2)}</div></div>
        </div>
        <span className="tiny muted num">{p.credits} {tt('кредит жиналды', 'кредитов набрано')} · {tt('4.0 шкаласы', 'шкала 4.0')}</span>
      </div>
      <section className="section">
        <SectionHead title={tt('Осы сессиядағы пәндер', 'Дисциплины этой сессии')} more={<button className="link-btn" type="button" onClick={() => update(s => { s.gpaPlan.push({ n: '', cr: 5, g: 'B' }); })}>+ {tt('Пән қосу', 'Добавить')}</button>} />
        {plan.length ? null : (
          <Empty icon="chart" action={<button className="btn secondary" type="button" id="gpaAdd" onClick={() => update(s => { s.gpaPlan.push({ n: '', cr: 5, g: 'B' }); })}><Icon n="plus" s={18} /> {tt('Пән қосу', 'Добавить предмет')}</button>}>
            {tt('Осы сессиядағы пәндеріңізді, кредитін және күтілетін бағаңызды қосыңыз — сессиядан кейінгі GPA есептеледі.', 'Добавьте предметы этой сессии, их кредиты и ожидаемую оценку — посчитаем GPA после сессии.')}
          </Empty>
        )}
        <div className="group">{plan.map((x, i) => (
          <div key={i} className="row" style={{ gap: 8 }}>
            <input className="input" style={{ flex: 1, minWidth: 0 }} aria-label={tt('Пән', 'Дисциплина')} placeholder={tt('Пән атауы', 'Название предмета')} value={x.n} onChange={e => setRow(i, 'n', e.target.value)} />
            <input className="input num" style={{ width: 64, flex: 'none' }} type="number" min="1" max="15" aria-label={tt('Кредит', 'Кредиты')} value={x.cr} onChange={e => setRow(i, 'cr', e.target.value)} />
            <select className="input" style={{ width: 78, flex: 'none' }} aria-label={tt('Баға', 'Оценка')} value={x.g} onChange={e => setRow(i, 'g', e.target.value)}>{GRADES.map(gr => <option key={gr[0]}>{gr[0]}</option>)}</select>
            <button className="icon-btn" type="button" style={{ color: 'var(--muted)', flex: 'none' }} aria-label={tt('Жою', 'Удалить')} onClick={() => update(s => { s.gpaPlan.splice(i, 1); })}><Icon n="x" s={18} /></button>
          </div>
        ))}</div>
        <p className="tiny muted">{tt('Атау · кредит · күтілетін баға', 'Название · кредиты · ожидаемая оценка')}</p>
      </section>
      {risk.length ? (
        <div className="demo-note" style={{ background: 'var(--warn-soft)' }}><Icon n="bell" s={18} /><span>{tt(
          `Назар аударыңыз: ${riskNames} — C немесе одан төмен баға. Әдетте мемлекеттік шәкіртақы «өте жақсы» және «жақсы» бағалармен (B− және жоғары) жабылған сессияға беріледі. Ережені университеттен нақтылаңыз.`,
          `Внимание: ${riskNames} — оценка C или ниже. Обычно госстипендия назначается за сессию, закрытую на «отлично» и «хорошо» (B− и выше). Уточните правила в университете.`)}</span></div>
      ) : null}
      <section className="card">
        <h2>{tt('Мақсатты GPA', 'Целевой GPA')}</h2>
        <div className="grid-2">
          <div className="field"><label htmlFor="gTarget">{tt('Мақсат', 'Цель')}</label>
            <input className="input num" id="gTarget" type="text" inputMode="decimal" value={draft.target}
              onChange={e => setDraft(d => ({ ...d, target: e.target.value }))}
              onBlur={e => { const v = parseFloat(e.target.value.replace(',', '.')); if (!isNaN(v)) setUi({ target: Math.max(0, Math.min(4, v)) }); }} /></div>
          <div className="field"><label htmlFor="gNext">{tt('Алдағы кредиттер', 'Предстоящие кредиты')}</label>
            <input className="input num" id="gNext" type="number" min="1" max="200" value={draft.next}
              onChange={e => setDraft(d => ({ ...d, next: e.target.value }))}
              onBlur={e => { const v = parseInt(e.target.value, 10); if (v > 0) setUi({ nextCr: v }); }} /></div>
        </div>
        <p>{p.gpa == null ? tt('Болжамды есептеу үшін GPA-ды профильде көрсетуге болады. Бұл міндетті емес.', 'Для расчёта прогноза можно указать GPA в профиле. Это необязательно.') : need <= 0 ? tt('Мақсатқа жеттіңіз — қазіргі деңгейді сақтаңыз.', 'Цель уже достигнута — держите уровень.')
          : need > 4 ? tt(`${nextCr} кредитте ${target.toFixed(2)}-ге жету мүмкін емес. Кредит санын көбейтіп көріңіз.`, `За ${nextCr} кредитов достичь ${target.toFixed(2)} невозможно. Попробуйте больше кредитов.`)
            : <>{tt(`Алдағы ${nextCr} кредитте орташа `, `В следующих ${nextCr} кредитах нужен средний балл `)}<b className="num">{need.toFixed(2)}</b>{tt(' алу керек — шамамен ', ' — примерно ')}<b>{needLetter}</b>{tt(' және жоғары.', ' и выше.')}</>}</p>
      </section>
      <section className="section"><h2>{tt('Бағалау шкаласы', 'Шкала оценок')}</h2>
        <div className="group table-wrap"><table><thead><tr><th>{tt('Әріп', 'Буква')}</th><th className="r">{tt('Балл', 'Балл')}</th><th className="r">%</th></tr></thead>
          <tbody>{GRADES.map(g => <tr key={g[0]}><td><b>{g[0]}</b></td><td className="r">{g[1].toFixed(2)}</td><td className="r">{g[2]}</td></tr>)}</tbody></table></div>
        <p className="tiny muted">{tt('Ресми бағаларды университеттің ақпараттық жүйесі (мысалы, Platonus) қосылғанда автоматты түрде көрсетеміз.', 'Официальные оценки появятся автоматически после подключения информационной системы университета (например, Platonus).')}</p>
      </section>
    </>
  );
}

/* ---------- documents, admission, payment, portfolio, settings ---------- */
export function Docs() {
  const { tt, L } = useApp();
  return (
    <>
      <div className="group">{DOCS.map(d => <Row key={d.id} go={'doc:' + d.id} icon="file" tone={d.id === 'medical' ? 'bad' : ''} title={L(d.title)} sub={L(d.why)} />)}</div>
      <p className="tiny muted">{tt('Университеттің жүйесі қосылғанда кейбір анықтамаларға тікелей қосымшадан тапсырыс беруге болады. Деректерді беруге тек өзіңіз рұқсат бересіз.', 'После подключения системы университета некоторые справки можно будет заказывать прямо в приложении. Передачу данных разрешаете только вы.')}</p>
    </>
  );
}

export function DocView({ id }: { id: string }) {
  const { tt, L } = useApp();
  const d = DOCS.find(x => x.id === id);
  if (!d) return <Docs />;
  return (
    <>
      <h1>{L(d.title)}</h1>
      <div className="group">
        <Row icon="target" title={tt('Не үшін керек', 'Для чего нужна')} sub={L(d.why)} chev={false} />
        <Row icon="pin" title={tt('Қайдан алуға болады', 'Где заказать')} sub={L(d.where)} chev={false} />
        <Row icon="globe" title={tt('Онлайн ба?', 'Можно ли онлайн?')} sub={L(d.online)} chev={false} />
      </div>
      <section className="section"><h2>{tt('Не дайындау керек', 'Что понадобится')}</h2><ul className="bullets">{d.need.map((n, i) => <li key={i}>{L(n)}</li>)}</ul></section>
      {d.id === 'medical' ? <a className="btn ghost" href="https://egov.kz" target="_blank" rel="noopener"><Icon n="ext" s={18} /> eGov.kz</a> : null}
      <p className="tiny muted">{tt('Дайын болуын университеттің ақпараттық жүйесінен немесе тіркеу кеңсесінен тексеріңіз.', 'Готовность проверяйте в информационной системе университета или в офисе регистратора.')}</p>
    </>
  );
}

export function Admission() {
  const { ui, tt, L, setUi, go } = useApp();
  const a = ADMISSION[ui.admTab] || ADMISSION.bach;
  return (
    <>
      <Seg items={Object.entries(ADMISSION).map(([k, v]): [string, string] => [k, L(v.title)])} cur={ui.admTab} onPick={v => setUi({ admTab: v })} />
      <div className="group">{a.items.map((it, i) => <div key={i} className="row" style={{ alignItems: 'flex-start' }}><span className="row-main"><span className="row-title">{L(it.h)}</span><span className="row-sub">{L(it.t)}</span></span></div>)}</div>
      <section className="card">
        <h3>{tt('Өткен жылдардың статистикасы', 'Статистика прошлых лет')}</h3>
        <p className="small muted">{tt('Грант саны мен шекті баллдар ресми дереккөз қосылғанда көрсетіледі — жылы мен дереккөзі әрдайым жазылады. Өткен жылғы балл келесі жылы түсуге кепілдік бермейді.', 'Число грантов и проходные баллы появятся после подключения официального источника — всегда с годом и источником. Прошлогодний балл не гарантирует поступление в следующем году.')}</p>
      </section>
      <button className="btn ghost" type="button" onClick={() => go('opps:intern')}><Icon n="globe" s={18} /> {tt('Академиялық ұтқырлық бағдарламалары', 'Программы академической мобильности')}</button>
      <p className="tiny muted">{tt('Ережелер жыл сайын өзгеруі мүмкін — университеттің қабылдау комиссиясынан нақтылаңыз.', 'Правила меняются каждый год — уточняйте в приёмной комиссии университета.')}</p>
    </>
  );
}

/** «Оқу ақысы»: the university's finance system is not connected, so no sums are shown — only where to check them
    and a way to keep one's own payment deadline in the calendar */
export function Pay() {
  const { tt, openSheet } = useApp();
  return (
    <>
      <Empty icon="tenge" action={
        <div className="btn-row" style={{ justifyContent: 'center' }}>
          <button className="btn secondary" type="button" id="payDeadline" onClick={() => openSheet(<DeadlineSheet />)}><Icon n="bell" s={18} /> {tt('Төлем мерзімін күнтізбеге қосу', 'Добавить срок оплаты в календарь')}</button>
        </div>
      }>
        {tt('Оқу ақысының деректері әзірге қосылмаған. Сома, мерзім мен төлем тарихын университеттің ақпараттық жүйесінен немесе бухгалтериядан тексеріңіз.', 'Данные об оплате обучения пока не подключены. Сумму, сроки и историю платежей проверяйте в информационной системе университета или в бухгалтерии.')}
      </Empty>
      <div className="group">
        <Row href="https://kazatu.edu.kz/ru/contacts" icon="globe" title={tt('Университет байланыстары', 'Контакты университета')} sub="kazatu.edu.kz" />
      </div>
      <p className="tiny muted">{tt('Төлемді тек университет бекіткен ресми тәсілдермен жасаңыз. Қосымша карта деректерін сақтамайды.', 'Оплачивайте только официальными способами, утверждёнными университетом. Приложение не хранит данные карт.')}</p>
    </>
  );
}

export function Portfolio() {
  const { S, I, tt, L, go, openSheet } = useApp();
  const certs = certificates(S, I);
  const { S, I, tt, L, go } = useApp();
  const attended = EVENTS.filter(e => e.past && S.events[e.id]);
  const p = S.profile;
  const exp = (p.exp || '').split('\n').filter(s => s.trim());
  return (
    <>
      <PublicPortfolioSection />
      <CertificatesSection />
      <section className="section"><h2>{tt('Қатысқан іс-шаралар', 'Мероприятия')}</h2>
        <VerifiedAttendance empty={<div className="group"><div className="empty">{tt('Әзірге жоқ. Іс-шараға QR арқылы белгіленсеңіз, мұнда расталған қатысу шығады.', 'Пока нет. Отметьтесь на мероприятии по QR — здесь появится подтверждённое участие.')}</div></div>} /></section>
      <section className="section"><h2>{tt('Тәжірибе', 'Опыт')}</h2>
        <div className="group">{exp.length ? exp.map((s, i) => <Row key={i} icon="briefcase" title={s} chev={false} />) : <div className="empty">{tt('Профильге тәжірибе қосыңыз', 'Добавьте опыт в профиль')}</div>}</div></section>
      <section className="section"><h2>{tt('Дағдылар', 'Навыки')}</h2><div className="chips wrap">{p.skills.filter(k => SKILLS[k]).map(k => <span key={k} className="chip">{L(SKILLS[k])}</span>)}</div></section>
      <button className="btn block" type="button" onClick={() => go('resume')}><Icon n="doc" s={18} /> {tt('Портфолиодан түйіндеме жасау', 'Собрать резюме из портфолио')}</button>
    </>
  );
}

export function Settings() {
  const { S, tt, toast, setUi, openSheet, go } = useApp();
  const tg = useTelegram();
  return (
    <>
      <section className="section"><h2>{tt('Тіл', 'Язык')}</h2><Seg items={[['kk', 'Қазақша'], ['ru', 'Русский']]} cur={S.lang} onPick={v => update(s => { s.lang = v === 'ru' ? 'ru' : 'kk'; })} /></section>
      {tg.app ? null : <section className="section"><h2>{tt('Тақырып', 'Тема')}</h2><Seg items={[['light', tt('Ашық', 'Светлая')], ['dark', tt('Қараңғы', 'Тёмная')]]} cur={S.theme === 'dark' ? 'dark' : 'light'} onPick={v => update(s => { s.theme = v; })} /></section>}
      <AccountSection />
      <NotifySection />
      <section className="section"><h2>{tt('Деректер', 'Данные')}</h2>
        <div className="group">
          <Row onClick={() => { update(s => { s.chat = []; }); setUi({ chatErr: '' }); toast(tt('Чат тазаланды', 'Чат очищен')); }} icon="chat" title={tt('Чат тарихын тазалау', 'Очистить историю чата')} chev={false} />
          <Row onClick={() => openSheet(<ConfirmSheet msg={tt('Барлық деректер өшіріледі. Жалғастыру керек пе?', 'Все данные будут удалены. Продолжить?')} yes={() => {
            resetState();
            go('home', { replace: true });
            setTimeout(() => openSheet(<Onboarding />), 60);
          }} />)} icon="trash" tone="bad" title={tt('Барлық деректерді өшіру', 'Удалить все данные')} sub={tt('Профиль, өтінімдер, жоспарлар, карточкалар', 'Профиль, отклики, планы, карточки')} chev={false} />
        </div></section>
      <div className="group"><Row go="privacy" icon="shield" title={tt('Құпиялылық саясаты', 'Политика конфиденциальности')} /></div>
      <p className="note">Student Hub 1.2. {tt('Алғашқы нұсқа (MVP). Профиль, GPA мен телефон осы құрылғыда сақталады.', 'Первая версия (MVP). Профиль, GPA и телефон хранятся на этом устройстве.')}</p>
    </>
  );
}
