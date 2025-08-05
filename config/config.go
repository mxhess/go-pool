/*
 * This file is part of go-pool.
 *
 * go-pool is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * go-pool is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with go-pool. If not, see <http://www.gnu.org/licenses/>.
 */

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const MAX_REQUEST_SIZE = 5 * 1024 // 5 MiB

var Cfg Config

func init() {
	fd, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Println(err)

		fd, err = os.ReadFile("../config.json")
		if err != nil {
			blankCfg, err := json.MarshalIndent(Config{}, "", "\t")

			if err != nil {
				panic(err)
			}

			os.WriteFile("config.json", blankCfg, 0o666)

			panic(fmt.Errorf("could not open config: %s. blank configuration created", err))
		}
	}

	err = json.Unmarshal(fd, &Cfg)
	if err != nil {
		panic(err)
	}

	// master password is hashed with sha256 to make it fixed-length (32 bytes long)
	MasterPass = sha256.Sum256([]byte(Cfg.MasterPass))
	fmt.Println("Master password is", hex.EncodeToString(MasterPass[:]))

}

var MasterPass [32]byte
var BlockTime uint64

type Config struct {
	LogLevel uint8 `json:"log_level"`

	DaemonRpc string `json:"daemon_rpc"`

	Atomic   int    `json:"atomic"`
	MinConfs uint64 `json:"min_confs"`

	AddrPrefix    []byte `json:"addr_prefix"`
	SubaddrPrefix []byte `json:"subaddr_prefix"`

	PoolAddress string `json:"pool_address"`
	FeeAddress  string `json:"fee_address"`

	UseP2Pool     bool   `json:"use_p2pool"`
	P2PoolAddress string `json:"p2pool_address"`

	MasterPass string `json:"master_pass"`

	AlgoName string `json:"algo_name"`

	MasterConfig MasterConfig `json:"master_config"`
	SlaveConfig  SlaveConfig  `json:"slave_config"`
}

type MasterConfig struct {
	ListenAddress    string        `json:"listen_address"`
	WalletRpc        string        `json:"wallet_rpc"`
	FeePercent       float64       `json:"fee_percent"`
	ApiPort          uint16        `json:"api_port"`
	WithdrawalFee    float64       `json:"withdrawal_fee"`
	MinWithdrawal    float64       `json:"min_withdrawal"`
	WithdrawInterval int64         `json:"withdrawal_interval_minutes"`
	PPLNSTargetShares uint64       `json:"pplns_target_shares"` // Target N for PPLNS
	PPLNSMinWindow    float64      `json:"pplns_min_window"`    // Min window as % of net diff (0.5 = 50%)
	PPLNSMaxWindow    float64      `json:"pplns_max_window"`    // Max window as % of net diff (5.0 = 500%)
	PPLNSLuckFactor   float64      `json:"pplns_luck_factor"`   // Adjustment factor (0.1 = 10%)
	Stratums         []StratumAddr `json:"stratums"`
}

type SlaveConfig struct {
	MasterAddress string `json:"master_address"`

	MinDiff         uint64 `json:"min_diff"`
	ShareTargetTime uint64 `json:"share_target_time"`
	TrustScore      uint64 `json:"trust_score"`

	PoolPort    uint16 `json:"pool_port"`
	PoolPortTls uint16 `json:"pool_port_tls"`

	MonitorPort int `json:"monitor_port"`

	TemplateTimeout int     `json:"template_timeout"`
	SlaveFee        float64 `json:"slave_fee"`

        // Daemon pool settings
        DaemonPoolSize    int `json:"daemon_pool_size"`        // Number of daemon connections
        DaemonPoolMaxAge  int `json:"daemon_pool_max_age"`     // Minutes before recycling connections
    
        // Verify queue settings  
        VerifyWorkers     int    `json:"verify_workers"`       // Number of verification workers
        VerifyQueueDB     string `json:"verify_queue_db"`      // Path to verification queue database
        VerifyAlertDelay  int    `json:"verify_alert_delay"`   // Seconds before alerting on slow verification

	// Penalty Box Configuration
	PenaltyEnabled        bool    `json:"penalty_enabled"`         // Enable/disable penalty box
	MaxSharesPerSecond    float64 `json:"max_shares_per_second"`   // Rate limit threshold
	BadShareRatio         float64 `json:"bad_share_ratio"`         // Max bad share ratio (e.g., 0.167 for 1/6)
	BanDurationMinutes    int     `json:"ban_duration_minutes"`    // How long to ban offenders
	ShareWindowMinutes    int     `json:"share_window_minutes"`    // Time window for ratio calculation
	ShareTimingWindowSize int     `json:"share_timing_window"`     // Number of shares to track for rate limiting

}

type StratumAddr struct {
	Addr string `json:"addr"`
	Desc string `json:"desc"`
	Tls  bool   `json:"tls"`
}
