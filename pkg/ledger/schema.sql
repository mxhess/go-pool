-- Minimal working schema for testing
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS blocks_found (
    height INTEGER PRIMARY KEY,
    hash TEXT UNIQUE NOT NULL,
    reward_total BIGINT NOT NULL,
    timestamp INTEGER NOT NULL,
    found_at INTEGER NOT NULL,
    processed_at INTEGER,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'orphaned'))
);

CREATE TABLE IF NOT EXISTS processed_transfers (
    txid TEXT PRIMARY KEY,
    height INTEGER NOT NULL,
    amount BIGINT NOT NULL,
    processed_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS balances (
    miner_address TEXT PRIMARY KEY,
    balance_pending BIGINT DEFAULT 0,
    balance_confirmed BIGINT DEFAULT 0,
    total_paid BIGINT DEFAULT 0,
    last_updated INTEGER NOT NULL,
    CHECK(balance_pending >= 0),
    CHECK(balance_confirmed >= 0),
    CHECK(total_paid >= 0)
);

CREATE TABLE IF NOT EXISTS shares (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER NOT NULL REFERENCES blocks_found(height),
    miner_address TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    difficulty BIGINT NOT NULL,
    timestamp INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER REFERENCES blocks_found(height),
    recipient_address TEXT NOT NULL,
    amount BIGINT NOT NULL,
    fee BIGINT NOT NULL DEFAULT 0,
    txid TEXT UNIQUE,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'confirmed', 'failed')),
    created_at INTEGER NOT NULL,
    sent_at INTEGER,
    confirmed_at INTEGER
);
