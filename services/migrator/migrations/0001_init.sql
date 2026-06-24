CREATE TABLE IF NOT EXISTS jobs (
  id            uuid PRIMARY KEY,
  status        text NOT NULL,
  original_key  text NOT NULL,
  thumbnail_key text,
  processed_key text,
  error         text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_created_at_idx ON jobs (created_at DESC);
