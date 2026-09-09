-- Contract events captured by standalone mode.
--
-- id is the RPC's TOID-based event identifier. It is zero-padded, so ordering
-- by id lexicographically is the same as ordering chronologically; every
-- cursor in SoroLens relies on that property.
CREATE TABLE IF NOT EXISTS events (
    id                 TEXT PRIMARY KEY,
    contract_id        TEXT        NOT NULL,
    ledger             BIGINT      NOT NULL,
    type               TEXT        NOT NULL,
    tx_hash            TEXT        NOT NULL DEFAULT '',
    tx_index           INTEGER     NOT NULL DEFAULT 0,
    op_index           INTEGER     NOT NULL DEFAULT 0,
    in_successful_call BOOLEAN     NOT NULL DEFAULT TRUE,
    topics             JSONB       NOT NULL DEFAULT '[]'::jsonb,
    value              JSONB       NOT NULL DEFAULT 'null'::jsonb,
    -- On-chain close time of the event's ledger, as distinct from created_at
    -- below, which is when SoroLens happened to ingest it.
    ledger_closed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS events_contract_id_idx ON events (contract_id);
CREATE INDEX IF NOT EXISTS events_ledger_idx      ON events (ledger);

-- Serves the contract detail page: one contract's events, newest first.
CREATE INDEX IF NOT EXISTS events_contract_id_ledger_idx ON events (contract_id, ledger);

-- Serves both the global feed and per-contract paging, which order by id DESC.
CREATE INDEX IF NOT EXISTS events_id_desc_idx             ON events (id DESC);
CREATE INDEX IF NOT EXISTS events_contract_id_id_desc_idx ON events (contract_id, id DESC);

-- Supports the topic filter, which uses jsonb containment (topics @> ...).
CREATE INDEX IF NOT EXISTS events_topics_idx ON events USING GIN (topics jsonb_path_ops);

-- Single-row table tracking ingestion progress. The CHECK keeps it single-row,
-- so a bug can never leave two competing resume points.
CREATE TABLE IF NOT EXISTS ingest_state (
    id          SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_ledger BIGINT      NOT NULL DEFAULT 0,
    last_cursor TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO ingest_state (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
