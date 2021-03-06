begin;

create extension "uuid-ossp";

create table results (
  id uuid primary key default uuid_generate_v4(),
  created_at timestamptz default now(),
  app text,
  database text,
  url text,
  checks json
);

commit;
