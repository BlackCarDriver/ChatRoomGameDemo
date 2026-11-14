package main

import "time"

// ============================ 数据模型 ============================
//
// 所有金额统一用「分」存储（int64），1 金币 = 100 分。
// 押注、贷款是整数金币；钱包、欠款、还款允许两位小数。

const (
	phaseIdle      = "idle"
	phaseBet       = "bet"
	phaseRevealing = "revealing"

	resultWin  = "win"
	resultLose = "lose"

	windowOpen    = "open"
	windowClosing = "closing"
	windowClosed  = "closed"

	storeVersion = 1
)

// Round 当前这一局的状态。
//
// 开奖时先写入 PendingResult 并把 Phase 置为 revealing，RevealAt 到点后才把
// 结果应用到 Pool 上。这样客户端可以完整播放 2.5 秒的红绿灯动画，断线/重启
// 也不会丢失已经决定的结果。
type Round struct {
	Phase         string `json:"phase"`
	BetCents      int64  `json:"betCents"`
	PoolCents     int64  `json:"poolCents"`
	PendingResult string `json:"pendingResult,omitempty"`
	RevealAt      int64  `json:"revealAt,omitempty"`
	Result        string `json:"result,omitempty"`
	ResultAt      int64  `json:"resultAt,omitempty"`
}

// Stats 玩家累计数据，仅用于展示和排障。
type Stats struct {
	Bets               int64 `json:"bets"`
	Wins               int64 `json:"wins"`
	Losses             int64 `json:"losses"`
	Cashouts           int64 `json:"cashouts"`
	MaxPoolCents       int64 `json:"maxPoolCents"`
	TotalWonCents      int64 `json:"totalWonCents"`
	TotalLostCents     int64 `json:"totalLostCents"`
	TotalBorrowedCents int64 `json:"totalBorrowedCents"`
	TotalRepaidCents   int64 `json:"totalRepaidCents"`
	TotalInterestCents int64 `json:"totalInterestCents"`
	TotalGiftCents     int64 `json:"totalGiftCents"`
}

// Account 一个昵称对应一个账号。
type Account struct {
	Nick   string `json:"nick"`
	Avatar string `json:"avatar"`
	Frame  string `json:"frame"`
	RoleID int    `json:"roleId"`

	WalletCents int64 `json:"walletCents"`
	DebtCents   int64 `json:"debtCents"`

	FreePlaysUsed    int    `json:"freePlaysUsed"`
	LastGiftDate     string `json:"lastGiftDate"`
	GiftPendingCents int64  `json:"giftPendingCents"`
	LastSettleDate   string `json:"lastSettleDate"`

	Bankrupt   bool  `json:"bankrupt"`
	BankruptAt int64 `json:"bankruptAt,omitempty"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`

	Round Round `json:"round"`
	Stats Stats `json:"stats"`

	// LastRequestID 用于幂等：客户端重发同一个操作时不重复扣款。
	LastRequestID string `json:"lastRequestId,omitempty"`
	LastRequestAt int64  `json:"lastRequestAt,omitempty"`
}

// StoreData players.json 的顶层结构。
type StoreData struct {
	Version  int                 `json:"version"`
	SavedAt  string              `json:"savedAt"`
	Accounts map[string]*Account `json:"accounts"`
}

// ============================ 纯函数规则 ============================

// interestCents 日利息（分）。欠款 1000 金币 -> 10.00 金币；欠款 1 金币 -> 0.01 金币。
func interestCents(debtCents, rateBPS int64) int64 {
	if debtCents <= 0 || rateBPS <= 0 {
		return 0
	}
	// ceil(debt * rate / 10000)
	return (debtCents*rateBPS + 9999) / 10000
}

// maxLoanCents 当前还能借多少（分）。
// A = 钱包 - 欠款；L <= multiplier*A - 欠款。
func maxLoanCents(walletCents, debtCents, multiplier int64) int64 {
	a := walletCents - debtCents
	if a <= 0 || multiplier <= 0 {
		return 0
	}
	l := multiplier*a - debtCents
	if l < 0 {
		return 0
	}
	return l
}

