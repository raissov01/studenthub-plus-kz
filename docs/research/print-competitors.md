# Print module: competitor research

Student Hub print (KazATU copy shop, one operator). Research date: **2026-11-05**.

## 1. Scope and method

- **Question:** what campus print systems, web-to-print shops and CIS/KZ copy centres do around ordering, files,
  pricing, payment, pickup, notifications and operator tools, and what users complain about. The goal is to rank ideas
  for our module.
- **Rules followed** (from `replica-recon` / `replica-entrepreneur`): public pages only, no logins, no scraping or
  bulk downloads. Pages were read one at a time with a fetch tool. No invented quotes, ratings or counts. Every claim
  links to its source.
- **How quotes were taken:** the fetch tool turns a page into text, and quotes were copied from that text. A few
  App Store excerpts are cut short (marked `…`), because the tool returns at most about 125 characters per quote.
  Review-site quotes keep their original language.
- **Limits, stated honestly:**
  - Reddit could not be reached through search (`site:reddit.com` returned nothing useful), so there are no Reddit
    quotes. The one Reddit-derived item is a third-party mirror and is not used.
  - Several vendor manual pages (PaperCut Web Print, Pharos, PrinterOn public edition, WEPA user guide, Staples
    same-day) returned 403/404, a redirect loop, or navigation-only text. For those products the features come from
    university help pages that document their own deployment, and settings differ by campus.
  - 2GIS Astana reviews have no dates or per-review stars in the fetched text.
  - No public Telegram print bot for a staffed copy shop with an order queue was found. Only DIY "print to my
    printer" bots and one self-service kiosk that accepts files through a bot.
  - Review samples are small and self-selected. Nothing below is statistically representative.

## 2. Sources

| # | Product | URL | Type | Accessed |
|---|---|---|---|---|
| S1 | PaperCut Web Print (Ithaca College deployment) | https://help.ithaca.edu/TDClient/34/Portal/KB/ArticleDet?ID=1842 | campus help doc | 2026-11-05 |
| S2 | PaperCut Job Ticketing: FAQ | https://papercut.com/help/manuals/job-ticketing/faq | vendor manual | 2026-11-05 |
| S3 | PaperCut Job Ticketing: managing orders | https://papercut.com/help/manuals/job-ticketing/managing-orders | vendor manual | 2026-11-05 |
| S4 | PaperCut Job Ticketing: placing an order | https://papercut.com/help/manuals/job-ticketing/placing-an-order | vendor manual | 2026-11-05 |
| S5 | PaperCut Mobility Print (Chrome Web Store reviews) | https://chromewebstore.google.com/detail/mobility-print/alhngdkjgnedakdlnamimgfihgkmenbh/reviews | store reviews | 2026-11-05 |
| S6 | PaperCut forum: preview vs charge | https://www.edugeek.net/forums/topic/181709-papercut-double-charging-for-html-documents/ | sysadmin forum (search snippet only) | 2026-11-05 |
| S7 | WEPA (Towson University deployment) | https://www.towson.edu/wepa | campus help doc | 2026-11-05 |
| S8 | WEPA (UAH deployment) | https://uah.teamdynamix.com/TDClient/2075/Portal/KB/Article/67076/Charger-Prints-Printing-with-the-Wepa-Print-App | campus help doc | 2026-11-05 |
| S9 | WEPA Print iOS app reviews | https://apps.apple.com/us/app/id1448454304?see-all=reviews | App Store | 2026-11-05 |
| S10 | Pharos Print Center (UW Oshkosh) | https://uwosh.atlassian.net/wiki/spaces/IT/pages/2079719433/Student+Printing+-+Pharos+Print+Center+Desktop+and+Mobile | campus help doc | 2026-11-05 |
| S11 | PrinterOn workflow options | https://documentation.printeron.com/help/configuring-printer-specific-workflow-options | vendor docs | 2026-11-05 |
| S12 | GoPrint (University of Windsor) | https://uwindsor.teamdynamix.com/TDClient/KB/ArticleDet?ID=48872 | campus help doc | 2026-11-05 |
| S13 | FedEx Office Print Online: Upload & Print quick reference guide (2013) | https://procurement.ufl.edu/wp-content/uploads/2018/01/FedExOffice_POLC_3-6_QRG-UploadandPrint.pdf | vendor guide (PDF) | 2026-11-05 |
| S14 | FedEx Office Salt Lake City store reviews | https://local.fedex.com/en-us/ut/salt-lake-city/office-2401 | store reviews | 2026-11-05 |
| S15 | FedEx Office Bellevue store reviews | https://local.fedex.com/en-us/wa/bellevue/office-5725 | store reviews | 2026-11-05 |
| S16 | FedEx Office Silver Spring store reviews | https://local.fedex.com/en-us/md/silver-spring/office-0227 | store reviews | 2026-11-05 |
| S17 | Proof Server: FedEx print order tracking notifications | https://help.proofserve.com/server/en/articles/11084683-new-fedex-print-order-tracking-notifications | partner help doc | 2026-11-05 |
| S18 | Staples Canada (Trustpilot, page 2) | https://ca.trustpilot.com/review/www.staples.ca?page=2 | Trustpilot | 2026-11-05 |
| S19 | Copiet (India copy-shop ordering) | https://printify.duckdns.org/ | product site (unofficial-looking domain) | 2026-11-05 |
| S20 | ЛистОк (St Petersburg self-service copy point) | https://yandex.com/maps/org/223357855216/ | Yandex Maps card + reviews | 2026-11-05 |
| S21 | Copy.ru (Russian copy centre) price list | https://yandex.com/maps/org/copy_ru/214010224554/prices/ | Yandex Maps prices | 2026-11-05 |
| S22 | Фотосалон, Astana (copy services) | https://2gis.kz/astana/firm/70000001019716586/tab/reviews | 2GIS reviews | 2026-11-05 |

