package main

// ============================ 下发给前端的视图 ============================

type AccountView struct {
	Nick   string `json:"nick"`
	Avatar string `json:"avatar"`
	Frame  string `json:"frame"`
	RoleID int    `json:"roleId"`

	WalletCents      int64 `json:"walletCents"`
	DebtCents        int64 `json:"debtCents"`
	TotalAssetsCents int64 `json:"totalAssetsCents"`
	NetWorthCents    int64 `json:"netWorthCents"`

	FreePlaysUsed int `json:"freePlaysUsed"`
	FreePlaysLeft int `json:"freePlaysLeft"`
	FreePlaysMax  int `json:"freePlaysMax"`

	DailyInterestCents int64 `json:"dailyInterestCents"`
	MaxLoanCents       int64 `json:"maxLoanCents"`
	MinBetCents        int64 `json:"minBetCents"`
	NextFeeCents       int64 `json:"nextFeeCents"`

	GiftPendingCents int64  `json:"giftPendingCents"`
	LastGiftDate     string `json:"lastGiftDate"`
	DailyGiftCents   int64  `json:"dailyGiftCents"`
	GiftClaimedToday bool   `json:"giftClaimedToday"`

	Bankrupt   bool  `json:"bankrupt"`
	BankruptAt int64 `json:"bankruptAt,omitempty"`

	Stats Stats `json:"stats"`
}

type RoundView struct {
	Phase     string `json:"phase"`
	BetCents  int64  `json:"betCents"`
	PoolCents int64  `json:"poolCents"`
	RevealAt  int64  `json:"revealAt,omitempty"`
	Result    string `json:"result,omitempty"`
	ResultAt  int64  `json:"resultAt,omitempty"`
}

type WindowView struct {
	Phase    string `json:"phase"`
	CloseAt  int64  `json:"closeAt,omitempty"`
	ReopenAt int64  `json:"reopenAt,omitempty"`
}

type LeaderboardEntry struct {
	Rank             int    `json:"rank"`
	Nick             string `json:"nick"`
	Avatar           string `json:"avatar"`
	Frame            string `json:"frame"`
	TotalAssetsCents int64  `json:"totalAssetsCents"`
	DebtCents        int64  `json:"debtCents"`
	NetWorthCents    int64  `json:"netWorthCents"`
	Bankrupt         bool   `json:"bankrupt"`
}

func accountView(acc *Account, cfg *Config, today string) AccountView {
	return AccountView{
		Nick:               acc.Nick,
		Avatar:             acc.Avatar,
		Frame:              acc.Frame,
		RoleID:             acc.RoleID,
		WalletCents:        acc.WalletCents,
		DebtCents:          acc.DebtCents,
		TotalAssetsCents:   totalAssetsCents(acc),
		NetWorthCents:      netWorthCents(acc),
		FreePlaysUsed:      acc.FreePlaysUsed,
		FreePlaysLeft:      freePlaysLeft(acc, cfg.FreePlays),
		FreePlaysMax:       cfg.FreePlays,
		DailyInterestCents: interestCents(acc.DebtCents, cfg.InterestRateBPS),
		MaxLoanCents:       maxLoanCents(acc.WalletCents, acc.DebtCents, cfg.LoanMultiplier),
		MinBetCents:        minBetCents(acc, cfg),
		NextFeeCents:       nextFeeCents(acc, cfg),
		GiftPendingCents:   acc.GiftPendingCents,
		LastGiftDate:       acc.LastGiftDate,
		DailyGiftCents:     cfg.GiftCents,
		GiftClaimedToday:   acc.LastGiftDate == today,
		Bankrupt:           acc.Bankrupt,
		BankruptAt:         acc.BankruptAt,
		Stats:              acc.Stats,
	}
}

func roundViewOf(acc *Account) RoundView {
	return RoundView{
		Phase:     acc.Round.Phase,
		BetCents:  acc.Round.BetCents,
		PoolCents: acc.Round.PoolCents,
		RevealAt:  acc.Round.RevealAt,
		Result:    acc.Round.Result,
		ResultAt:  acc.Round.ResultAt,
	}
}

func leaderboardEntryOf(acc *Account) LeaderboardEntry {
	return LeaderboardEntry{
		Nick:             acc.Nick,
		Avatar:           acc.Avatar,
		Frame:            acc.Frame,
		TotalAssetsCents: totalAssetsCents(acc),
		DebtCents:        acc.DebtCents,
		NetWorthCents:    netWorthCents(acc),
		Bankrupt:         acc.Bankrupt,
	}
}
