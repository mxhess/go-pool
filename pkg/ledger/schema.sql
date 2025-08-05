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

CREATE TABLE IF NOT EXISTS pool_stats_history (
    timestamp INTEGER PRIMARY KEY,
    pool_hashrate INTEGER NOT NULL,      -- Changed from BIGINT
    network_hashrate INTEGER NOT NULL,   -- Changed from BIGINT
    network_difficulty INTEGER NOT NULL, -- Changed from BIGINT
    connected_miners INTEGER NOT NULL,
    connected_workers INTEGER NOT NULL,
    round_shares INTEGER DEFAULT 0,
    blocks_found INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS blockchain_info (
    id INTEGER PRIMARY KEY DEFAULT 1,
    height INTEGER NOT NULL DEFAULT 0,
    block_reward INTEGER NOT NULL DEFAULT 0,
    difficulty INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL DEFAULT 0
);

-- Insert default row
INSERT OR IGNORE INTO blockchain_info (id) VALUES (1);