Not reached: wepanow.com/user-guide (403), staples.com same-day page (403), printeron.com public-edition (redirect
loop), kb.indiana.edu (DNS), karandash.by (TLS error), Stevens Pharos KB (title only).

## 3. Per-product notes

**PaperCut Web Print** (S1). Log in, choose a queue ("Black and White or Color"), set copies "1 to 25", upload, then
release the job at a campus printer with an ID card tap. File types: Word/Excel/PowerPoint, PDF, images. Other types
are rejected. The Ithaca guide mentions no duplex, page range, cost display or preview. Jobs "stay in the queue for
72 hours" and are then deleted. A sysadmin forum reports charges that don't match the preview: "costs are inaccurate
even if you confirm the number of pages in print preview" (S6, search snippet only, not fetched).

**PaperCut Job Ticketing** (S2–S4). The closest analogue to us: a print-room order form plus an operator interface.
- **Customers** pick from a product catalogue, approve a quote or proof, comment, and cancel (S4).
- **Operators** get a "Workflow" view, "a visual display of orders sorted by status", and an "Order List" (S3).
  Operator tasks: change status, comment, internal notes, download files, **print a job ticket**, charge, cancel and
  refund.
- **Limitations, stated in the FAQ (S2):**
  - "Job Ticketing does not convert the supplied files in anyway"
  - "you cannot resubmit a previous order"
  - "If a quote is required, you should add the quote using a comment."
  - "only the financial transactions against Job Ticketing accounts are tracked"
  - "Job Ticketing retains all customer supplied files indefinitely."

**PaperCut Mobility Print** (S5). A driver or extension for BYOD printing, with no ordering UI. The Chrome Web Store
shows 2.9/5 from 36 ratings. Complaints are about reliability, not features (section 5).

**WEPA** (S7–S9). A cloud kiosk system.
- **Upload:** driver, app, web, or email to print@wepanow.com. Email keywords set options: "duplex", "color", and
  "handouts3" for slides per page (S7).
- **Release:** at the kiosk with a campus card, "a release code", or an account (S8).
- **Pricing:** published per-page table (Towson: B/W 10¢, colour 59¢). Account deposit "minimum … is $5.00"; a 40¢
  per-transaction card fee (S7).
- **Reviews:** iOS app 1.5/5 from 466 ratings (S9).

**Pharos Print Center** (S10). Web upload with SSO. The defaults are "Black & White and Double Sided"; release at a
card reader next to the printer. You "must be on campus to log in". The mobile app was paused waiting on SSO support.
Hold time was not in the fetched text.

