# KazATU (S. Seifullin University) buildings in Astana

Research for the map's «Қаладағы басқа ғимараттар / Другие здания в городе» list. Checked on 2026-11-04.

**Sources used:** official kazatu.edu.kz pages and PDFs, the university's own Yandex constructor map (embedded on
«Как нас найти»), and OpenStreetMap (Nominatim / Overpass, a few rate-limited requests). 2GIS was not scraped.

**How the coordinates were picked:** each point is the centre of the OpenStreetMap building that has the official address.
Two of those buildings (2Б, 2Д) are also named «Казахский агротехнический университет им. С. Сейфуллина» in OSM. For the
others, the point is an address match only, and the OSM building has no name. No coordinate was made up. If no building
with that address is mapped, the row has no point.

## Main campus (already on the illustrated plan, unchanged)

The university's Yandex map shows only the main campus: buildings 1 to 6 and the military department, inside one boundary
between пр. Жеңіс, ул. Ы. Алтынсарина, ул. Бейбітшілік and ул. А. Молдагуловой. Its addresses are пр. Жеңіс 62, 62А, 62Б,
62В and 62/1, Алтынсарина 2, Молдагуловой 27 and 27Г, Бейбітшілік 73, and the dormitories at Молдагуловой 29, 29А, 29В and 29Г.
The app already has all of these as b1–b15.
Source: https://kazatu.edu.kz/ru/pages/kontakty/kak-nas-najti (Yandex map `constructor:PM96aIexFoICHLaZa7Q20ONLCISiFunI`).

## Added to the app (`CITY_SITES` in `lib/data/campus.ts`)

| id | Name (kk / ru) | Address | What is there | Lat, lon | Coordinate source | Address source |
|---|---|---|---|---|---|---|
| c1 | Оқу шаруашылығы (Учхоз): №8 корпус, ветеринариялық клиника / Учхоз: корпус №8, ветеринарная клиника | пос. Интернациональный, ул. Армандастар, 2Б | Faculty of veterinary medicine and animal husbandry: departments of veterinary sanitation, microbiology and biotechnology, hunting and fisheries; canteen. Medical point at 2В, room 8210 | 51.129685, 71.586530 | OSM way 499624397: addr «улица Армандастар 2Б», named KazATU | https://kazatu.edu.kz/ru/facultet/kafedra-veterinarnoj-sanitarii («Корпус «Ветеринарная клиника», корпус №8»); https://kazatu.edu.kz/ru/facultet/kafedra-mikrobiologii-i-biotehnologii; https://kazatu.edu.kz/ru/facultet/kafedra-ohotovedenia-i-rybohozajstva; https://kazatu.edu.kz/ru/facultet/fakultet-veterinarii-i-tehnologii-zivotnovodstva («8 корпус - Учхоз»); https://kazatu.edu.kz/ru/page/stolovaa; https://kazatu.edu.kz/ru/page/medicinskie-punkty |
| c2 | Ветеринария және мал шаруашылығы технологиясы факультетінің корпусы (2Д) / Корпус факультета ветеринарии и технологии животноводства (2Д) | пос. Интернациональный, ул. Армандастар, 2Д | Department of technology of production and processing of livestock products, 2nd floor | 51.129995, 71.587775 | OSM way 1117038221: housenumber «2д», named KazATU | https://kazatu.edu.kz/ru/facultet/kafedra-tehnologii-proizvodstva-i-pererabotki-produktov-zivotnovodstva |
| c3 | №6 жатақхана / Общежитие №6 | пос. Интернациональный, ул. Армандастар, 2А | Dormitory for the Institute of Animal Science and Veterinary Medicine, tel. 43-67-09 | 51.129702, 71.588194 | OSM way 1117038225: address match, no name | Regulations on dormitories 2024-2025: https://kazatu.edu.kz/webroot/js/kcfinder/upload/files/22-08-24%20%20%D0%9F%D0%9E%D0%9B%D0%9E%D0%96%D0%95%D0%9D%D0%98%D0%95%202024-2025.pdf; «Заселение в общежития 2026-2027»: https://kazatu.edu.kz/webroot/js/kcfinder/upload/files/student_life/%D0%97%D0%B0%D1%81%D0%B5%D0%BB%D0%B5%D0%BD%D0%B8%D0%B5%20%D0%B2%20%D0%BE%D0%B1%D1%89%D0%B5%D0%B6%D0%B8%D1%82%D0%B8%D1%8F%202026-2027-%D1%80%D1%83%D1%81.pdf |
| c4 | №1А жатақхана / Общежитие №1А | пр. Сарыарка, 38 | Dormitory for senior students of all institutes, tel. 44-45-90 | 51.174073, 71.407224 | OSM way 30614718: address match, 9-storey, tagged `apartments`, no name | the same two PDFs |
| c5 | Пәтер үлгісіндегі №14 жатақхана / Общежитие квартирного типа №14 | ул. Керей и Жанибек хандар, 14А, 14Б, 14В | Flat-type dormitory for master's and PhD students | 51.113926, 71.419840 | Average of OSM ways 506032008 (14А), 506032010 (14Б) and 506032009 (14В): address matches, no names | the same two PDFs; price list on https://kazatu.edu.kz/ru/page/studenceskie-obsezitia |
| c6 | №8 жатақхана / Общежитие №8 | ул. А. Молдагуловой, 29Д | Dormitory for land resources and architecture and for energy students; medical point in room 110; tel. 31-06-55 | — (no point) | No OSM building has 29Д, so the app links to 2GIS and Google search by address | the same two PDFs; https://kazatu.edu.kz/ru/page/medicinskie-punkty |

