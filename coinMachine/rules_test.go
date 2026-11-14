package main

import (
	"path/filepath"
	"testing"
	"time"
)

func testConfig() *Config {
	return &Config{
		DailyGiftCoins:  10,
		FreePlays:       10,
		FeeCoins:        1,
		InterestRateBPS: 100,
		LoanMultiplier:  100,
		GiftCents:       1000,
		FeeCents:        100,
	}
}

func TestInterestCents(t *testing.T) {
	cases := []struct {
		debt int64
		want int64
	}{
		{0, 0},
		{1, 1},
		{100, 1},
		{1000, 10},
		{10000, 100},
		{100000, 1000},
		{100050, 1001},
	}
	for _, c := range cases {
		if got := interestCents(c.debt, 100); got != c.want {
			t.Errorf("interestCents(%d)=%d want %d", c.debt, got, c.want)
		}
	}
}

func TestMaxLoanCents(t *testing.T) {
	cases := []struct {
		wallet, debt, want int64
	}{
		{1000, 0, 100000},
		{101000, 100000, 0},
		{200000, 0, 20000000},
		{0, 0, 0},
		{1000, 1000, 0},
		{1000, 2000, 0},
	}
	for _, c := range cases {
		if got := maxLoanCents(c.wallet, c.debt, 100); got != c.want {
			t.Errorf("maxLoanCents(%d,%d)=%d want %d", c.wallet, c.debt, got, c.want)
		}
	}
}

func TestFreePlaysAndFee(t *testing.T) {
	cfg := testConfig()
	acc := &Account{}
	if got := nextFeeCents(acc, cfg); got != 0 {
		t.Errorf("first bet fee=%d want 0", got)
	}
	if got := minBetCents(acc, cfg); got != 100 {
		t.Errorf("first bet min=%d want 100", got)
	}
	acc.FreePlaysUsed = 9 // 第 10 局
	if got := nextFeeCents(acc, cfg); got != 0 {
		t.Errorf("10th bet fee=%d want 0", got)
	}
	acc.FreePlaysUsed = 10 // 第 11 局
	if got := nextFeeCents(acc, cfg); got != 100 {
		t.Errorf("11th bet fee=%d want 100", got)
	}
	if got := minBetCents(acc, cfg); got != 200 {
		t.Errorf("11th bet min=%d want 200", got)
	}
	acc.FreePlaysUsed = 19 // 第 20 局
	if got := nextFeeCents(acc, cfg); got != 200 {
		t.Errorf("20th bet fee=%d want 200", got)
	}
	if got := minBetCents(acc, cfg); got != 300 {
		t.Errorf("20th bet min=%d want 300", got)
	}
	acc.FreePlaysUsed = 29 // 第 30 局
	if got := nextFeeCents(acc, cfg); got != 300 {
		t.Errorf("30th bet fee=%d want 300", got)
	}
	if got := minBetCents(acc, cfg); got != 400 {
		t.Errorf("30th bet min=%d want 400", got)
	}
	acc.FreePlaysUsed = 39 // 第 40 局
	if got := nextFeeCents(acc, cfg); got != 400 {
		t.Errorf("40th bet fee=%d want 400", got)
	}
}

func TestSettleGiftOffset(t *testing.T) {
	cfg := testConfig()
	now := time.Now().UnixMilli()

	// 欠款 1000 金币，利息 10，赠送 10 -> 欠款不变，钱包 0
	acc := &Account{DebtCents: 100000, GiftPendingCents: 1000}
	if !settleAccount(acc, "2025-01-01", cfg, now) {
		t.Fatal("expected change")
	}
	if acc.DebtCents != 100000 || acc.WalletCents != 0 {
		t.Errorf("debt=%d wallet=%d want 100000/0", acc.DebtCents, acc.WalletCents)
	}
	if !acc.Bankrupt {
		t.Errorf("0 assets vs 1000 debt should be bankrupt")
	}

	// 欠款 2000，利息 20，赠送 10 -> 欠款 201000
	acc = &Account{DebtCents: 200000, GiftPendingCents: 1000}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.DebtCents != 201000 || acc.WalletCents != 0 {
		t.Errorf("debt=%d wallet=%d want 201000/0", acc.DebtCents, acc.WalletCents)
	}
	if !acc.Bankrupt {
		t.Errorf("0 assets vs 2010 debt should be bankrupt")
	}

	// 欠款 500，利息 5，赠送 10 -> 多余 5 进钱包
	acc = &Account{DebtCents: 50000, GiftPendingCents: 1000}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.DebtCents != 50000 || acc.WalletCents != 500 {
		t.Errorf("debt=%d wallet=%d want 50000/500", acc.DebtCents, acc.WalletCents)
	}
	if !acc.Bankrupt {
		t.Errorf("0.5 assets vs 500 debt should be bankrupt")
	}

	// 先还清欠款，赠送全进钱包
	acc = &Account{DebtCents: 0, GiftPendingCents: 1000}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.WalletCents != 1000 {
		t.Errorf("wallet=%d want 1000", acc.WalletCents)
	}
}