func totalAssetsCents(acc *Account) int64 {
	return acc.WalletCents + acc.Round.PoolCents
}

func netWorthCents(acc *Account) int64 {
	return totalAssetsCents(acc) - acc.DebtCents
}

func freePlaysLeft(acc *Account, freePlays int) int {
	left := freePlays - acc.FreePlaysUsed
	if left < 0 {
		return 0
	}
	return left
}

// nextFeeCents 下一次押注要收的手续费（分）。
//
// 每天前 FreePlays 次免费；之后按当天第几局阶梯收费：
//
//	11~19 局 1 倍，20~29 局 2 倍，30~39 局 3 倍，以此类推。
//
// 局数按天重置（FreePlaysUsed 在日结时归零）。
func nextFeeCents(acc *Account, cfg *Config) int64 {
	index := acc.FreePlaysUsed + 1 // 今天第几局（1-based）
	if index <= cfg.FreePlays {
		return 0
	}
	tier := int64(index / 10)
	if tier < 1 {
		tier = 1
	}
	return cfg.FeeCents * tier
}

// minBetCents 下一次押注的最小金额（分）。
func minBetCents(acc *Account, cfg *Config) int64 {
	return nextFeeCents(acc, cfg) + 100
}

// resolveReveal 把已经确定的结果应用到奖池上。
func resolveReveal(acc *Account, now int64) bool {
	if acc.Round.Phase != phaseRevealing {
		return false
	}
	if acc.Round.PendingResult == resultWin {
		profit := acc.Round.PoolCents
		acc.Round.PoolCents *= 2
		acc.Round.Result = resultWin
		acc.Round.Phase = phaseBet
		acc.Stats.Wins++
		acc.Stats.TotalWonCents += profit
		if acc.Round.PoolCents > acc.Stats.MaxPoolCents {
			acc.Stats.MaxPoolCents = acc.Round.PoolCents
		}
	} else {
		acc.Round.PoolCents = 0
		acc.Round.Result = resultLose
		acc.Round.Phase = phaseIdle
		acc.Stats.Losses++
		acc.Stats.TotalLostCents += acc.Round.BetCents
	}
	acc.Round.ResultAt = now
	acc.Round.PendingResult = ""
	acc.Round.RevealAt = 0
	acc.UpdatedAt = now
	return true
}

// settleAccount 对一个账号执行日结：结算奖池、计息、抵扣赠送、判定破产、重置免费次数。
// 返回是否发生了变更。
func settleAccount(acc *Account, date string, cfg *Config, now int64) bool {
	if acc.LastSettleDate == date {
		return false
	}

	// 日结时如果还有未公布的结果，先按已确定的结果结算；奖池全部提款。
	if acc.Round.Phase == phaseRevealing {
		resolveReveal(acc, now)
	}
	if acc.Round.PoolCents > 0 {
		acc.WalletCents += acc.Round.PoolCents
		acc.Stats.Cashouts++
		acc.Round.PoolCents = 0
	}
	acc.Round.Phase = phaseIdle
	acc.Round.BetCents = 0
	acc.Round.PendingResult = ""
	acc.Round.RevealAt = 0
	acc.Round.Result = ""
	acc.Round.ResultAt = 0

	interest := interestCents(acc.DebtCents, cfg.InterestRateBPS)
	offset := int64(0)
	if acc.GiftPendingCents > 0 {
		offset = minInt64(acc.GiftPendingCents, interest)
		acc.WalletCents += acc.GiftPendingCents - offset
		acc.GiftPendingCents = 0
	}
	acc.DebtCents += interest - offset
	acc.Stats.TotalInterestCents += interest
	acc.FreePlaysUsed = 0
	acc.LastSettleDate = date

	if !acc.Bankrupt && acc.DebtCents > 0 && acc.WalletCents < acc.DebtCents {
		acc.Bankrupt = true
		acc.BankruptAt = now
	}

	acc.UpdatedAt = now
	return true
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// localDay 返回本地日期字符串 YYYY-MM-DD。
func localDay(t time.Time) string {
	return t.Format("2006-01-02")
}