In the app, a row with coordinates gets the links `https://2gis.kz/astana/geo/<lon>,<lat>?m=<lon>,<lat>/17` and
`https://www.google.com/maps/search/?api=1&query=<lat>,<lon>`. A row without coordinates (c6) gets an address search on
both services.

## Removed from the app (the old `UNI_SITES` list, which came from 2GIS)

- «Институт повышения квалификации, общежитие №8, ЦОО — Молдағұлова 27д». Official sources put dormitory №8 at **29Д**,
  and the ЦОО (Student Service Centre) is in the main building, пр. Женис 62, room 1103
  (https://kazatu.edu.kz/ru/pages/coo/kontaktnye-dannye_20210819062832). The app now lists dormitory №8 at 29Д as c6.
- «Сәулет факультеті — Абай даңғылы, 15». No official page confirms this address. Every official page puts architecture at
  ул. Бейбітшілік 73 (https://kazatu.edu.kz/ru/facultet/kafedra-arhitektury-i-dizajna), which is b8 on the plan. In OSM,
  пр. Абая 15 is an unnamed commercial building.

## To verify (not added)

- **Where dormitory №8 is (29Д).** OSM has a building at Молдагуловой **27Д** (51.187642, 71.412810), on campus next to the
  «Жастар» club. The 2GIS-based data put №8 at 27Д. The canteen page lists 27Д as «Учебный корпус Технического
  факультета», and an older sanitary certificate lists medical rooms at «Молдагулова 27 Д». So 27Д and 29Д may be the same
  building under an old and a new number, but no source confirms it. Check on site, then put a pin on the plan.
- **Campus dormitory numbers.** The app shows b11 «№2А» at 29А and b13 «№2Б» at 29Б. The official lists have №2А at 29А
  and no №2Б. OSM names 29А «№9 жатақхана» and gives 29Б the placeholder name «с». The official contacts page lists only
  dormitories 1А, 2А, 4, 5, 6, 7 and 8.
- **Teaching buildings 7 and 8.** The contacts page (https://kazatu.edu.kz/ru/contacts) lists teaching buildings №1–№8 with
  phone numbers only. №8 is the Учхоз at Армандастар 2Б (c1). The forestry department places «корпус 7» at пр. Жеңіс 62а,
  the military department building on campus (b4). Nothing was added for these.
- **Other numbers at Армандастар (2/1–2/5, 2Г).** OSM shows more small buildings around the Учхоз, but no source ties them
  to the university.

## Out of scope (not in Astana)

- Dormitory №3 and the forestry teaching building in Щучинск (ул. Кирова 60, ул. Верхняя 16), Акмолинская обл.
- Practice bases and field stations in Акмолинская обл. listed on department pages.
