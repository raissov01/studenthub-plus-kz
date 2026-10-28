'use client';
/* Bottom-sheet contents: deal QR, course, digital student card, add certificate, «Тексерілген» explanation. */
import { useApp } from '@/components/app';
import { Icon, Qr, Row } from '@/components/ui';
import { reportHref } from '@/lib/config';
import { COURSES, DEAL_CATS, SKILLS, SPECS } from '@/lib/data/static';
import { studentCode } from '@/lib/domain';
import { isoLocal, parseYmd, plural, T0 } from '@/lib/format';
import type { Provenance } from '@/lib/types';

export function DealSheet({ id }: { id: string }) {
  const { S, tt, L, content } = useApp();
  const d = content.discounts.find(x => x.id === id);
  if (!d) return null;
  const code = `${d.id.toUpperCase()}-${studentCode(S.profile)}`;
  return (
    <>
      <span className="badge b-line" style={{ alignSelf: 'flex-start' }}>{L(DEAL_CATS[d.cat])}</span>
      <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'baseline' }}><h2>{L(d.name)}</h2><span className="deal-pct">{d.pct}</span></div>
      <p>{L(d.what)}</p>
      <Qr text={'STUDENTHUB-DEAL|' + code} />
      <p style={{ textAlign: 'center' }}><span className="code">{code}</span></p>
      <div className="group"><Row icon="check" tone="ok" title={tt('Шарттары', 'Условия')} sub={L(d.cond)} chev={false} /></div>
      <p className="tiny muted">{d.is_demo
        ? tt('Кассада QR-кодты немесе студенттік мәртебеңізді көрсетіңіз. Серіктес — үлгі.', 'Покажите QR-код или студенческий статус на кассе. Партнёр — пример.')
        : tt('Кассада QR-кодты немесе студенттік мәртебеңізді көрсетіңіз.', 'Покажите QR-код или студенческий статус на кассе.')}</p>
    </>
  );
}

export function CourseSheet({ id }: { id: string }) {
  const { S, tt, L, update, toast, closeSheet } = useApp();
  const c = COURSES.find(x => x.id === id);
  if (!c) return null;
  const done = !!S.done[c.id];
  const toggle = () => {
    if (done) {
      update(s => { delete s.done[c.id]; });
      toast(tt('Алынды', 'Убрано'));
    } else {
      const added = c.skills.filter(k => !S.profile.skills.includes(k));
      update(s => { s.done[c.id] = Date.now(); s.profile.skills.push(...added); });
      toast(added.length ? tt('Сертификат пен дағды профильге қосылды', 'Сертификат и навык добавлены в профиль') : tt('Сертификат қосылды', 'Сертификат добавлен'));
    }
    closeSheet();
  };
  return (
    <>
      <span className="eyebrow">{c.platform}</span>
      <h2>{L(c.title)}</h2>
      <dl className="kv">
        <dt>{tt('Ұзақтығы', 'Длительность')}</dt><dd>{tt(`${c.weeks} апта`, `${c.weeks} ${plural(c.weeks, ['неделя', 'недели', 'недель'])}`)}</dd>
        <dt>{tt('Сертификат', 'Сертификат')}</dt><dd>{c.cert === 'paid' ? tt('Ақылы (оқу тегін)', 'Платный (обучение бесплатно)') : tt('Тегін', 'Бесплатно')}</dd>
        {c.skills.length ? <><dt>{tt('Дағдылар', 'Навыки')}</dt><dd>{c.skills.map(k => L(SKILLS[k])).join(', ')}</dd></> : null}
      </dl>
      <a className="btn" href={c.url} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Платформаны ашу', 'Открыть платформу')}</a>
      <button className={`btn ${done ? 'danger' : 'sun'}`} type="button" onClick={toggle}>
        {done ? tt('Аяқталғаннан алып тастау', 'Убрать из пройденных') : <><Icon n="award" s={18} /> {tt('Аяқтадым — сертификатты профильге қосу', 'Прошёл(ла) — добавить сертификат в профиль')}</>}
      </button>
      <p className="tiny muted">{tt('Курс сыртқы платформада өтеді. Мазмұны мен бағасын платформадан тексеріңіз.', 'Курс проходит на внешней платформе. Содержание и стоимость проверяйте там.')}</p>
    </>
  );
}

export function StudentQrSheet() {
  const { S, tt, L } = useApp();
  const p = S.profile;
  return (
    <>
      <span className="eyebrow">{tt('Цифрлық студенттік', 'Цифровой студенческий')}</span>
      <h2>{p.name}</h2>
      <p className="muted small">{tt('Сейфуллин университеті', 'Университет им. С. Сейфуллина')} · {L(SPECS[p.spec])} · {tt(`${p.course}-курс`, `${p.course} курс`)}</p>
      <Qr text={`STUDENTHUB-STUDENT|${studentCode(p)}|${p.name}|${p.spec}|${p.course}`} />
      <p style={{ textAlign: 'center' }}><span className="code">{studentCode(p)}</span></p>
      <p className="tiny muted">{tt('Прототипте QR-код студент мәртебесін көрсету үшін ғана. Ресми растау университет жүйесі қосылғанда іске қосылады.', 'В прототипе QR-код только демонстрирует статус студента. Официальная проверка появится после подключения системы университета.')}</p>
    </>
  );
}

