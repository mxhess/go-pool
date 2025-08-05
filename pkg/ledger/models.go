

package ledger

import (
	"time"
)

// BlockFound represents a block discovered by the pool
type BlockFound struct {
	Height      uint64  `db:"height" json:"height"`
	Hash        string  `db:"hash" json:"hash"`
	RewardTotal uint64  `db:"reward_total" json:"reward_total"`
	Timestamp   int64   `db:"timestamp" json:"timestamp"`
	FoundAt     int64   `db:"found_at" json:"found_at"`
	ProcessedAt *int64  `db:"processed_at" json:"processed_at,omitempty"`
	Status      string  `db:"status" json:"status"` // pending, processing, completed, orphaned
}

// Share represents a single share submission
type Share struct {
    ID          uint64  `db:"id" json:"id"`
    BlockHeight *uint64 `db:"block_height" json:"block_height,omitempty"` // Made nullable with pointer
    MinerAddr   string  `db:"miner_address" json:"miner_address"`
    WorkerID    string  `db:"worker_id" json:"worker_id"`
    Difficulty  uint64  `db:"difficulty" json:"difficulty"`
    Timestamp   int64   `db:"timestamp" json:"timestamp"`
}

// Distribution represents calculated reward distribution for a block
type Distribution struct {
	ID               uint64  `db:"id" json:"id"`
	BlockHeight      uint64  `db:"block_height" json:"block_height"`
	MinerAddr        string  `db:"miner_address" json:"miner_address"`
	SharesContrib    uint64  `db:"shares_contributed" json:"shares_contributed"`
	Percentage       float64 `db:"percentage" json:"percentage"`
	RewardGross      uint64  `db:"reward_gross" json:"reward_gross"`
	RewardNet        uint64  `db:"reward_net" json:"reward_net"`
	CalculatedAt     int64   `db:"calculated_at" json:"calculated_at"`
}

// Payment represents a payment made to a miner
type Payment struct {
	ID            uint64  `db:"id" json:"id"`
	BlockHeight   *uint64 `db:"block_height" json:"block_height,omitempty"`
	RecipientAddr string  `db:"recipient_address" json:"recipient_address"`
	Amount        uint64  `db:"amount" json:"amount"`
	Fee           uint64  `db:"fee" json:"fee"`
	TxID          *string `db:"txid" json:"txid,omitempty"`
	Status        string  `db:"status" json:"status"` // pending, sent, confirmed, failed
	CreatedAt     int64   `db:"created_at" json:"created_at"`
	SentAt        *int64  `db:"sent_at" json:"sent_at,omitempty"`
	ConfirmedAt   *int64  `db:"confirmed_at" json:"confirmed_at,omitempty"`
}

// ProcessedTransfer tracks which wallet transfers have been processed
type ProcessedTransfer struct {
	TxID        string `db:"txid" json:"txid"`
	Height      uint64 `db:"height" json:"height"`
	Amount      uint64 `db:"amount" json:"amount"`
	ProcessedAt int64  `db:"processed_at" json:"processed_at"`
}

// Worker represents a mining worker/device
type Worker struct {
	ID            uint64  `db:"id" json:"id"`
	MinerAddr     string  `db:"miner_address" json:"miner_address"`
	WorkerID      string  `db:"worker_id" json:"worker_id"`
	DeviceInfo    *string `db:"device_info" json:"device_info,omitempty"`
	FirstSeen     int64   `db:"first_seen" json:"first_seen"`
	LastSeen      int64   `db:"last_seen" json:"last_seen"`
	UserAgent     *string `db:"user_agent" json:"user_agent,omitempty"`
	IPAddress     *string `db:"ip_address" json:"ip_address,omitempty"`
	TotalShares   uint64  `db:"total_shares" json:"total_shares"`
	ValidShares   uint64  `db:"valid_shares" json:"valid_shares"`
	StaleShares   uint64  `db:"stale_shares" json:"stale_shares"`
	InvalidShares uint64  `db:"invalid_shares" json:"invalid_shares"`
	Status        string  `db:"status" json:"status"` // active, inactive, banned
}

