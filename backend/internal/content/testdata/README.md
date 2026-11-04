Test fixtures only. `data/*.json` here are the former bundled sample records, kept so the loader,
Sheets round-trip, admin editor and discount tests have stable input. They are never served:
production reads `backend/data/*.json`, which holds only real records.
