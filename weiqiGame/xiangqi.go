package main

import "fmt"

// ============================ 象棋规则 ============================
//
// 坐标系与围棋一致：Cells[y][x]，x 为列（0~8，从红方左侧数起），y 为行（0~9，从上到下）。
// 颜色沿用同一套常量：
//
//	ColorBlack(1) = 红方（先手，棋盘下方，座位「黑」）
//	ColorWhite(2) = 黑方（后手，棋盘上方，座位「白」）
//
// 这样对局轮次、座位、计时等通用逻辑不需要区分玩法，只有棋子表现不同。

// 象棋棋子类型
const (
	XqGeneral  = 1 // 帅 / 将
	XqAdvisor  = 2 // 仕 / 士
	XqElephant = 3 // 相 / 象
	XqHorse    = 4 // 马
	XqChariot  = 5 // 车
	XqCannon   = 6 // 炮
	XqSoldier  = 7 // 兵 / 卒
)

// XqPiece 打包棋子编码：高 4 位颜色，低 4 位棋子类型；0 表示空点。
func XqPiece(color, typ int) int { return color<<4 | typ }

// XqColor 取棋子颜色（0 表示空点）
func XqColor(piece int) int { return piece >> 4 }

// XqType 取棋子类型
func XqType(piece int) int { return piece & 0xF }

// XqName 棋子中文名：红方为帅仕相马车炮兵，黑方为将士象马车炮卒
func XqName(piece int) string {
	red := []rune("帅仕相马车炮兵")
	black := []rune("将士象马车炮卒")
	typ := XqType(piece)
	if typ < 1 || typ > 7 {
		return ""
	}
	if XqColor(piece) == ColorWhite {
		return string(black[typ-1])
	}
	return string(red[typ-1])
}

// NewXiangqiBoard 摆好初始棋子的象棋棋盘
func NewXiangqiBoard() *Board {
	b := NewBoard(BoardColsXiangqi, BoardRowsXiangqi)
	back := []int{XqChariot, XqHorse, XqElephant, XqAdvisor, XqGeneral, XqAdvisor, XqElephant, XqHorse, XqChariot}
	for x, typ := range back {
		b.Cells[0][x] = XqPiece(ColorWhite, typ) // 上方：黑方
		b.Cells[9][x] = XqPiece(ColorBlack, typ) // 下方：红方
	}
	for _, x := range []int{1, 7} {
		b.Cells[2][x] = XqPiece(ColorWhite, XqCannon)
		b.Cells[7][x] = XqPiece(ColorBlack, XqCannon)
	}
	for _, x := range []int{0, 2, 4, 6, 8} {
		b.Cells[3][x] = XqPiece(ColorWhite, XqSoldier)
		b.Cells[6][x] = XqPiece(ColorBlack, XqSoldier)
	}
	return b
}

// ---------- 棋形工具 ----------

var xqDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
var xqDiags = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

// 马走「日」：8 个落点
var xqHorseTargets = [8][2]int{
	{1, 2}, {2, 1}, {-1, 2}, {-2, 1},
	{1, -2}, {2, -1}, {-1, -2}, {-2, -1},
}

// 象走「田」：4 个落点
var xqElephantTargets = [4][2]int{{2, 2}, {2, -2}, {-2, 2}, {-2, -2}}

// xqInPalace 是否在该方九宫内（红方在下、黑方在上）
func xqInPalace(color, x, y int) bool {
	if x < 3 || x > 5 {
		return false
	}
	if color == ColorBlack {
		return y >= 7 && y <= 9
	}
	return y >= 0 && y <= 2
}

// xqCrossedRiver 该方棋子是否已过河（红方过河即 y<=4，黑方过河即 y>=5）
func xqCrossedRiver(color, y int) bool {
	if color == ColorBlack {
		return y <= 4
	}
	return y >= 5
}

// xqForward 该方前进方向：红方往上（-1），黑方往下（+1）
func xqForward(color int) int {
	if color == ColorBlack {
		return -1
	}
	return 1
}

// xqCanLand 目标点是否可以落子：空点或对方棋子
func (b *Board) xqCanLand(x, y, color int) bool {
	target := b.At(x, y)
	return target == ColorEmpty || XqColor(target) != color
}

// ---------- 着法生成 ----------

type xqMove struct {
	FromX, FromY int
	ToX, ToY     int
}

