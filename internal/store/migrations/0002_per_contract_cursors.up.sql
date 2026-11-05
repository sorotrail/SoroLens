-- Per-contract ingestion cursors so each watched contract tracks its own
-- progress independently while keeping ingest_state (id = 1) for the
-- unwatched ingest-everything mode and for seeding existing deployments.
CREATE TABLE IF NOT EXISTS contract_ingest_state (
    contract_id TEXT        PRIMARY KEY,
    last_ledger BIGINT      NOT NULL DEFAULT 0,
    last_cursor TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed per-contract cursors for contracts that already have stored events so
-- an upgraded deployment does not re-ingest its retention window from scratch.
INSERT INTO contract_ingest_state (contract_id, last_ledger, last_cursor, updated_at)
SELECT DISTINCT e.contract_id,
       GREATEST(COALESCE((SELECT last_ledger FROM ingest_state WHERE id = 1), 0), MAX(e.ledger)),
       '',
       now()
FROM events e
GROUP BY e.contract_id
ON CONFLICT (contract_id) DO NOTHING;
