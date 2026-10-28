'use client';
/* Profile and its sub-pages, the student card and GPA bonus of the discounts category, settings and onboarding. The profile and photo stay on this device only. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Icon, Note, Row, SectionHead, Seg } from '@/components/ui';
import { AddCertSheet, StudentQrSheet } from '@/components/views/sheets';
import { ADMISSION, CITIES, DOCS, EVENTS, GRADES, HOME_CITIES, LEVELS, PAYMENT, SKILLS, SPECS, TIERS } from '@/lib/data/static';
import { certificates, profileFill, studentCode } from '@/lib/domain';
import { dOff, money } from '@/lib/format';
import { demoProfile } from '@/lib/defaults';
import { getState, resetState, update } from '@/lib/store';
import { track } from '@/features/analytics/track';
import { useTelegram } from '@/lib/telegram';
import { AccountSection } from '@/components/views/account';
import { NotifySection } from '@/components/views/notify';
import { RoadmapProgress } from '@/components/views/roadmap';
import { ProfileCommunity } from '@/components/views/entries';
import { VerifiedAttendance } from '@/components/views/events';
import { PublicPortfolioSection } from '@/components/views/portfolio';
import type { Profile as ProfileT } from '@/lib/types';

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
function readProfileForm(root: HTMLElement, keepDemo = false) {
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
    if (!keepDemo) p.demo = false;
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
        <button className="btn ghost" type="button" id="obDemo" onClick={() => { update(s => { s.profile = demoProfile(s.lang); s.onboarded = true; }); closeSheet(); }}>{tt('Үлгі профильмен қарау', 'Посмотреть с примером профиля')}</button>
      </form>
    </>
  );
}

/* ---------- profile ---------- */
export function Profile() {
  const { S, tt, L, I, toast, go } = useApp();
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
      {p.demo ? <Note>{tt('Бұл — үлгі профиль. Өз деректеріңізді енгізіңіз.', 'Это пример профиля. Укажите свои данные.')}</Note> : null}
      <div className="group">
        <Row go="edit" icon="user" title={tt('Жеке ақпарат', 'Личные данные')} />
        <Row go="gpa" icon="cap" title={tt('Білім туралы', 'Об учёбе')} />
        <Row go="portfolio" icon="award" title={tt('Жетістіктер', 'Достижения')} />
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
      <Note>{tt('Серіктестер мен жеңілдік мөлшері — үлгі. Нақты шарттар серіктестермен жасалған келісімге байланысты.', 'Партнёры и размеры скидок — примеры. Реальные условия зависят от соглашений с партнёрами.')}</Note>
    </>
  );
}

