# Google арқылы кіру («Sign in with Google»)

Студент кіру терезесінде «Google арқылы кіру / Войти через Google / Continue with Google» батырмасын басады,
Google-да аккаунтын таңдайды және SIVA-ға сол бетке қайтып келеді. Әдепкі бойынша **өшік**: `FEATURE_GOOGLE_LOGIN`,
`GOOGLE_CLIENT_ID` және `GOOGLE_CLIENT_SECRET` үшеуі де қойылмаса, батырма көрінбейді, ал маршруттар белгісіз
маршрут сияқты 404 қайтарады. `GET /api/config` тек `googleLogin: true/false` береді (client id де, secret те емес).

Код: `backend/internal/auth/google.go` (тесттер `google_test.go`), миграция
`backend/migrations/20261108000100_google_login.sql`, фронт `frontend/src/components/views/account.tsx`.

## Қалай жұмыс істейді

Серверлік OAuth 2.0 authorization-code ағыны, PKCE (S256) + `state` + `nonce`, OpenID Connect ID token.

| Маршрут | Не істейді |
| --- | --- |
| `GET /api/auth/google/start?next=/бет[&link=1][&lang=kk\|ru][&ref=…]` | `state`, `nonce`, PKCE verifier жасап, оларды 10 минуттық HMAC-қолтаңбалы HttpOnly cookie-ге (`sh_google`, `Path=/api/auth/google/`, `SameSite=Lax`, `Secure`) салады да, Google-ға (`scope=openid email profile`) 302 жібереді |
| `GET /api/auth/google/callback?code=…&state=…` | cookie-ді тексеріп бірден өшіреді (бір реттік), `state`-ті салыстырады, кодты verifier-мен бірге Google-дың token endpoint-іне айырбастайды, ID token-ді тексереді, кіргізеді де `next`-ке 303 қайтарады |
| `DELETE /api/me/google` | Баптаулардан Google-ды ажырату; кірудің соңғы жолы болса 409 `last_method` |

ID token тексерісі (`VerifyGoogleIDToken`): RS256, қолтаңба Google JWKS кілтімен
(`https://www.googleapis.com/oauth2/v3/certs`, `Cache-Control` бойынша кэш, 5 мин … 24 сағ; белгісіз `kid` —
30 секундта бір рет қайта жүктеу), `iss` = `https://accounts.google.com` не `accounts.google.com`, `aud` = біздің
client id (бірнеше aud болса `azp` те), `exp`/`iat` (1 минут ауытқу), `nonce` cookie-дегімен бірдей, `sub` бар,
**`email_verified = true`**. Расталмаған Google email ешнәрсеге қолданылмайды (`?google=email_unverified`).

Аккаунт ережесі:

1. `google_sub` бұрын байланған болса — сол аккаунтқа кіреді (Google-дағы email өзгерсе де: `sub` тұрақты).
2. Әйтпесе сол **расталған** email-і бар аккаунт болса — соған кіреді, `google_sub` байланады (`audit_log`:
   `google_link`). Ол аккаунтта басқа Google тұрса — `?google=mismatch`.
3. Әйтпесе email арқылы тіркелу сияқты жаңа аккаунт ашылады: email Google растаған күйде, келісім (`consent_at`)
   әлі жоқ, сондықтан қайтып келгенде келісім терезесі ашылады. Аяқталмаған email тіркелуі (код енгізілмеген) жол
   береді — `POST /api/me/email/confirm`-дағыдай.
4. `ADMIN_EMAILS` Google растаған email-ге де қолданылады, `role_grant` бір рет жазылады.
5. Өшірілген аккаунт кірмейді (`?google=blocked`).