func TestSettleBankruptcy(t *testing.T) {
	cfg := testConfig()
	now := time.Now().UnixMilli()

	// 0 资产，1000 欠款 -> 利息 10 加进欠款，破产
	acc := &Account{DebtCents: 100000}
	settleAccount(acc, "2025-01-01", cfg, now)
	if !acc.Bankrupt {
		t.Fatal("expected bankrupt")
	}
	if acc.DebtCents != 101000 {
		t.Errorf("debt=%d want 101000", acc.DebtCents)
	}

	// 资产恰好等于利息后的欠款 -> 不破产（严格小于才破产）
	acc = &Account{DebtCents: 100000, WalletCents: 101000}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.Bankrupt {
		t.Errorf("net worth 0 should not be bankrupt, debt=%d wallet=%d", acc.DebtCents, acc.WalletCents)
	}

	// 0/0 不破产
	acc = &Account{}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.Bankrupt {
		t.Errorf("0/0 should not be bankrupt")
	}
}

func TestSettleResolvesRevealAndCashout(t *testing.T) {
	cfg := testConfig()
	now := time.Now().UnixMilli()
	acc := &Account{
		WalletCents: 0,
		Round: Round{
			Phase:         phaseRevealing,
			BetCents:      1000,
			PoolCents:     1000,
			PendingResult: resultWin,
			RevealAt:      now + 10000,
		},
	}
	settleAccount(acc, "2025-01-01", cfg, now)
	if acc.Round.PoolCents != 0 || acc.Round.Phase != phaseIdle {
		t.Errorf("round not settled: %+v", acc.Round)
	}
	if acc.WalletCents != 2000 {
		t.Errorf("wallet=%d want 2000", acc.WalletCents)
	}
}

func TestResolveRevealWinLose(t *testing.T) {
	now := time.Now().UnixMilli()
	acc := &Account{Round: Round{Phase: phaseRevealing, BetCents: 1000, PoolCents: 1000, PendingResult: resultWin}}
	resolveReveal(acc, now)
	if acc.Round.PoolCents != 2000 || acc.Round.Phase != phaseBet || acc.Round.Result != resultWin {
		t.Errorf("win: %+v", acc.Round)
	}

	acc = &Account{Round: Round{Phase: phaseRevealing, BetCents: 1000, PoolCents: 1000, PendingResult: resultLose}}
	resolveReveal(acc, now)
	if acc.Round.PoolCents != 0 || acc.Round.Phase != phaseIdle || acc.Round.Result != resultLose {
		t.Errorf("lose: %+v", acc.Round)
	}
}

func TestCashoutRound(t *testing.T) {
	now := time.Now().UnixMilli()
	acc := &Account{WalletCents: 500, Round: Round{Phase: phaseBet, BetCents: 1000, PoolCents: 1000}}
	if !cashoutRound(acc, now) {
		t.Fatal("expected change")
	}
	if acc.WalletCents != 1500 || acc.Round.PoolCents != 0 || acc.Round.Phase != phaseIdle {
		t.Errorf("cashout: wallet=%d round=%+v", acc.WalletCents, acc.Round)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	acc, created, err := st.Ensure("alice", "a.png", "", 1, now)
	if err != nil || !created {
		t.Fatalf("ensure: %v created=%v", err, created)
	}
	_, err = st.Update("alice", func(a *Account) error {
		a.WalletCents = 1234
		a.DebtCents = 5678
		a.Round = Round{Phase: phaseBet, BetCents: 100, PoolCents: 100}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	st2 := NewStore(dir)
	if err := st2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := st2.Get("alice")
	if !ok {
		t.Fatal("account missing")
	}
	if got.WalletCents != 1234 || got.DebtCents != 5678 || got.Round.PoolCents != 100 {
		t.Errorf("got %+v", got)
	}
	if acc.Nick != "alice" {
		t.Errorf("nick=%s", acc.Nick)
	}
	if _, err := filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
}

func TestLeaderboardSort(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	_ = st.Load()
	now := time.Now()
	for _, item := range []struct {
		nick     string
		wallet   int64
		debt     int64
		bankrupt bool
	}{
		{"a", 500000, 0, false},
		{"b", 300000, 100000, false},
		{"c", 80000, 120000, false},
		{"d", 0, 500000, true},
	} {
		_, _, _ = st.Ensure(item.nick, "", "", 0, now)
		_, _ = st.Update(item.nick, func(acc *Account) error {
			acc.WalletCents = item.wallet
			acc.DebtCents = item.debt
			acc.Bankrupt = item.bankrupt
			return nil
		})
	}
	h := newHub(testConfig(), st, time.Local)
	top, self, total := h.Leaderboard("c")
	if total != 4 {
		t.Fatalf("total=%d", total)
	}
	want := []string{"a", "b", "c", "d"}
	for i, e := range top {
		if e.Nick != want[i] {
			t.Errorf("rank %d = %s want %s", i+1, e.Nick, want[i])
		}
	}
	if self == nil || self.Nick != "c" || self.Rank != 3 {
		t.Errorf("self=%+v", self)
	}
}
