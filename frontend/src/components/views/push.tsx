'use client';
/* Settings → «Хабарламалар» → «Браузер хабарламалары» (docs/PUSH.md). Shown only when the server has Web Push on
   (/api/config webPush.enabled). The browser asks for permission only when the student turns the switch on; nothing
   happens on page load except reading this browser's current subscription. */
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { ErrorLine, errText } from '@/components/views/common';
import { useConfig } from '@/lib/config';
import { currentSubscription, disablePush, enablePush, isIOS, permission, PushError, pushSupport, testPush } from '@/lib/push';

type State = 'loading' | 'on' | 'off';

export function PushToggle() {
  const cfg = useConfig();
  if (!cfg.webPush.enabled) return null;
  return <PushToggle_ publicKey={cfg.webPush.publicKey} />;
}

function PushToggle_({ publicKey }: { publicKey: string }) {
  const { tt, lang, toast } = useApp();
  const [state, setState] = useState<State>('loading');
  const [support, setSupport] = useState<ReturnType<typeof pushSupport>>('ok');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    const s = pushSupport();
    currentSubscription().then(sub => {
      if (!live) return;
      setSupport(s);
      setState(sub && permission() === 'granted' ? 'on' : 'off');
    }, () => { if (live) { setSupport(s); setState('off'); } });
    return () => { live = false; };
  }, []);

  const turn = async (on: boolean) => {
    setBusy(true);
    setError('');
    try {
      if (on) {
        await enablePush(publicKey, lang);
        setState('on');
        toast(tt('Браузер хабарламалары қосылды', 'Уведомления в браузере включены'));
      } else {
        await disablePush(lang);
        setState('off');
      }
    } catch (e) {
      setState(on ? 'off' : 'on');
      if (e instanceof PushError && e.code === 'denied') setError(tt('Браузер рұқсат бермеді. Сайт баптауларынан хабарламаларға рұқсат беріңіз.', 'Браузер не дал разрешение. Разрешите уведомления в настройках сайта.'));
      else if (e instanceof PushError) setError(tt('Бұл браузерде хабарлама қосылмайды.', 'В этом браузере уведомления не включаются.'));
      else setError(errText(e, tt('Болмады. Кейінірек қайталаңыз.', 'Не получилось. Попробуйте позже.')));
    } finally { setBusy(false); }
  };

  const test = async () => {
    setBusy(true);
    setError('');
    try {
      const r = await testPush(lang);
      toast(r.delivered ? tt('Тексеру хабарламасы жіберілді', 'Проверочное уведомление отправлено') : tt('Хабарлама жеткізілмеді', 'Уведомление не доставлено'));
    } catch (e) { setError(errText(e, tt('Болмады. Кейінірек қайталаңыз.', 'Не получилось. Попробуйте позже.'))); }
    finally { setBusy(false); }
  };

  const blocked = permission() === 'denied';
  const disabled = busy || state === 'loading' || support !== 'ok' || (blocked && state !== 'on');
  return (
    <div className="group" id="push-settings">
      <label className="row toggle-row">
        <span className="row-main"><span className="row-title">{tt('Браузер хабарламалары', 'Уведомления в браузере')}</span>
          <span className="row-sub">{tt('Бот орнына осы құрылғыға келеді. Санаттар, тыныш уақыт және күндік шек бірдей.', 'Приходят на это устройство вместо бота. Категории, тихие часы и дневной лимит — те же.')}</span></span>
        <input type="checkbox" id="pushToggle" checked={state === 'on'} disabled={disabled} onChange={e => turn(e.target.checked)} />
      </label>
      {support === 'ios-install'
        ? <p className="note" id="pushIos">{tt('iPhone мен iPad-та хабарлама тек басты экранға орнатылған қолданбада жұмыс істейді (iOS 16.4 және жаңасы): Safari → «Бөлісу» → «Басты экранға қосу», сосын қолданбадан қосыңыз.', 'На iPhone и iPad уведомления работают только в приложении на главном экране (iOS 16.4 и новее): Safari → «Поделиться» → «На экран „Домой“», затем включите их в приложении.')}</p>
        : support === 'unsupported'
          ? <p className="note">{tt('Бұл браузер хабарламаларды қолдамайды.', 'Этот браузер не поддерживает уведомления.')}{isIOS() ? ' ' + tt('iOS 16.4 не жаңасы керек.', 'Нужна iOS 16.4 или новее.') : ''}</p>
          : blocked && state !== 'on'
            ? <p className="note">{tt('Хабарламалар браузерде бұғатталған. Сайт баптауларынан рұқсат беріңіз.', 'Уведомления заблокированы в браузере. Разрешите их в настройках сайта.')}</p>
            : null}
      {state === 'on' ? <button type="button" className="btn ghost" id="pushTest" disabled={busy} onClick={test}>{tt('Тексеру хабарламасын жіберу', 'Отправить проверочное уведомление')}</button> : null}
      <ErrorLine err={error} />
    </div>
  );
}
