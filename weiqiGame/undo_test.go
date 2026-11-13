package main

import "testing"

// 围棋：悔棋回退自己的最后一手，轮次回到自己
func TestUndoGo(t *testing.T) {
	g := NewGame(ModeGo, "黑方", "白方")
	if err := g.Place("黑方", -1, -1, 3, 3); err != nil {
		t.Fatalf("黑落子失败: %v", err)
	}
	if err := g.Place("白方", -1, -1, 4, 4); err != nil {
		t.Fatalf("白落子失败: %v", err)
	}
	// 白方刚下完，黑方不能悔棋
	if _, err := g.Undo("黑方"); err == nil {
		t.Fatalf("对手落子后不应允许悔棋")
	}
	// 白方可以悔棋
	used, err := g.Undo("白方")
	if err != nil {
		t.Fatalf("白方悔棋失败: %v", err)
	}
	if used != 1 {
		t.Fatalf("悔棋次数应为 1，实际 %d", used)
	}
	if g.Board.At(4, 4) != ColorEmpty {
		t.Fatalf("悔棋后该点应恢复为空")
	}
	if g.Board.At(3, 3) != ColorBlack {
		t.Fatalf("黑方棋子应保留")
	}
	if g.Turn != ColorWhite {
		t.Fatalf("悔棋后应轮到悔棋方（白）")
	}
	if len(g.Moves) != 1 {
		t.Fatalf("着法数应为 1，实际 %d", len(g.Moves))
	}
}

// 围棋：提子后悔棋应恢复被提的棋子（棋形全部由真实着法形成）
func TestUndoGoRestoresCaptured(t *testing.T) {
	g := NewGame(ModeGo, "黑方", "白方")
	steps := []struct {
		nick string
		x, y int
	}{
		{"黑方", 1, 0},
		{"白方", 1, 1},
		{"黑方", 0, 1},
		{"白方", 5, 5},
		{"黑方", 2, 1},
		{"白方", 5, 6},
	}
	for i, st := range steps {
		if err := g.Place(st.nick, -1, -1, st.x, st.y); err != nil {
			t.Fatalf("第 %d 手失败: %v", i+1, err)
		}
	}
	// 黑棋落 (1,2) 收气，提掉白子 (1,1)
	if err := g.Place("黑方", -1, -1, 1, 2); err != nil {
		t.Fatalf("提子失败: %v", err)
	}
	if g.Board.At(1, 1) != ColorEmpty {
		t.Fatalf("应已提掉白子")
	}
	if _, err := g.Undo("黑方"); err != nil {
		t.Fatalf("悔棋失败: %v", err)
	}
	if g.Board.At(1, 1) != ColorWhite {
		t.Fatalf("悔棋后被提的白子应恢复")
	}
	if g.Board.At(1, 2) != ColorEmpty {
		t.Fatalf("悔棋后黑子应移除")
	}
	if g.Captured[ColorBlack] != 0 {
		t.Fatalf("悔棋后提子数应还原为 0，实际 %d", g.Captured[ColorBlack])
	}
}

// 每局最多 3 次；用完后再悔棋失败
func TestUndoLimit(t *testing.T) {
	g := NewGame(ModeGo, "黑方", "白方")
	for i := 0; i < maxUndoPerGame; i++ {
		x := 2 + i
		if err := g.Place("黑方", -1, -1, x, 5); err != nil {
			t.Fatalf("第 %d 手落子失败: %v", i+1, err)
		}
		used, err := g.Undo("黑方")
		if err != nil {
			t.Fatalf("第 %d 次悔棋失败: %v", i+1, err)
		}
		if used != i+1 {
			t.Fatalf("悔棋次数应为 %d，实际 %d", i+1, used)
		}
	}
	if err := g.Place("黑方", -1, -1, 8, 5); err != nil {
		t.Fatalf("落子失败: %v", err)
	}
	if _, err := g.Undo("黑方"); err == nil {
		t.Fatalf("超过 %d 次后应拒绝悔棋", maxUndoPerGame)
	}
}

// 终局后不能悔棋
func TestUndoRejectedAfterEnd(t *testing.T) {
	g := NewGame(ModeGo, "黑方", "白方")
	if err := g.Place("黑方", -1, -1, 3, 3); err != nil {
		t.Fatalf("落子失败: %v", err)
	}
	if err := g.Resign("白方"); err != nil {
		t.Fatalf("认输失败: %v", err)
	}
	if _, err := g.Undo("黑方"); err == nil {
		t.Fatalf("对局结束后不应允许悔棋")
	}
}

// 五子棋：悔棋回退最后一子
func TestUndoGomoku(t *testing.T) {
	g := NewGame(ModeGomoku, "黑方", "白方")
	// 黑子间隔落（避免连成五子提前终局）：(0,0) (2,0) (4,0)
	for _, x := range []int{0, 2, 4} {
		if err := g.Place("黑方", -1, -1, x, 0); err != nil {
			t.Fatalf("黑落子失败: %v", err)
		}
		if err := g.Place("白方", -1, -1, x, 5); err != nil {
			t.Fatalf("白落子失败: %v", err)
		}
	}
	// 黑方再下一子 (6,0)，随后悔棋
	if err := g.Place("黑方", -1, -1, 6, 0); err != nil {
		t.Fatalf("黑落子失败: %v", err)
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("此时对局应仍在进行，实际 %s", g.Phase)
	}
	if _, err := g.Undo("黑方"); err != nil {
		t.Fatalf("五子棋悔棋失败: %v", err)
	}
	if g.Board.At(6, 0) != ColorEmpty {
		t.Fatalf("悔棋后该点应为空")
	}
	if g.Board.At(4, 0) != ColorBlack {
		t.Fatalf("之前的黑子应保留")
	}
	if g.Turn != ColorBlack {
		t.Fatalf("悔棋后应轮到黑方")
	}
}

// 象棋：悔棋回退走子（含吃子恢复）
func TestUndoXiangqi(t *testing.T) {
	g := NewGame(ModeXiangqi, "红方", "黑方")
	// 红车从 (0,9) 走到 (0,8)? 先走一步合法棋：红兵 (0,6) -> (0,5)
	if err := g.Place("红方", 0, 6, 0, 5); err != nil {
		t.Fatalf("红方走子失败: %v", err)
	}
	if g.Board.At(0, 5) == ColorEmpty {
		t.Fatalf("走子后目标点应有棋子")
	}
	if _, err := g.Undo("红方"); err != nil {
		t.Fatalf("象棋悔棋失败: %v", err)
	}
	if XqColor(g.Board.At(0, 6)) != ColorBlack || g.Board.At(0, 6) == ColorEmpty {
		t.Fatalf("悔棋后起点应恢复红方棋子")
	}
	if g.Board.At(0, 5) != ColorEmpty {
		t.Fatalf("悔棋后终点应为空")
	}
	if g.Turn != ColorBlack {
		t.Fatalf("悔棋后应轮到红方（ColorBlack 常量代表红方先手）")
	}
	if len(g.Moves) != 0 {
		t.Fatalf("悔棋后着法应为空，实际 %d", len(g.Moves))
	}
}