// xqMovesOfPiece 生成单个棋子的全部走法（不检查走完是否被将军）
func xqMovesOfPiece(b *Board, x, y int) []xqMove {
	piece := b.At(x, y)
	if piece == ColorEmpty {
		return nil
	}
	color := XqColor(piece)
	out := make([]xqMove, 0, 16)
	add := func(tx, ty int) {
		if b.xqCanLand(tx, ty, color) {
			out = append(out, xqMove{FromX: x, FromY: y, ToX: tx, ToY: ty})
		}
	}

	switch XqType(piece) {
	case XqChariot:
		// 车：四方向直线，遇子即止
		for _, d := range xqDirs {
			cx, cy := x+d[0], y+d[1]
			for b.InBounds(cx, cy) {
				target := b.At(cx, cy)
				if target == ColorEmpty {
					out = append(out, xqMove{FromX: x, FromY: y, ToX: cx, ToY: cy})
					cx += d[0]
					cy += d[1]
					continue
				}
				if XqColor(target) != color {
					out = append(out, xqMove{FromX: x, FromY: y, ToX: cx, ToY: cy})
				}
				break
			}
		}

	case XqCannon:
		// 炮：不吃子时同车；吃子必须隔一个炮架
		for _, d := range xqDirs {
			cx, cy := x+d[0], y+d[1]
			for b.InBounds(cx, cy) && b.At(cx, cy) == ColorEmpty {
				out = append(out, xqMove{FromX: x, FromY: y, ToX: cx, ToY: cy})
				cx += d[0]
				cy += d[1]
			}
			if !b.InBounds(cx, cy) {
				continue
			}
			// 当前点即炮架，越过它找第一个棋子
			cx += d[0]
			cy += d[1]
			for b.InBounds(cx, cy) {
				target := b.At(cx, cy)
				if target == ColorEmpty {
					cx += d[0]
					cy += d[1]
					continue
				}
				if XqColor(target) != color {
					out = append(out, xqMove{FromX: x, FromY: y, ToX: cx, ToY: cy})
				}
				break
			}
		}

	case XqHorse:
		// 马：走日，蹩马腿不能走
		for _, m := range xqHorseTargets {
			tx, ty := x+m[0], y+m[1]
			if !b.InBounds(tx, ty) {
				continue
			}
			lx, ly := x, y
			if m[0] == 2 || m[0] == -2 {
				lx += m[0] / 2
			} else {
				ly += m[1] / 2
			}
			if b.At(lx, ly) != ColorEmpty {
				continue
			}
			add(tx, ty)
		}

	case XqElephant:
		// 相/象：走田，塞象眼不能走，且不能过河
		for _, m := range xqElephantTargets {
			tx, ty := x+m[0], y+m[1]
			if !b.InBounds(tx, ty) {
				continue
			}
			if b.At(x+m[0]/2, y+m[1]/2) != ColorEmpty {
				continue
			}
			if xqCrossedRiver(color, ty) != xqCrossedRiver(color, y) {
				continue
			}
			add(tx, ty)
		}

	case XqAdvisor:
		// 仕/士：九宫内斜走一格
		for _, d := range xqDiags {
			tx, ty := x+d[0], y+d[1]
			if !xqInPalace(color, tx, ty) {
				continue
			}
			add(tx, ty)
		}

	case XqGeneral:
		// 帅/将：九宫内直走一格
		for _, d := range xqDirs {
			tx, ty := x+d[0], y+d[1]
			if !xqInPalace(color, tx, ty) {
				continue
			}
			add(tx, ty)
		}
		// 白脸将：同列中间无子时可直接吃对方将帅（也用于判定「将帅照面」不合法）
		for _, dy := range []int{1, -1} {
			cy := y + dy
			for b.InBounds(x, cy) {
				target := b.At(x, cy)
				if target == ColorEmpty {
					cy += dy
					continue
				}
				if XqType(target) == XqGeneral && XqColor(target) != color {
					out = append(out, xqMove{FromX: x, FromY: y, ToX: x, ToY: cy})
				}
				break
			}
		}

	case XqSoldier:
		// 兵/卒：只能向前一格；过河后可左右走一格，不能后退
		add(x, y+xqForward(color))
		if xqCrossedRiver(color, y) {
			add(x-1, y)
			add(x+1, y)
		}
	}

	return out
}

// xqApplyMove 返回走完这一步后的新棋盘
func xqApplyMove(b *Board, m xqMove) *Board {
	next := b.Clone()
	next.Cells[m.ToY][m.ToX] = next.Cells[m.FromY][m.FromX]
	next.Cells[m.FromY][m.FromX] = ColorEmpty
	return next
}

// xqGeneralPos 找到某方将帅的位置
func xqGeneralPos(b *Board, color int) (x, y int, ok bool) {
	target := XqPiece(color, XqGeneral)
	for cy := 0; cy < b.H; cy++ {
		for cx := 0; cx < b.W; cx++ {
			if b.Cells[cy][cx] == target {
				return cx, cy, true
			}
		}
	}
	return 0, 0, false
}

