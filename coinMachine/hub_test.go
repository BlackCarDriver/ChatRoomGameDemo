package main

import (
	"testing"
	"time"
)

func newTestHub(t *testing.T) *hub {
	t.Helper()
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.DataDir = dir
	h := newHub(cfg, st, time.Local)
	h.cfg = cfg
	return h
}

func ensureTestAccount(t *testing.T, h *hub, wallet int64) {
	t.Helper()
	_, _, err := h.store.Ensure("alice", "", "", 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.store.Update("alice", func(a *Account) error {
		a.WalletCents = wallet
		a.DebtCents = 0
		a.FreePlaysUsed = 0
		a.Round = Round{Phase: phaseIdle}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getTestAccount(t *testing.T, h *hub) *Account {
	t.Helper()
	acc, ok := h.store.Get("alice")
	if !ok {
		t.Fatal("account missing")
	}
	return acc
}

func TestHubBetFeeAndDuplicate(t *testing.T) {
	h := newTestHub(t)
	ensureTestAccount(t, h, 10000)

	h.Bet("alice", "r1", 1000)
	acc := getTestAccount(t, h)
	if acc.WalletCents != 9000 || acc.Round.PoolCents != 1000 || acc.FreePlaysUsed != 1 {
		t.Fatalf("after bet: %+v", acc)
	}

	// 同一个 requestId 重发，不应重复扣款
	h.Bet("alice", "r1", 1000)
	acc = getTestAccount(t, h)
	if acc.WalletCents != 9000 || acc.Round.PoolCents != 1000 || acc.FreePlaysUsed != 1 {
		t.Fatalf("duplicate bet changed state: %+v", acc)
	}

	h.Cashout("alice", "r2")
	acc = getTestAccount(t, h)
	if acc.WalletCents != 10000 || acc.Round.PoolCents != 0 || acc.Round.Phase != phaseIdle {
		t.Fatalf("after cashout: %+v", acc)
	}

	// 第 11 次押注：押 2 金币，1 金币手续费，奖池 1 金币
	_, _ = h.store.Update("alice", func(a *Account) error {
		a.FreePlaysUsed = 10
		return nil
	})
	h.Bet("alice", "r3", 200)
	acc = getTestAccount(t, h)
	if acc.WalletCents != 9800 || acc.Round.PoolCents != 100 || acc.FreePlaysUsed != 11 {
		t.Fatalf("paid bet: %+v", acc)
	}
	h.Cashout("alice", "r4")
	acc = getTestAccount(t, h)
	if acc.WalletCents != 9900 {
		t.Fatalf("fee should be lost, wallet=%d", acc.WalletCents)
	}
}

func TestHubBorrowRepay(t *testing.T) {
	h := newTestHub(t)
	ensureTestAccount(t, h, 1000)

	h.Borrow("alice", "b1", 100000)
	acc := getTestAccount(t, h)
	if acc.WalletCents != 101000 || acc.DebtCents != 100000 {
		t.Fatalf("borrow: %+v", acc)
	}
	if got := maxLoanCents(acc.WalletCents, acc.DebtCents, h.cfg.LoanMultiplier); got != 0 {
		t.Fatalf("max loan after borrowing max = %d, want 0", got)
	}

	// 再借应该被拒绝
	h.Borrow("alice", "b2", 100000)
	acc = getTestAccount(t, h)
	if acc.DebtCents != 100000 {
		t.Fatalf("over-borrow changed debt: %d", acc.DebtCents)
	}

	h.Repay("alice", "r1", 50000)
	acc = getTestAccount(t, h)
	if acc.WalletCents != 51000 || acc.DebtCents != 50000 {
		t.Fatalf("repay: %+v", acc)
	}
}

func TestHubBankruptCannotBet(t *testing.T) {
	h := newTestHub(t)
	ensureTestAccount(t, h, 1000)
	_, _ = h.store.Update("alice", func(a *Account) error {
		a.Bankrupt = true
		return nil
	})
	h.Bet("alice", "r1", 1000)
	acc := getTestAccount(t, h)
	if acc.WalletCents != 1000 || acc.Round.Phase != phaseIdle {
		t.Fatalf("bankrupt bet changed state: %+v", acc)
	}
}

func TestHubAutoCashout(t *testing.T) {
	h := newTestHub(t)
	ensureTestAccount(t, h, 10000)
	h.Bet("alice", "r1", 1000)
	h.autoCashout("alice", "test")
	acc := getTestAccount(t, h)
	if acc.WalletCents != 10000 || acc.Round.PoolCents != 0 || acc.Round.Phase != phaseIdle {
		t.Fatalf("auto cashout: %+v", acc)
	}
}

func TestWindowPhase(t *testing.T) {
	h := newTestHub(t)
	loc := h.loc
	cases := []struct {
		hour, min int
		want      string
	}{
		{23, 49, windowOpen},
		{23, 50, windowClosing},
		{23, 58, windowClosing},
		{23, 59, windowClosed},
		{0, 0, windowClosed},
		{0, 0, windowClosed},
		{0, 1, windowOpen},
		{12, 0, windowOpen},
	}
	for _, c := range cases {
		now := time.Date(2026, 1, 1, c.hour, c.min, 0, 0, loc)
		if got := h.windowPhaseAt(now); got != c.want {
			t.Errorf("%02d:%02d = %s want %s", c.hour, c.min, got, c.want)
		}
	}
}

func TestSettleOnceIdempotent(t *testing.T) {
	h := newTestHub(t)
	ensureTestAccount(t, h, 0)
	_, _ = h.store.Update("alice", func(a *Account) error {
		a.DebtCents = 100000
		return nil
	})
	now := time.Now()
	h.settleOnce("2026-01-01", now)
	acc := getTestAccount(t, h)
	if acc.DebtCents != 101000 {
		t.Fatalf("first settle debt=%d", acc.DebtCents)
	}
	h.settleOnce("2026-01-01", now)
	acc = getTestAccount(t, h)
	if acc.DebtCents != 101000 {
		t.Fatalf("second settle changed debt=%d", acc.DebtCents)
	}
}