// Balance represents current miner balance
type Balance struct {
	MinerAddr        string `db:"miner_address" json:"miner_address"`
	BalancePending   uint64 `db:"balance_pending" json:"balance_pending"`
	BalanceConfirmed uint64 `db:"balance_confirmed" json:"balance_confirmed"`
	TotalPaid        uint64 `db:"total_paid" json:"total_paid"`
	LastUpdated      int64  `db:"last_updated" json:"last_updated"`
}

// PoolFee tracks fees earned per block
type PoolFee struct {
	ID          uint64 `db:"id" json:"id"`
	BlockHeight uint64 `db:"block_height" json:"block_height"`
	FeeAmount   uint64 `db:"fee_amount" json:"fee_amount"`
	CollectedAt int64  `db:"collected_at" json:"collected_at"`
}

// HashrateHistory stores time-series hashrate data
type HashrateHistory struct {
	ID              uint64  `db:"id" json:"id"`
	Timestamp       int64   `db:"timestamp" json:"timestamp"`
	MinerAddr       string  `db:"miner_address" json:"miner_address"`
	WorkerID        *string `db:"worker_id" json:"worker_id,omitempty"` // NULL = aggregate for miner
	Hashrate5m      uint64  `db:"hashrate_5m" json:"hashrate_5m"`
	Hashrate15m     uint64  `db:"hashrate_15m" json:"hashrate_15m"`
	Hashrate1h      uint64  `db:"hashrate_1h" json:"hashrate_1h"`
	SharesSubmitted int     `db:"shares_submitted" json:"shares_submitted"`
	SharesValid     int     `db:"shares_valid" json:"shares_valid"`
	DifficultyAvg   float64 `db:"difficulty_avg" json:"difficulty_avg"`
}

// PoolStats stores pool-wide operational statistics
type PoolStats struct {
	Timestamp         int64   `db:"timestamp" json:"timestamp"`
	TotalHashrate     uint64  `db:"total_hashrate" json:"total_hashrate"`
	ConnectedMiners   int     `db:"connected_miners" json:"connected_miners"`
	ConnectedWorkers  int     `db:"connected_workers" json:"connected_workers"`
	BlocksFound1h     int     `db:"blocks_found_1h" json:"blocks_found_1h"`
	BlocksFound24h    int     `db:"blocks_found_24h" json:"blocks_found_24h"`
	NetworkDifficulty uint64  `db:"network_difficulty" json:"network_difficulty"`
	NetworkHashrate   uint64  `db:"network_hashrate" json:"network_hashrate"`
	PoolLuck1h        float64 `db:"pool_luck_1h" json:"pool_luck_1h"`
	PoolLuck24h       float64 `db:"pool_luck_24h" json:"pool_luck_24h"`
	TotalPayments24h  uint64  `db:"total_payments_24h" json:"total_payments_24h"`
	PendingBalance    uint64  `db:"pending_balance" json:"pending_balance"`
}

// ShareSubmission stores detailed share submission data
type ShareSubmission struct {
	ID                uint64  `db:"id" json:"id"`
	Timestamp         int64   `db:"timestamp" json:"timestamp"`
	MinerAddr         string  `db:"miner_address" json:"miner_address"`
	WorkerID          string  `db:"worker_id" json:"worker_id"`
	JobID             string  `db:"job_id" json:"job_id"`
	Nonce             string  `db:"nonce" json:"nonce"`
	Result            string  `db:"result" json:"result"`
	Difficulty        uint64  `db:"difficulty" json:"difficulty"`
	TargetDifficulty  uint64  `db:"target_difficulty" json:"target_difficulty"`
	Status            string  `db:"status" json:"status"` // valid, stale, duplicate, low_difficulty, invalid
	ProcessingTimeMs  *int    `db:"processing_time_ms" json:"processing_time_ms,omitempty"`
	BlockCandidate    bool    `db:"block_candidate" json:"block_candidate"`
}