**PrinterOn** (S11). Web Print portal, email printing with a per-printer address, mobile and IPP. An optional release
code covers a whole email job ("One Release Code for All Attachments"). The admin chooses whether the email body
prints.

**GoPrint** (S12). Browser submit ("Any computer that has a Web browser can submit a job"). At the release station
the "total cost will be shown" before you swipe the card. Unreleased jobs are deleted after 30 minutes.

**FedEx Office Print Online** (S13–S17). The richest ordering UX of the set.
- **Upload:** up to 100MB per file. You can "combine files to create single document" or keep them separate.
- **Live preview and price:** "Document Preview and Estimated Price will dynamically update to reflect your
  selections". The preview "is your online proof of the finished document."
- **Fallback for odd files:** for special instructions or non-standard sizes, a staff member "will contact you within
  30 minutes" and full preview or pricing may be missing. This is the same idea as our Office-file `review` state.
- **Reorder:** "Documents are available to reorder for a period of six months".
- **Pickup:** choose a centre and a pickup time (S13).
- **Tracking (S17):** a partner integration has statuses "Processing / Ready for Pickup / Completed" and push alerts
  when the order is confirmed, ready, or failed. Store reviews show notifications do not always arrive (section 5).

**Staples print online** (S18). Upload with same-day or standard service. The main Trustpilot complaints are orders
routed to the wrong service level and missing orders at pickup. The Staples same-day policy page returned 403, so its
cutoff times are not quoted here.

**Copiet** (S19). An India copy-shop app. The student finds a shop or scans its QR code, uploads "PDFs, Word docs, or
images", picks size, colour and copies, and sees "transparent pricing" before confirming. The shop marks the job
'Ready' and the student collects it. The site sits on a duckdns.org subdomain. No independent reviews were found and
payment is not described, so treat it as a concept reference, not proof that it works.

**ЛистОк, St Petersburg** (S20). A self-service A4 B/W machine. Files arrive by "USB, telegram-бот или web-сервис";
payment by card or QR. Price is 20 ₽ per page, 13 ₽ per page from 10 pages. 4.1 from 41 ratings, 25 reviews. This is
the only public example found of a copy point that takes files through a Telegram bot.

**Copy.ru, Russia** (S21). Two speeds: "Печать в салоне — сразу, онлайн — от 2 часов". Binding services show
"Онлайн-расчёт" (an online price calculator), "Заказ онлайн со скидкой 5%", pickup, and courier or CDEK delivery.
Diploma binding: one day, or 2–4 h urgent.

**Astana copy centres on 2GIS** (S22 plus search results). The typical KZ flow is: send the file on WhatsApp, pay by
QR at the counter. The sample shop (Фотосалон, Astana) lists two WhatsApp numbers and no web ordering; 4.8 from 156
ratings, 134 reviews. This matches our own problem statement in `docs/print.md` (files sent to the operator one by
one over WhatsApp).

## 4. Comparison matrix

Legend: Y = yes, N = no, P = partial or deployment-dependent, ? = not verified.

"Ours" uses the brief, cross-checked with `docs/print.md` and `backend/internal/printshop/notify.go`. Two corrections
to the brief, found in the code:
- A **spoken pickup code** already exists: Telegram says "Назовите код" with `№<PickupCode>`. There is no QR and no
  scan-to-verify at the counter.
- **Queue position as a count** exists ("кезектегі орын — ертерек кезекке тұрған ашық тапсырыстар саны"). There is
  no ETA.