export function AddCertSheet() {
  const { tt, update, toast, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Сертификат қосу', 'Добавить сертификат')}</h2>
      <form className="form" id="certForm" onSubmit={e => {
        e.preventDefault();
        const f = e.currentTarget;
        const val = (id: string) => (f.querySelector<HTMLInputElement>('#' + id)?.value || '').trim();
        update(s => { s.certs.push({ title: val('ctT'), org: val('ctO'), date: (val('ctD') || isoLocal(T0())) + 'T00:00:00' }); });
        toast(tt('Қосылды', 'Добавлено'));
        closeSheet();
      }}>
        <div className="field"><label htmlFor="ctT">{tt('Атауы', 'Название')}</label><input className="input" id="ctT" required /></div>
        <div className="field"><label htmlFor="ctO">{tt('Ұйым', 'Организация')}</label><input className="input" id="ctO" required /></div>
        <div className="field"><label htmlFor="ctD">{tt('Күні', 'Дата')}</label><input className="input" id="ctD" type="date" defaultValue={isoLocal(T0())} /></div>
        <button className="btn" type="submit">{tt('Қосу', 'Добавить')}</button>
      </form>
    </>
  );
}

/** What «Тексерілген» means for this record: what was checked, by whom and when. */
export function VerifiedSheet({ item, kind, id, path }: { item: Provenance; kind: string; id: string; path: string }) {
  const { tt, fDateY, lang } = useApp();
  const c = item.contact || {};
  const hasContact = !!(c.phone || c.email || c.site);
  const url = window.location.origin + path;
  return (
    <>
      <span className="badge b-ok" style={{ alignSelf: 'flex-start' }}><Icon n="shield" s={12} /> {tt('Тексерілген', 'Проверено')}</span>
      <h2>{tt('Біз нені тексердік', 'Что мы проверили')}</h2>
      <div>
        <div className="res">
          <span className={`dot ${item.is_demo ? 'mid' : 'ok'}`}>{item.is_demo ? '!' : '✓'}</span>
          <div className="row-main">
            <span className="row-title">{tt('Дереккөз', 'Источник')}</span>
            <span className="row-sub">{item.is_demo
              ? tt('Үлгі жазба — ресми дереккөз әлі жоқ.', 'Пример — официального источника пока нет.')
              : tt('Жарияланым ресми дереккөзде бар екені расталды.', 'Подтверждено, что публикация есть в официальном источнике.')}</span>
          </div>
        </div>
        <div className="res">
          <span className={`dot ${hasContact ? 'ok' : 'mid'}`}>{hasContact ? '✓' : '!'}</span>
          <div className="row-main">
            <span className="row-title">{tt('Байланыс деректері', 'Контакты')}</span>
            <span className="row-sub">{hasContact
              ? tt('Телефон, пошта немесе сайт ұйымға тиесілі екені тексерілді.', 'Проверено, что телефон, почта или сайт принадлежат организации.')
              : tt('Байланыс деректері көрсетілмеген — ресми дереккөз арқылы хабарласыңыз.', 'Контакты не указаны — связывайтесь через официальный источник.')}</span>
          </div>
        </div>
        <div className="res">
          <span className="dot ok">✓</span>
          <div className="row-main">
            <span className="row-title">{tt('Алдын ала төлем сұралмайды', 'Предоплату не требуют')}</span>
            <span className="row-sub">{tt('Өтінім беру не жұмысқа орналасу үшін ақша сұралмайды.', 'За заявку или трудоустройство деньги не просят.')}</span>
          </div>
        </div>
      </div>
      <dl className="kv">
        <dt>{tt('Кім тексерді', 'Кто проверил')}</dt><dd>{item.verified_by || '—'}</dd>
        <dt>{tt('Қашан', 'Когда')}</dt><dd>{item.verified_at ? fDateY(parseYmd(item.verified_at)) : '—'}</dd>
      </dl>
      <p className="tiny muted">{tt('Тексеру кепілдік емес. Біреу алдын ала ақша сұраса, төлемеңіз және бізге хабарлаңыз.', 'Проверка — не гарантия. Если у вас просят деньги заранее, не платите и сообщите нам.')}</p>
      <a className="btn ghost" href={reportHref(id, kind, url, lang)} target="_blank" rel="noopener"><Icon n="bell" s={18} /> {tt('Қате не алаяқтық туралы хабарлау', 'Сообщить об ошибке или мошенничестве')}</a>
    </>
  );
}