// ConnectionEvent tracks miner connection events
type ConnectionEvent struct {
	ID             uint64  `db:"id" json:"id"`
	Timestamp      int64   `db:"timestamp" json:"timestamp"`
	MinerAddr      string  `db:"miner_address" json:"miner_address"`
	WorkerID       string  `db:"worker_id" json:"worker_id"`
	EventType      string  `db:"event_type" json:"event_type"` // connect, disconnect, subscribe, authorize
	IPAddress      *string `db:"ip_address" json:"ip_address,omitempty"`
	UserAgent      *string `db:"user_agent" json:"user_agent,omitempty"`
	AdditionalData *string `db:"additional_data" json:"additional_data,omitempty"` // JSON for extra context
}

// MinerLocation stores geographic data for miners
type MinerLocation struct {
	MinerAddr   string   `db:"miner_address" json:"miner_address"`
	CountryCode *string  `db:"country_code" json:"country_code,omitempty"`
	CountryName *string  `db:"country_name" json:"country_name,omitempty"`
	Region      *string  `db:"region" json:"region,omitempty"`
	City        *string  `db:"city" json:"city,omitempty"`
	Latitude    *float64 `db:"latitude" json:"latitude,omitempty"`
	Longitude   *float64 `db:"longitude" json:"longitude,omitempty"`
	Timezone    *string  `db:"timezone" json:"timezone,omitempty"`
	LastUpdated int64    `db:"last_updated" json:"last_updated"`
}

// Helper methods for common operations
func (b *BlockFound) IsProcessed() bool {
	return b.ProcessedAt != nil
}

func (b *BlockFound) MarkProcessed() {
	now := time.Now().Unix()
	b.ProcessedAt = &now
	b.Status = "completed"
}

func (p *Payment) MarkSent(txid string) {
	now := time.Now().Unix()
	p.TxID = &txid
	p.SentAt = &now
	p.Status = "sent"
}

func (p *Payment) MarkConfirmed() {
	now := time.Now().Unix()
	p.ConfirmedAt = &now
	p.Status = "confirmed"
}

func (w *Worker) UpdateLastSeen() {
	w.LastSeen = time.Now().Unix()
}

func (w *Worker) IsOnline() bool {
	return time.Now().Unix()-w.LastSeen < 300 // 5 minutes
}

// View models for web interface
type WorkerPerformance struct {
	MinerAddr        string  `json:"miner_address"`
	WorkerID         string  `json:"worker_id"`
	DeviceInfo       *string `json:"device_info"`
	TotalShares      uint64  `json:"total_shares"`
	ValidShares      uint64  `json:"valid_shares"`
	StaleShares      uint64  `json:"stale_shares"`
	InvalidShares    uint64  `json:"invalid_shares"`
	EfficiencyPercent float64 `json:"efficiency_percent"`
	CurrentHashrate  uint64  `json:"current_hashrate"`
	LastSeen         int64   `json:"last_seen"`
	Status           string  `json:"status"` // online, recent, offline
}

type TopMiner struct {
	MinerAddr     string  `json:"miner_address"`
	WorkerCount   int     `json:"worker_count"`
	AvgHashrate   uint64  `json:"avg_hashrate"`
	TotalShares   uint64  `json:"total_shares"`
	CountryName   *string `json:"country_name"`
	CountryCode   *string `json:"country_code"`
}

type BlockDetail struct {
	Height          uint64  `json:"height"`
	Hash            string  `json:"hash"`
	FoundTime       string  `json:"found_time"`
	RewardTotal     uint64  `json:"reward_total"`
	MinersRewarded  int     `json:"miners_rewarded"`
	PoolFeeEarned   uint64  `json:"pool_fee_earned"`
	Status          string  `json:"status"`
	BlockLuck       float64 `json:"block_luck"`
}


// Withdrawal represents a completed withdrawal transaction
type Withdrawal struct {
	TxID      string
	Amount    uint64
	Timestamp int64
}

// BlockInfo is a simplified block structure for API responses
type BlockInfo struct {
	Height      uint64
	Hash        string
	RewardTotal uint64
	Timestamp   int64
	Status      string
}

// MinerBalance is an alias for Balance for API compatibility
type MinerBalance = Balance

type MinerHashratePoint struct {
    Timestamp int64    `db:"timestamp" json:"timestamp"`
    Hashrate  float64  `db:"hashrate" json:"hashrate"`
}