| Feature | Ours | PaperCut Web Print | PaperCut Job Ticketing | WEPA | Pharos | GoPrint | FedEx Online | Staples | Copiet | ЛистОк | Copy.ru | Astana WhatsApp shop |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Web upload | Y | Y | Y | Y | Y | Y | Y | Y | Y | Y | ? | N (WhatsApp) |
| Upload via messenger/email | N | N | N | Y (email) | P (email) | N | N | N | N | Y (Telegram bot) | ? | Y (WhatsApp) |
| Server page count (PDF) | Y | P | N | ? | ? | Y (at release) | Y | ? | ? | ? | ? | N |
| Office files handled | P (declared + operator confirms) | Y (converted) | N ("does not convert") | Y | Y | Y | Y | Y | Y | DOC/DOCX | ? | manual |
| Page thumbnails / proof before pay | **N** | N | P (proof via operator) | ? | ? | N | **Y** (live proof) | ? | ? | N | ? | N |
| Per-page colour | **N** | N | ? | N | N | N | ? | ? | N | N (B/W only) | ? | manual |
| Page range | Y | N | ? | ? | ? | ? | ? | ? | ? | ? | ? | manual |
| Duplex / size / copies | Y | P (queue = colour) | Y (form) | Y | Y | P | Y | Y | Y | A4 only | Y | manual |
| Binding / lamination / goods | Y | N | Y (catalogue) | N | N | N | Y | Y | ? | N | Y | Y (manual) |
| Price before paying | Y (after upload) | N | P (quote by comment) | Y (table) | ? | Y (at station) | Y (live) | Y | Y | Y (table) | Y | N (asked in chat) |
| Calculator without upload | **N** | N | N | P (static table) | N | N | N | ? | ? | P (table) | Y ("Онлайн-расчёт") | N |
| Operator price change + student approval | Y | N | Y (quote approval) | N | N | N | P (staff calls) | ? | ? | N | ? | chat |
| Payment | Kaspi / counter | account | account | account / card / PayPal etc. | account | campus card | card | card | ? | card / QR | ? | Kaspi QR (typical) |
| Pickup slot / time | Y | N | Y (needed-by date) | N | N | N | Y | Y | N | N | Y | N |
| Pickup code / QR | P (spoken code) | N (card) | ? | Y (release code) | N (card) | N (card) | ? | ? | Y (shop QR to connect) | N | ? | N |
| Queue position / ETA | P (count only) | N | N | N | N | N | N | N | P (live tracking) | N | P (fixed "от 2 часов") | N |
| Ready notification | Y (Telegram + site) | N | P (email, off by default) | N | N | N | Y (email / push, inconsistent) | Y (inconsistent) | Y | N | ? | WhatsApp |
| Reorder / print again | **N** | N | N ("cannot resubmit") | P (file stays in cloud) | N | N | **Y** (6 months) | ? | ? | N | ? | N |
| Saved presets | **N** | N | N | P (email keywords) | P (defaults) | N | P (account defaults) | ? | ? | N | ? | N |
| Operator board | Y (tablet) | N/A | Y (Workflow + list) | N/A | N/A | N/A | internal | internal | Y | N/A | ? | phone |
| Operator bulk actions | **N** | N/A | ? | N/A | N/A | N/A | ? | ? | ? | N/A | ? | N |
| Job ticket / merged print-ready PDF | **N** | N | P (printable job ticket) | N | N | N | Y (combine files) | ? | ? | N | ? | N |
| Daily report | **N** | admin reports | P (transaction log only) | ? | ? | ? | ? | ? | ? | ? | ? | N |
| Rating after pickup | **N** | N | N | N | N | N | external review site | external | ? | external | external | external (2GIS) |
| File retention / expiry | Y (deleted after retention) | 72 h (Ithaca) | **indefinite** | ? | ? | 30 min | 6 months (for reorder) | ? | "encrypted" (claim) | ? | ? | lives in WhatsApp |
| Signed / private downloads | Y | N/A | ? | ? | ? | N/A | ? | ? | claimed | ? | ? | N |
| Hold-and-release at printer | N (staffed counter) | Y | N | Y | Y | Y | N | N | N | self-service | N | N |

## 5. Top user complaints (verbatim)