`next` — тек осы сайттың жолы (`/…`): схема, хост, `//`, `\`, басқару таңбалары, `/api/…` болса `/` қойылады;
callback әрқашан салыстырмалы `Location` береді — ашық redirect жоқ. Нәтиже `?google=<күй>` болып келеді, фронт оны
хабарға айналдырып, адрестен алып тастайды: `ok`, `linked`, `cancelled`, `expired`, `state`, `invalid`,
`unavailable`, `email_unverified`, `taken`, `mismatch`, `email_taken`, `blocked`, `session`, `rate_limited`.

Байланыстыру (Баптаулар → Аккаунт): кірген студент «Google-ды байланыстыру» басады (`start?link=1`); flow cookie
ішінде оның id-і қолтаңбамен сақталады, callback-та сол сессия болуы керек. Басқа аккаунттағы Google —
`?google=taken` (аккаунттар біріктірілмейді). Email-сіз (тек Telegram) аккаунт Google растаған email-ді де алады,
егер оны басқа нақты аккаунт ұстамаса. Ажырату Telegram не «расталған email + құпиясөз» қалса ғана болады.

Шектер: `start` пен `callback` — IP бойынша 15 минутта 30; `DELETE /api/me/google` — `auth.Write` (Origin + жазу
шегі). Ішкі Telegram Mini App-та батырма жасырылған: Google ендірілген WebView-та кіруге рұқсат бермейді.

## Иесі жасайтын қадамдар (Google Cloud Console)

1. <https://console.cloud.google.com/> → жоғарыдағы жоба таңдаушы → **New Project** → Project name: `SIVA` →
   **Create**. Жаңа жобаны таңдаңыз.
2. **APIs & Services → OAuth consent screen** (жаңа интерфейсте **Google Auth Platform → Branding**) →
   **Get started**:
   - App name: `SIVA`
   - User support email: өз адресіңіз (мысалы `raissov1@gmail.com`)
   - Audience: **External**
   - Contact information (developer email): өз адресіңіз → **Create**.
3. **Branding** бетінде: App logo (міндетті емес, 120×120 PNG), App home page
   `https://studenthub.studywithraissov.com`, Privacy policy `https://studenthub.studywithraissov.com/privacy`,
   **Authorized domains → Add domain**: `studywithraissov.com` → **Save**.
4. **Data Access** (ескі интерфейсте **Scopes**) → **Add or remove scopes** → `openid`, `.../auth/userinfo.email`,
   `.../auth/userinfo.profile` (үшеуі де «non-sensitive», Google тексеруі керек емес) → **Update** → **Save**.
5. **Clients** (ескі интерфейсте **Credentials → Create credentials → OAuth client ID**) → **Create client**:
   - Application type: **Web application**, Name: `SIVA web`
   - Authorized JavaScript origins: керек емес (ағын толық серверде)
   - **Authorized redirect URIs → Add URI**:
     - `https://studenthub.studywithraissov.com/api/auth/google/callback`
     - `http://localhost:3000/api/auth/google/callback` (жергілікті `next dev`; басқа порт қолдансаңыз — соны)
   - **Create** → шыққан **Client ID** (`…apps.googleusercontent.com`) мен **Client secret** (`GOCSPX-…`) көшіріп
     алыңыз. Secret-ті чатқа, репозиторийге, скриншотқа салмаңыз.
6. **Audience** → Publishing status **Testing** → **Publish app** → **Confirm** («In production»). Testing күйінде
   тек «Test users» тізіміндегілер кіре алады. Тек openid/email/profile сұрайтындықтан, жариялау тексерусіз өтеді.

## Сервер баптаулары

`/opt/studenthub/.env` (серверде) және ноутбуктағы `.secrets/prod.env` (көшірмесі, репозиторийге кірмейді) — екеуіне
де бірдей:

```bash
FEATURE_GOOGLE_LOGIN=true
GOOGLE_CLIENT_ID=<Client ID>.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=<Client secret>
# міндетті емес: әдепкісі SITE_URL + /api/auth/google/callback
# GOOGLE_REDIRECT_URL=https://studenthub.studywithraissov.com/api/auth/google/callback
```