/** «Үздік оқығаны үшін бонус» under the discounts */
export function BonusTiers() {
  const { S, tt, tp, go } = useApp();
  const p = S.profile;
  const tier = TIERS.find(t => p.gpa != null && p.gpa >= t.min);
  const next = TIERS.slice().reverse().find(t => p.gpa != null && p.gpa < t.min);
  return (
    <section className="section">
      <h2>{tt('Үздік оқығаны үшін бонус', 'Бонусы за успеваемость')}</h2>
      <div className="card">
        <div className="card-row" style={{ alignItems: 'center' }}><span className={`ico ${tier ? 'sun' : ''}`}><Icon n="award" /></span>
          <div className="row-main"><span className="row-title">{tier ? tp(tier.name) : tt('Деңгей әзірге жоқ', 'Уровня пока нет')}</span>
            <span className="row-sub">GPA {(p.gpa == null ? tt('көрсетілмеген', 'не указан') : p.gpa.toFixed(2))}{next ? ' · ' + tt(`${tp(next.name)} үшін ${next.min.toFixed(2)} керек`, `для уровня «${tp(next.name)}» нужно ${next.min.toFixed(2)}`) : ''}</span></div></div>
        {tier ? <ul className="bullets">{tier.perks.map((x, i) => <li key={i}>{tp(x)}</li>)}</ul> : null}
        <p className="tiny muted">{tt('Жоғары баға өздігінен жеңілдік бермейді: ереже мен қаржыландыру университетпен және серіктестермен келісіледі.', 'Высокие оценки сами по себе не дают права на скидку: правила и финансирование согласуются с университетом и партнёрами.')}</p>
        <button className="btn ghost" type="button" onClick={() => go('gpa')}><Icon n="chart" s={18} /> {tt('GPA калькуляторы', 'Калькулятор GPA')}</button>
      </div>
    </section>
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
        <SectionHead title={tt('Осы сессиядағы пәндер', 'Дисциплины этой сессии')} more={<button className="link-btn" type="button" onClick={() => update(s => { s.gpaPlan.push({ n: tt('Жаңа пән', 'Новый предмет'), cr: 5, g: 'B' }); })}>+ {tt('Пән қосу', 'Добавить')}</button>} />
        <div className="group">{plan.map((x, i) => (
          <div key={i} className="row" style={{ gap: 8 }}>
            <input className="input" style={{ flex: 1, minWidth: 0 }} aria-label={tt('Пән', 'Дисциплина')} value={x.n} onChange={e => setRow(i, 'n', e.target.value)} />
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

export function Pay() {
  const { tt, L, I } = useApp();
  const y = PAYMENT, rest = y.year - y.paid, pct = Math.round(100 * y.paid / y.year), due = dOff(y.nextDue);
  return (
    <>
      <Note>{tt('Сомалар — үлгі. Нақты деректер университеттің қаржы жүйесімен келісілген қосылудан кейін көрсетіледі.', 'Суммы — пример. Реальные данные появятся после согласованного подключения к финансовой системе университета.')}</Note>
      <div className="card">
        <span className="eyebrow">{tt('2026–2027 оқу жылы', '2026–2027 учебный год')}</span>
        <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'baseline' }}><span className="big-num" style={{ fontSize: '1.7rem' }}>{money(y.paid)}</span><span className="muted small">/ {money(y.year)}</span></div>
        <div className="meter"><i style={{ width: `${pct}%` }}></i></div>
        <dl className="kv"><dt>{tt('Қалдық', 'Остаток')}</dt><dd className="num">{money(rest)}</dd><dt>{tt('Берешек', 'Задолженность')}</dt><dd style={{ color: 'var(--ok)' }}>{tt('Жоқ', 'Нет')}</dd></dl>
      </div>
      <div className="group"><Row icon="bell" tone={y.nextDue <= 7 ? 'bad' : 'sun'} title={tt('Келесі төлем', 'Следующий платёж') + ': ' + money(y.nextAmount)} sub={`${I.fDate(due)} ${tt('дейін', 'до')} · ${I.daysLeftTxt(y.nextDue)}`} chev={false} /></div>
      <section className="section"><h2>{tt('Төлемдер тарихы', 'История платежей')}</h2>
        <div className="group table-wrap"><table><thead><tr><th>{tt('Күні', 'Дата')}</th><th>{tt('Тәсілі', 'Способ')}</th><th className="r">{tt('Сома', 'Сумма')}</th></tr></thead>
          <tbody>{y.history.map(h => <tr key={h.day}><td>{I.fDateY(dOff(h.day))}</td><td>{L(h.via)}</td><td className="r">{money(h.amount)}</td></tr>)}</tbody></table></div></section>
      <section className="card"><h3>{tt('Құны неге өзгерді?', 'Почему изменилась стоимость?')}</h3>
        <dl className="kv"><dt>{tt('Бұрын', 'Было')}</dt><dd className="num">{money(y.change.from)}</dd><dt>{tt('Қазір', 'Стало')}</dt><dd className="num">{money(y.change.to)}</dd>
          <dt>{tt('Күні', 'Дата')}</dt><dd>{I.fDateY(dOff(y.change.date))}</dd><dt>{tt('Негізі', 'Основание')}</dt><dd>{L(y.change.basis)}</dd></dl></section>
      <p className="tiny muted">{tt('Төлемді тек университет бекіткен ресми тәсілдермен жасаңыз. Қосымша карта деректерін сақтамайды.', 'Оплачивайте только официальными способами, утверждёнными университетом. Приложение не хранит данные карт.')}</p>
    </>
  );
}

export function Portfolio() {
  const { S, I, tt, L, go, openSheet } = useApp();
  const certs = certificates(S, I);
  const attended = EVENTS.filter(e => e.past && S.events[e.id]);
  const p = S.profile;
  const exp = (p.exp || '').split('\n').filter(s => s.trim());
  return (
    <>
      <PublicPortfolioSection />
      <section className="section"><SectionHead title={tt('Сертификаттар', 'Сертификаты')} more={<button className="link-btn" type="button" onClick={() => openSheet(<AddCertSheet />)}>+ {tt('Қосу', 'Добавить')}</button>} />
        <div className="group">{certs.length ? certs.map((c, k) => (
          <Row key={k} icon="award" tone="sun" title={c.title} sub={`${c.org} · ${I.fDateY(c.date)}`} chev={false}
            end={c.src === 'manual' ? <button className="icon-btn" type="button" style={{ color: 'var(--muted)' }} aria-label={tt('Жою', 'Удалить')} onClick={() => update(s => { s.certs.splice(c.i ?? -1, 1); })}><Icon n="trash" s={18} /></button> : null} />
        )) : <div className="empty">{tt('Курсты аяқтаңыз немесе сертификат қосыңыз', 'Пройдите курс или добавьте сертификат')}</div>}</div></section>
      <section className="section"><h2>{tt('Қатысқан іс-шаралар', 'Мероприятия')}</h2>
        <VerifiedAttendance />
        <div className="group">{attended.length ? attended.map(e => <Row key={e.id} icon="calendar" title={L(e.title)} sub={I.fDateY(dOff(e.day))} chev={false} />) : <div className="empty">{tt('Әзірге жоқ', 'Пока нет')}</div>}</div></section>
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