**A. No word that the order is ready, or vague timing.** This is the most common theme for staffed shops.
- "I did not receive a notification through email or by phone that my order was ready for pickup." (FedEx Salt Lake
  City, 2025-08-21) [S14](https://local.fedex.com/en-us/ut/salt-lake-city/office-2401)
- "There was also no way to contact the FedEx location that I placed the order with" (same review) [S14](https://local.fedex.com/en-us/ut/salt-lake-city/office-2401)
- "There was no communication on when my order would be ready, and the team members did not help." (FedEx Bellevue,
  2026-05-19, 1★) [S15](https://local.fedex.com/en-us/wa/bellevue/office-5725)
- "I did not receive a call to let me know its status so I called myself at 3pm and was told it would be in 2 hrs."
  (FedEx Bellevue, 2026-03-30, 2★) [S15](https://local.fedex.com/en-us/wa/bellevue/office-5725)

**B. Order lost, wrong service level, or never done.**
- "Ordered a document print (online) on 14 December 2025, still nothing today." (Staples CA, 2026-01-05, 1★) [S18](https://ca.trustpilot.com/review/www.staples.ca?page=2)
- "my order had been processed as a standard order" (Staples CA, 2026-07-25, 1★, a stamp order, not documents) [S18](https://ca.trustpilot.com/review/www.staples.ca?page=2)
- "then they couldn't even find my order" (Staples CA, 2026-08-14, 1★, online banner pickup) [S18](https://ca.trustpilot.com/review/www.staples.ca?page=2)

**C. Output differs from what was set (orientation, duplex, preview mismatch).**
- "the pages always came flipped on the short side making the pages unusable." (WEPA iOS, 2024-06-04, title
  "Terrible, don't recommend.") [S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)
- "costs are inaccurate even if you confirm the number of pages in print preview" (PaperCut, EduGeek forum;
  search snippet, page not fetched) [S6](https://www.edugeek.net/forums/topic/181709-papercut-double-charging-for-html-documents/)

**D. Upload and app friction.**
- "The app does not let you print a PDF file from your phone" (WEPA iOS, Oct 16) [S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)
- "Takes far too long to log in and access what you want to print" (WEPA iOS, 2024-02-28) [S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)
- "Application does not even work." (Mobility Print, 2023-09-04) [S5](https://chromewebstore.google.com/detail/mobility-print/alhngdkjgnedakdlnamimgfihgkmenbh/reviews)
- "randomly can't find printers." (Mobility Print, 2023-01-28) [S5](https://chromewebstore.google.com/detail/mobility-print/alhngdkjgnedakdlnamimgfihgkmenbh/reviews)

**E. Paying for more than you print (prepaid wallets).**
- "you can't withdraw left over funds, nor can you just pay for the pages you need." (WEPA iOS, 2022-10-18, title
  "Spent $5 to print 3 pages") [S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)

**F. Local KZ counter experience: price and staff.** (2GIS; no dates or stars in the fetched text)
- "Ужасно Дорого!" [S22](https://2gis.kz/astana/firm/70000001019716586/tab/reviews)
- "Ужасно грубые муж с женой, научитесь разговаривать с клиентами." [S22](https://2gis.kz/astana/firm/70000001019716586/tab/reviews)
- Positive, for contrast: "Всё сделали буквально за пару минут, качество печати отличное, буквы четкие." [S22](https://2gis.kz/astana/firm/70000001019716586/tab/reviews)

**G. Self-service payment failure.** ЛистОк, 2025-07-14. The fetched text only paraphrases this review, so it is not
quoted: the reviewer reports a payment problem that stopped an urgent print. [S20](https://yandex.com/maps/org/223357855216/)

What this means for us:
- Themes A and B are the strongest signal for a one-operator shop. Status, timing and "where is my order" matter more
  than print features.
- Theme C says to show what will print before money moves.
- Theme E supports pay-per-order (Kaspi or counter) over a wallet. That is a positioning point we already have.

## 6. Ranked ideas

**Scoring:** Impact (1–5) + Fit for a one-operator KazATU shop (1–5) + Evidence strength (1–3) − Cost (S=0, M=1, L=2).
Maximum is 13. Cost is relative to our stack: Go API with a 150 MB memory limit, no LibreOffice, Telegram infra
already in place.

| Rank | Idea | Evidence | Cost | Fit notes | I | F | E | Score |
|---|---|---|---|---|---|---|---|---|
| 1 | **ETA on top of the queue count.** Show "≈ N min" from pages ahead × operator pace, and let the operator push "delayed until HH:MM". | Complaint A ([S15](https://local.fedex.com/en-us/wa/bellevue/office-5725)); Copy.ru gives only a fixed "от 2 часов" ([S21](https://yandex.com/maps/org/copy_ru/214010224554/prices/)) | S | Queue count already exists. One operator means pace is easy to estimate from history. | 4 | 5 | 3 | 12 |
| 2 | **Complete the Telegram status set:** accepted, printing started, delayed (with reason), ready, reminder before no-show, file deleted. | Complaint A ([S14](https://local.fedex.com/en-us/ut/salt-lake-city/office-2401), [S15](https://local.fedex.com/en-us/wa/bellevue/office-5725)); FedEx partner has confirmed / ready / failed push ([S17](https://help.proofserve.com/server/en/articles/11084683-new-fedex-print-order-tracking-notifications)) | S | Infra exists (`notify.go`); "ready" and price messages are already there. | 4 | 5 | 3 | 12 |
| 3 | **PDF page thumbnails and proof before paying.** Render in the browser with pdf.js, so no server load. Show orientation and the duplex flip edge. | FedEx "online proof" ([S13](https://procurement.ufl.edu/wp-content/uploads/2018/01/FedExOffice_POLC_3-6_QRG-UploadandPrint.pdf)); complaint C ([S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)) | M | Client-side keeps the API inside its memory limit. Office files keep the `review` path. | 5 | 4 | 3 | 11 |
| 4 | **Upload through the existing Telegram bot.** The student sends the file to the bot, which opens the order with the file attached. | ЛистОк takes files by Telegram bot ([S20](https://yandex.com/maps/org/223357855216/)); WEPA and PrinterOn email-to-print ([S7](https://www.towson.edu/wepa), [S11](https://documentation.printeron.com/help/configuring-printer-specific-workflow-options)); local habit is sending files over WhatsApp ([S22](https://2gis.kz/astana/firm/70000001019716586/tab/reviews)) | M | Meets students where they already are. Same validation pipeline; watch the Bot API file-size limit. | 4 | 5 | 3 | 11 |
| 5 | **Merged print-ready PDF plus job-ticket cover page per order.** All files in order, options, code, name, slot; operator prints one file. | FedEx "combine files to create single document" ([S13](https://procurement.ufl.edu/wp-content/uploads/2018/01/FedExOffice_POLC_3-6_QRG-UploadandPrint.pdf)); PaperCut operator task "print a job ticket" ([S3](https://papercut.com/help/manuals/job-ticketing/managing-orders)) | M | Saves the single operator many clicks. PDF-only merge; Office files get a cover sheet only. | 4 | 5 | 2 | 10 |
| 6 | **Price calculator without upload.** Pages × options → total, reusing the `quote` pricing with typed page counts. Can be public on the print landing page. | Copy.ru "Онлайн-расчёт" ([S21](https://yandex.com/maps/org/copy_ru/214010224554/prices/)); WEPA published table ([S7](https://www.towson.edu/wepa)); GoPrint "total cost will be shown" ([S12](https://uwindsor.teamdynamix.com/TDClient/KB/ArticleDet?ID=48872)); complaint F (price) | S | Answers "how much?" before the student commits. The server stays the price authority. | 3 | 5 | 2 | 10 |
| 7 | **QR at pickup** (keep the spoken code). Student shows a QR; the operator tablet scans it and marks "Берілді". | WEPA release code ([S8](https://uah.teamdynamix.com/TDClient/2075/Portal/KB/Article/67076/Charger-Prints-Printing-with-the-Wepa-Print-App)); PrinterOn release code ([S11](https://documentation.printeron.com/help/configuring-printer-specific-workflow-options)); "couldn't even find my order" ([S18](https://ca.trustpilot.com/review/www.staples.ca?page=2)) | S | Prevents handing over the wrong stack in a rush. A spoken code may be enough at low volume. | 3 | 4 | 2 | 9 |
| 8 | **Reminder and expiry rules.** Telegram nudge before no-show and before file deletion; clear "not charged / refunded" message. | PaperCut 72 h deletion ([S1](https://help.ithaca.edu/TDClient/34/Portal/KB/ArticleDet?ID=1842)); GoPrint 30 min ([S12](https://uwindsor.teamdynamix.com/TDClient/KB/ArticleDet?ID=48872)); Job Ticketing keeps files "indefinitely" (privacy risk we avoid) ([S2](https://papercut.com/help/manuals/job-ticketing/faq)) | S | Cuts no-shows. The 120-min grace and retention already exist. | 3 | 4 | 2 | 9 |
| 9 | **Layout options:** orientation, flip on long/short edge, slides per page (N-up). | WEPA "handouts3" keyword ([S7](https://www.towson.edu/wepa)); flipped-pages complaint ([S9](https://apps.apple.com/us/app/id1448454304?see-all=reviews)) | M | Common for lecture slides. The operator applies it in the print dialog. | 3 | 4 | 2 | 8 |
| 10 | **Operator daily report:** orders, pages B/W and colour, revenue Kaspi vs counter, no-shows, refunds. | Job Ticketing tracks "only the financial transactions" ([S2](https://papercut.com/help/manuals/job-ticketing/faq)) | S | Needed for Kaspi reconciliation and for the manager. Data is already in `print_job_events`. | 3 | 4 | 1 | 8 |
| 11 | **Operator bulk actions:** select several orders → printing / ready / picked up. | Workflow-by-status view ([S3](https://papercut.com/help/manuals/job-ticketing/managing-orders)); no direct bulk evidence | S | Useful at rush hour, low risk. | 3 | 4 | 1 | 8 |
| 12 | **Saved presets** (e.g. "Курстық: A4, B/W, екі жақты, скоба"). | Pharos defaults "Black & White and Double Sided" ([S10](https://uwosh.atlassian.net/wiki/spaces/IT/pages/2079719433/Student+Printing+-+Pharos+Print+Center+Desktop+and+Mobile)); WEPA email keywords ([S7](https://www.towson.edu/wepa)) | S | Nice to have. Last-used options may be enough. | 2 | 4 | 1 | 7 |
| 13 | **Print again / reorder** from history, while the file is still retained. | FedEx 6-month reorder ([S13](https://procurement.ufl.edu/wp-content/uploads/2018/01/FedExOffice_POLC_3-6_QRG-UploadandPrint.pdf)); Job Ticketing "cannot resubmit" ([S2](https://papercut.com/help/manuals/job-ticketing/faq)) | M | Conflicts with short retention. Could clone options only and ask for a re-upload. | 2 | 3 | 2 | 6 |
| 14 | **Rating after pickup** (private, to the manager). | Only indirect: rude-staff and price reviews end up on 2GIS ([S22](https://2gis.kz/astana/firm/70000001019716586/tab/reviews)) | S | Useful to the manager, but sensitive with one operator. | 2 | 3 | 1 | 6 |
| 15 | **Server-side Office conversion** for page count and preview. | FedEx falls back to staff contact for odd files ([S13](https://procurement.ufl.edu/wp-content/uploads/2018/01/FedExOffice_POLC_3-6_QRG-UploadandPrint.pdf)); Job Ticketing does not convert ([S2](https://papercut.com/help/manuals/job-ticketing/faq)) | L | Needs an isolated converter (see `docs/print.md`). The current `review` path matches industry practice. | 3 | 3 | 2 | 6 |
| 16 | **Per-page colour selection** (e.g. "pp. 5, 12 colour"). | No verified competitor offers it in the sources read | M | Real need for theses with charts, but adds pricing and operator complexity. A note field covers it today. | 3 | 2 | 1 | 5 |
| 17 | **Hold-and-release at the printer** (card or code at a release station). | Standard on campus systems ([S1](https://help.ithaca.edu/TDClient/34/Portal/KB/ArticleDet?ID=1842), [S8](https://uah.teamdynamix.com/TDClient/2075/Portal/KB/Article/67076/Charger-Prints-Printing-with-the-Wepa-Print-App), [S12](https://uwindsor.teamdynamix.com/TDClient/KB/ArticleDet?ID=48872)) | L | Needs print-management hardware or software. Our model is a staffed counter. A cheap variant: "print when I arrive" toggle, so nothing is printed for no-shows. | 2 | 1 | 3 | 4 |

**Positioning points we already have and should keep:**
- Pay per order, no wallet or minimum deposit (complaint E).
- Operator price change that the student must accept. This is a formal version of the PaperCut quote-by-comment
  pattern ([S2](https://papercut.com/help/manuals/job-ticketing/faq)).
- Short file retention and signed links, against "retains all customer supplied files indefinitely"
  ([S2](https://papercut.com/help/manuals/job-ticketing/faq)).
