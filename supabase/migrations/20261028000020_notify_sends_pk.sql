-- notify_sends had no primary key; add one so rows can be addressed (deletion proofs, cleanup).
alter table notify_sends add column id bigint generated always as identity primary key;