// xqAttackedBy 目标点是否被 byColor 一方攻击
func xqAttackedBy(b *Board, x, y, byColor int) bool {
	for cy := 0; cy < b.H; cy++ {
		for cx := 0; cx < b.W; cx++ {
			piece := b.Cells[cy][cx]
			if piece == ColorEmpty || XqColor(piece) != byColor {
				continue
			}
			for _, m := range xqMovesOfPiece(b, cx, cy) {
				if m.ToX == x && m.ToY == y {
					return true
				}
			}
		}
	}
	return false
}

// xqInCheck 某方是否被将军（将帅已被吃也视为被将军）
func xqInCheck(b *Board, color int) bool {
	x, y, ok := xqGeneralPos(b, color)
	if !ok {
		return true
	}
	return xqAttackedBy(b, x, y, opponent(color))
}

// xqLegalMoves 某方全部合法走法（已过滤会导致己方被将军的走法）
func xqLegalMoves(b *Board, color int) []xqMove {
	out := make([]xqMove, 0, 48)
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			piece := b.Cells[y][x]
			if piece == ColorEmpty || XqColor(piece) != color {
				continue
			}
			for _, m := range xqMovesOfPiece(b, x, y) {
				if !xqInCheck(xqApplyMove(b, m), color) {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// xqMoveJSON 下发给前端的合法走法
type xqMoveJSON struct {
	FromX int `json:"fromX"`
	FromY int `json:"fromY"`
	ToX   int `json:"toX"`
	ToY   int `json:"toY"`
}

// xqLegalMovesJSON 生成下发用的合法走法列表；对局未进行时返回空
func xqLegalMovesJSON(b *Board, color int) []xqMoveJSON {
	moves := xqLegalMoves(b, color)
	out := make([]xqMoveJSON, 0, len(moves))
	for _, m := range moves {
		out = append(out, xqMoveJSON{FromX: m.FromX, FromY: m.FromY, ToX: m.ToX, ToY: m.ToY})
	}
	return out
}

// ---------- 落子与终局 ----------

// placeXiangqi 象棋走子：先校验走法，再判将军/将死/困毙
func (g *Game) placeXiangqi(fromX, fromY, x, y, color int, nick string) error {
	if fromX < 0 || fromY < 0 {
		return fmt.Errorf("请先选中要走的棋子")
	}
	if !g.Board.InBounds(fromX, fromY) {
		return fmt.Errorf("起点超出棋盘")
	}
	piece := g.Board.Cells[fromY][fromX]
	if piece == ColorEmpty || XqColor(piece) != color {
		return fmt.Errorf("起点没有你的棋子")
	}
	if !g.Board.InBounds(x, y) {
		return fmt.Errorf("终点超出棋盘")
	}
	target := g.Board.Cells[y][x]
	if target != ColorEmpty && XqColor(target) == color {
		return fmt.Errorf("不能吃自己的棋子")
	}

	move := xqMove{FromX: fromX, FromY: fromY, ToX: x, ToY: y}
	shapeOK := false
	for _, m := range xqMovesOfPiece(g.Board, fromX, fromY) {
		if m == move {
			shapeOK = true
			break
		}
	}
	if !shapeOK {
		return fmt.Errorf("%s不能这样走", XqName(piece))
	}
	if xqInCheck(xqApplyMove(g.Board, move), color) {
		if xqInCheck(g.Board, color) {
			return fmt.Errorf("正在被将军，这一步不能解将")
		}
		return fmt.Errorf("这一步会让将帅被将军")
	}

	g.Board = xqApplyMove(g.Board, move)
	g.Moves = append(g.Moves, Move{
		X: x, Y: y, Color: color, Nick: nick,
		FromX: fromX, FromY: fromY, Piece: piece, Taken: target,
	})
	if target != ColorEmpty {
		g.Captured[color]++
	}
	g.Turn = opponent(color)

	// 终局：先是对方将帅被吃（理论上不会发生），再看对方是否还有合法着法
	oppColor := opponent(color)
	oppNick := g.nickOf(oppColor)
	if _, _, ok := xqGeneralPos(g.Board, oppColor); !ok {
		g.endWin(nick, fmt.Sprintf("%s 吃掉对方将帅，获胜", nick))
		return nil
	}
	if len(xqLegalMoves(g.Board, oppColor)) == 0 {
		if xqInCheck(g.Board, oppColor) {
			g.endWin(nick, fmt.Sprintf("%s 将死 %s，获胜", nick, oppNick))
		} else {
			g.endWin(nick, fmt.Sprintf("%s 无棋可走（困毙），%s 获胜", oppNick, nick))
		}
	}
	return nil
}

// endWin 终局并记录胜方
func (g *Game) endWin(winner, result string) {
	g.endGame()
	g.Winner = winner
	g.Result = result
}

// xqInCheckNow 当前局面下轮到的一方是否被将军（用于快照提示）
func (g *Game) xqInCheckNow() bool {
	if g.Mode != ModeXiangqi || g.Phase != PhasePlaying {
		return false
	}
	return xqInCheck(g.Board, g.Turn)
}