`SITE_URL=https://studenthub.studywithraissov.com` болуы керек (redirect URI одан құралады; Console-дағы URI-мен
әріпке дейін бірдей болмаса, Google `redirect_uri_mismatch` береді).

Қосу: файлды `cp -p .env .env.bak-$(date +%F)` деп сақтап, жолдарды қосыңыз, сосын API контейнерін env-ді қайта
оқитындай қайта жасаңыз (ноутбуктан әдеттегі деплой, не серверде
`cd /opt/studenthub/docker && docker compose up -d --force-recreate api`). Тексеру:

```bash
curl -s https://studenthub.studywithraissov.com/api/config | grep -o '"googleLogin":[a-z]*'   # "googleLogin":true
curl -sI 'https://studenthub.studywithraissov.com/api/auth/google/start?next=/' | grep -i '^location'  # accounts.google.com/…
```

Өшіру: `FEATURE_GOOGLE_LOGIN=false` (не жолды өшіру) + API-ді қайта жасау. Байланған `google_sub`-тар қалады,
қайта қосқанда сол аккаунттарға кіреді.

Жергілікті: `backend/` ішінде `SITE_URL=http://localhost:3000 FEATURE_GOOGLE_LOGIN=true GOOGLE_CLIENT_ID=…
GOOGLE_CLIENT_SECRET=… go run ./cmd/server`, фронт `npm run dev` (:3000, `/api` → :8080).

## Android қосымшасы (дизайн ғана, әлі жасалмаған)

Нативті қосымшада Google кіруінің екі жолы бар; екеуі де осы серверлік тексеруді қайта пайдаланады.

**A. Custom Tab → `/start` → App Link (ұсынылады, сервер өзгерісі аз).** Қосымша Chrome Custom Tab-та
`https://studenthub.studywithraissov.com/api/auth/google/start?next=/app/return` ашады (WebView емес: Google
WebView-ты бұғаттайды). Ағын браузерде толық өтеді, callback cookie орнатып `/app/return?google=ok`-қа қайтарады.
`/app/return` — Android App Link (`assetlinks.json` сайтта, қосымшада `autoVerify` intent-filter), сондықтан Custom
Tab қосымшаға қайтады. Бірақ сессия cookie-і Custom Tab-тың cookie қоймасында қалады, қосымшаның өзінде емес;
сондықтан қосу керек: callback `next` = `/app/return` болғанда cookie орнына бір реттік, 60 секундтық, бір
қолданылатын `code` (кездейсоқ, сервер кестесінде хэші) береді, қосымша оны `POST /api/auth/app/exchange`-ке
жіберіп, Mini App сияқты Bearer токен алады (`X-Session-Token` жаңартуы бар). Код ұрланбас үшін қосымша да өз PKCE
жұбын жасап, `start`-қа `app_challenge` береді, `exchange`-те `app_verifier` береді.

**B. Бөлек Android OAuth client + ID token айырбасы.** Google Cloud-та **Android** түрлі client (package name +
SHA-1 қолтаңба) жасалады; қосымша Credential Manager (`GetGoogleIdOption`, `serverClientId` = біздің **Web** client
id) арқылы ID token алады, `nonce`-ты серверден сұрайды (`POST /api/auth/google/nonce` → қысқа мерзімді, бір
реттік, серверде сақталған). Қосымша токенді `POST /api/auth/google/token {idToken, nonce}`-ке жібереді; сервер
`VerifyGoogleIDToken`-ді сол күйінде шақырады (`aud` = Web client id, өйткені `serverClientId` солай) және
`googleSignIn` ережелерімен кіргізіп, Bearer токен қайтарады. Бұл жолда браузер ашылмайды, бірақ Play Services
керек және екінші client-ті баптау қажет.

Екі жағдайда да: расталмаған email қабылданбайды, аккаунттарды байланыстыру/ажырату ережелері осы файлдағыдай,
Android-тағы токен тек жадта не EncryptedSharedPreferences-те сақталады, `/api/auth/logout` оны серверде жабады.
