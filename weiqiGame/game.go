package main

import (
	"fmt"
	"time"
)

// 棋子颜色
const (
	ColorEmpty = 0
	ColorBlack = 1
	ColorWhite = 2
)

// BoardSizeStandard 国际标准围棋棋盘路数（19×19）
const BoardSizeStandard = 19

// BoardSizeGomoku 标准五子棋棋盘路数（15×15）
const BoardSizeGomoku = 15

// 象棋棋盘尺寸：9 路（列）× 10 线（行），棋子走交叉点
const (
	BoardColsXiangqi = 9
	BoardRowsXiangqi = 10
)

// 玩法模式
const (
	ModeGo      = "go"      // 围棋
	ModeGomoku  = "gomoku"  // 五子棋
	ModeXiangqi = "xiangqi" // 象棋
)

// maxUndoPerGame 每局每方最多悔棋次数
const maxUndoPerGame = 3

// BoardDimsForMode 各玩法的标准棋盘尺寸（固定，不可配置）：
// 围棋 19×19、五子棋 15×15、象棋 9 列 × 10 行。
func BoardDimsForMode(mode string) (cols, rows int) {
	switch mode {
	case ModeGomoku:
		return BoardSizeGomoku, BoardSizeGomoku
	case ModeXiangqi:
		return BoardColsXiangqi, BoardRowsXiangqi
	default:
		return BoardSizeStandard, BoardSizeStandard
	}
}

// BoardSizeForMode 各玩法的标准棋盘路数（取长边，供旧版客户端展示用）
func BoardSizeForMode(mode string) int {
	cols, rows := BoardDimsForMode(mode)
	if rows > cols {
		return rows
	}
	return cols
}

// ModeLabel 玩法中文名
func ModeLabel(mode string) string {
	switch mode {
	case ModeGomoku:
		return "五子棋"
	case ModeXiangqi:
		return "象棋"
	default:
		return "围棋"
	}
}

// Point 棋盘坐标
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Move 一手棋
type Move struct {
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Color    int    `json:"color"`
	Pass     bool   `json:"pass"`
	Captured int    `json:"captured"`
	Nick     string `json:"nick"`
	// Timeout 该手因思考超时被判为放弃
	Timeout bool `json:"timeout,omitempty"`

	// 象棋专用：起点坐标、移动的棋子编码、被吃棋子编码（0 表示空点）
	FromX int `json:"fromX,omitempty"`
	FromY int `json:"fromY,omitempty"`
	Piece int `json:"piece,omitempty"`
	Taken int `json:"taken,omitempty"`
}

// Board 棋盘。围棋/五子棋为正方形（W=H），象棋为 9 列 × 10 行。
// Cells 按行存储：Cells[y][x]。
// 围棋/五子棋的格子存颜色（0/1/2），象棋存棋子编码（见 xiangqi.go）。
type Board struct {
	W     int
	H     int
	Cells [][]int
}

// NewBoard 创建空棋盘
func NewBoard(w, h int) *Board {
	cells := make([][]int, h)
	for i := range cells {
		cells[i] = make([]int, w)
	}
	return &Board{W: w, H: h, Cells: cells}
}

// Clone 复制棋盘
func (b *Board) Clone() *Board {
	cells := make([][]int, b.H)
	for i := range cells {
		cells[i] = make([]int, b.W)
		copy(cells[i], b.Cells[i])
	}
	return &Board{W: b.W, H: b.H, Cells: cells}
}

// InBounds 坐标是否在棋盘内
func (b *Board) InBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < b.W && y < b.H
}

// At 读取坐标（围棋/五子棋为颜色，象棋为棋子编码）
func (b *Board) At(x, y int) int {
	if !b.InBounds(x, y) {
		return ColorEmpty
	}
	return b.Cells[y][x]
}

// neighbors 返回相邻点
func (b *Board) neighbors(x, y int) []Point {
	out := make([]Point, 0, 4)
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		nx, ny := x+d[0], y+d[1]
		if b.InBounds(nx, ny) {
			out = append(out, Point{nx, ny})
		}
	}
	return out
}

// group 广度优先收集同色棋块及其气
func (b *Board) group(x, y int) (stones []Point, liberties map[Point]bool) {
	color := b.At(x, y)
	if color == ColorEmpty {
		return nil, nil
	}
	liberties = map[Point]bool{}
	visited := map[Point]bool{}
	queue := []Point{{x, y}}
	visited[Point{x, y}] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		stones = append(stones, cur)
		for _, n := range b.neighbors(cur.X, cur.Y) {
			v := b.At(n.X, n.Y)
			if v == ColorEmpty {
				liberties[n] = true
				continue
			}
			if v == color && !visited[n] {
				visited[n] = true
				queue = append(queue, n)
			}
		}
	}
	return stones, liberties
}

// PlayResult 落子结果
type PlayResult struct {
	Board    *Board
	Captured int
}

// Play 尝试落子。返回错误表示不合法（自杀、打劫、占用）。
func (b *Board) Play(x, y, color, koX, koY int) (result PlayResult, err error) {
	if !b.InBounds(x, y) {
		return result, fmt.Errorf("落子超出棋盘")
	}
	if b.At(x, y) != ColorEmpty {
		return result, fmt.Errorf("该位置已有棋子")
	}

	next := b.Clone()
	next.Cells[y][x] = color
	opponent := ColorBlack
	if color == ColorBlack {
		opponent = ColorWhite
	}

	// 提掉无气的对方棋块
	captured := 0
	checked := map[Point]bool{}
	for _, n := range next.neighbors(x, y) {
		if next.At(n.X, n.Y) != opponent || checked[n] {
			continue
		}
		stones, libs := next.group(n.X, n.Y)
		for _, s := range stones {
			checked[s] = true
		}
		if len(libs) == 0 {
			for _, s := range stones {
				next.Cells[s.Y][s.X] = ColorEmpty
				captured++
			}
		}
	}

	// 自杀判断：自己棋块无气
	if _, libs := next.group(x, y); len(libs) == 0 {
		return result, fmt.Errorf("该位置会导致自杀")
	}

	// 简单打劫：koX/koY 为上一手提子后形成的禁着点，对手不得立即回提
	if koX >= 0 && koY >= 0 && x == koX && y == koY {
		return result, fmt.Errorf("打劫，需先在他处落子")
	}

	return PlayResult{Board: next, Captured: captured}, nil
}

// KoPoint 计算新形成的打劫禁着点（无则返回 -1,-1）
func KoPoint(prev, next *Board, x, y int, captured int) (int, int) {
	if captured != 1 {
		return -1, -1
	}
	stones, libs := next.group(x, y)
	if len(stones) != 1 || len(libs) != 1 {
		return -1, -1
	}
	lib, ok := firstLiberty(libs)
	if !ok {
		return -1, -1
	}
	return lib.X, lib.Y
}

func firstLiberty(libs map[Point]bool) (Point, bool) {
	for p := range libs {
		return p, true
	}
	return Point{}, false
}

// Score 中国规则数子：棋子数 + 单方围住的空点
func (b *Board) Score() (black, white int) {
	black, white = 0, 0
	visited := map[Point]bool{}
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			switch b.Cells[y][x] {
			case ColorBlack:
				black++
			case ColorWhite:
				white++
			}
		}
	}

	// 空区域归属：只被一方包住的空点计入该方
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			p := Point{x, y}
			if b.Cells[y][x] != ColorEmpty || visited[p] {
				continue
			}
			region := []Point{}
			borders := map[int]bool{}
			queue := []Point{p}
			visited[p] = true
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				region = append(region, cur)
				for _, n := range b.neighbors(cur.X, cur.Y) {
					v := b.At(n.X, n.Y)
					if v == ColorEmpty {
						if !visited[n] {
							visited[n] = true
							queue = append(queue, n)
						}
						continue
					}
					borders[v] = true
				}
			}
			if len(borders) != 1 {
				continue
			}
			if borders[ColorBlack] {
				black += len(region)
			} else {
				white += len(region)
			}
		}
	}
	return black, white
}

// 游戏阶段
const (
	PhaseWaiting = "waiting"
	PhasePlaying = "playing"
	PhaseEnded   = "ended"
)

// Game 一局棋
type Game struct {
	// Mode 玩法：go / gomoku
	Mode     string
	Board    *Board
	Turn     int
	Phase    string
	Moves    []Move
	Black    string
	White    string
	KoX      int
	KoY      int
	Passes   int
	Captured map[int]int // 各色提子数
	Winner   string
	Result   string
	// Abandoned 棋手离房导致对局作废（没有胜方）
	Abandoned bool
	// TurnDeadline 当前回合截止时间（服务端毫秒），0 表示不计时
	TurnDeadline int64
	// StartedAt 开局时刻（服务端毫秒），用于展示本局用时
	StartedAt int64
	// EndedAt 终局时刻（服务端毫秒），0 表示对局仍在进行；
	// 本局用时 = EndedAt - StartedAt，前端结束后不再实时走秒
	EndedAt int64
	// UndoUsed 各方已使用的悔棋次数：color -> 次数（每局上限 maxUndoPerGame）
	UndoUsed map[int]int
}

// NewGame 新建一局，棋盘尺寸由玩法决定
func NewGame(mode, black, white string) *Game {
	if mode == "" {
		mode = ModeGo
	}
	board := NewXiangqiBoard()
	if mode != ModeXiangqi {
		cols, rows := BoardDimsForMode(mode)
		board = NewBoard(cols, rows)
	}
	return &Game{
		Mode:      mode,
		Board:     board,
		Turn:      ColorBlack,
		Phase:     PhasePlaying,
		Black:     black,
		White:     white,
		KoX:       -1,
		KoY:       -1,
		Captured:  map[int]int{ColorBlack: 0, ColorWhite: 0},
		UndoUsed:  map[int]int{ColorBlack: 0, ColorWhite: 0},
		StartedAt: time.Now().UnixMilli(),
	}
}

// Place 落子/走子，规则按玩法分派。
// 围棋/五子棋忽略 fromX/fromY；象棋必须给出起点（先选棋子再点终点）。
func (g *Game) Place(nick string, fromX, fromY, x, y int) error {
	if g.Phase != PhasePlaying {
		return fmt.Errorf("对局尚未开始")
	}
	color := ColorBlack
	if nick == g.White {
		color = ColorWhite
	} else if nick != g.Black {
		return fmt.Errorf("你不是本局棋手")
	}
	if color != g.Turn {
		return fmt.Errorf("还没轮到你落子")
	}
	switch g.Mode {
	case ModeGomoku:
		return g.placeGomoku(x, y, color, nick)
	case ModeXiangqi:
		return g.placeXiangqi(fromX, fromY, x, y, color, nick)
	default:
		return g.placeGo(x, y, color, nick)
	}
}

// placeGo 围棋落子：提子、禁自杀、简单打劫
func (g *Game) placeGo(x, y, color int, nick string) error {
	res, err := g.Board.Play(x, y, color, g.KoX, g.KoY)
	if err != nil {
		return err
	}

	g.KoX, g.KoY = KoPoint(g.Board, res.Board, x, y, res.Captured)
	g.Board = res.Board
	g.Captured[color] += res.Captured
	g.Passes = 0
	g.Moves = append(g.Moves, Move{X: x, Y: y, Color: color, Captured: res.Captured, Nick: nick})
	g.Turn = opponent(color)
	return nil
}

// placeGomoku 五子棋落子：无提子/禁着点，落子后先判五连
func (g *Game) placeGomoku(x, y, color int, nick string) error {
	if !g.Board.InBounds(x, y) {
		return fmt.Errorf("落子超出棋盘")
	}
	if g.Board.At(x, y) != ColorEmpty {
		return fmt.Errorf("该位置已有棋子")
	}
	g.Board.Cells[y][x] = color
	g.Passes = 0
	g.Moves = append(g.Moves, Move{X: x, Y: y, Color: color, Nick: nick})
	g.Turn = opponent(color)

	if fiveInRow(g.Board, x, y, color) {
		g.endGame()
		g.Winner = nick
		g.Result = fmt.Sprintf("%s 五子连珠，获胜", nick)
		return nil
	}
	if g.Board.stoneCount() == g.Board.W*g.Board.H {
		g.endDraw("棋盘已满，和棋")
	}
	return nil
}

// fiveInRow 以 (x,y) 为中心，四个方向是否存在五连（含长连）
func fiveInRow(b *Board, x, y, color int) bool {
	for _, d := range [][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}} {
		if countLine(b, x, y, d[0], d[1], color) >= 5 {
			return true
		}
	}
	return false
}

// countLine 统计 (x,y) 在某一方向上的同色连子数（含自身）
func countLine(b *Board, x, y, dx, dy, color int) int {
	count := 1
	for _, sign := range []int{1, -1} {
		cx, cy := x+dx*sign, y+dy*sign
		for b.At(cx, cy) == color {
			count++
			cx += dx * sign
			cy += dy * sign
		}
	}
	return count
}

// stoneCount 棋盘上已有棋子数
func (b *Board) stoneCount() int {
	count := 0
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if b.Cells[y][x] != ColorEmpty {
				count++
			}
		}
	}
	return count
}

// endGame 标记终局并记录终局时刻（幂等：重复调用不覆盖首次时刻）
func (g *Game) endGame() {
	g.Phase = PhaseEnded
	if g.EndedAt == 0 {
		g.EndedAt = time.Now().UnixMilli()
	}
}

// endDraw 终局和棋（无胜方）
func (g *Game) endDraw(result string) {
	g.endGame()
	g.Winner = ""
	g.Result = result
}

// Pass 停一手。双方连续停手则终局。象棋没有停一手。
func (g *Game) Pass(nick string) error {
	if g.Phase != PhasePlaying {
		return fmt.Errorf("对局尚未开始")
	}
	if g.Mode == ModeXiangqi {
		return fmt.Errorf("象棋不能停一手")
	}
	color := ColorBlack
	if nick == g.White {
		color = ColorWhite
	} else if nick != g.Black {
		return fmt.Errorf("你不是本局棋手")
	}
	if color != g.Turn {
		return fmt.Errorf("还没轮到你停手")
	}

	g.Moves = append(g.Moves, Move{Pass: true, Color: color, Nick: nick})
	g.Passes++
	g.Turn = opponent(color)
	if g.Passes >= 2 {
		g.finishByPasses()
	}
	return nil
}

// Timeout 当前回合超出思考时限：
// 围棋/五子棋视为放弃一手，轮到对手；象棋超时直接判负。
// 返回超时的棋手昵称。
func (g *Game) Timeout() string {
	if g.Phase != PhasePlaying {
		return ""
	}
	color := g.Turn
	nick := g.nickOf(color)
	if g.Mode == ModeXiangqi {
		winner := g.nickOf(opponent(color))
		g.Moves = append(g.Moves, Move{Color: color, Nick: nick, Pass: true, Timeout: true})
		g.endWin(winner, fmt.Sprintf("%s 思考超时，%s 获胜", nick, winner))
		return nick
	}
	g.Moves = append(g.Moves, Move{Color: color, Nick: nick, Pass: true, Timeout: true})
	g.Passes++
	g.Turn = opponent(color)
	if g.Passes >= 2 {
		g.finishByPasses()
	}
	return nick
}

// Resign 认输
func (g *Game) Resign(nick string) error {
	if g.Phase != PhasePlaying {
		return fmt.Errorf("对局尚未开始")
	}
	if nick == g.Black {
		g.Winner = g.White
	} else if nick == g.White {
		g.Winner = g.Black
	} else {
		return fmt.Errorf("你不是本局棋手")
	}
	g.endGame()
	g.Result = fmt.Sprintf("%s 认输，%s 胜", nick, g.Winner)
	return nil
}

// finishByPasses 双方连续停手后的收尾：围棋数子，五子棋按和棋
func (g *Game) finishByPasses() {
	if g.Mode == ModeGomoku {
		g.endDraw("双方停手，和棋")
		return
	}
	g.finishByScore()
}

// finishByScore 双方停手后按数子判定胜负
func (g *Game) finishByScore() {
	black, white := g.Board.Score()
	g.endGame()
	if black > white {
		g.Winner = g.Black
	} else if white > black {
		g.Winner = g.White
	} else {
		g.Winner = ""
	}
	g.Result = fmt.Sprintf("黑 %d : 白 %d", black, white)
}

func opponent(color int) int {
	if color == ColorBlack {
		return ColorWhite
	}
	return ColorBlack
}

// nickOf 返回某方棋手的昵称
func (g *Game) nickOf(color int) string {
	if color == ColorBlack {
		return g.Black
	}
	return g.White
}

// Undo 悔棋：回退自己刚下的一手（对手落子前可用），每局最多 maxUndoPerGame 次，
// 无需对手同意。返回该方已用的悔棋次数。
func (g *Game) Undo(nick string) (int, error) {
	if g.Phase != PhasePlaying {
		return 0, fmt.Errorf("对局已结束，不能悔棋")
	}
	color := 0
	switch nick {
	case g.Black:
		color = ColorBlack
	case g.White:
		color = ColorWhite
	default:
		return 0, fmt.Errorf("你不是本局棋手")
	}
	if len(g.Moves) == 0 {
		return 0, fmt.Errorf("还没有走子，无法悔棋")
	}
	last := g.Moves[len(g.Moves)-1]
	if last.Pass {
		return 0, fmt.Errorf("最后一手是停手，无法悔棋")
	}
	if last.Color != color {
		return 0, fmt.Errorf("对手已经落子，现在不能悔棋")
	}
	if g.UndoUsed[color] >= maxUndoPerGame {
		return 0, fmt.Errorf("本局悔棋次数已用完（最多 %d 次）", maxUndoPerGame)
	}

	// 回退最后一手：按剩余着法从初始局面重建棋盘（三种玩法通用）
	g.Moves = g.Moves[:len(g.Moves)-1]
	g.rebuildBoard()
	g.Turn = color
	g.Passes = 0
	g.UndoUsed[color]++
	return g.UndoUsed[color], nil
}

// rebuildBoard 按当前 Moves 从初始局面重建棋盘与提子数（悔棋回退用）。
// 围棋需要重算打劫点；象棋从初始棋形逐个应用走子；五子棋只摆子。
func (g *Game) rebuildBoard() {
	captured := map[int]int{ColorBlack: 0, ColorWhite: 0}
	koX, koY := -1, -1

	switch g.Mode {
	case ModeXiangqi:
		board := NewXiangqiBoard()
		for _, m := range g.Moves {
			if m.Pass {
				continue
			}
			board = xqApplyMove(board, xqMove{FromX: m.FromX, FromY: m.FromY, ToX: m.X, ToY: m.Y})
			if m.Taken != ColorEmpty {
				captured[m.Color]++
			}
		}
		g.Board = board

	case ModeGomoku:
		cols, rows := BoardDimsForMode(g.Mode)
		board := NewBoard(cols, rows)
		for _, m := range g.Moves {
			if m.Pass || !board.InBounds(m.X, m.Y) {
				continue
			}
			board.Cells[m.Y][m.X] = m.Color
		}
		g.Board = board

	default:
		cols, rows := BoardDimsForMode(g.Mode)
		board := NewBoard(cols, rows)
		for _, m := range g.Moves {
			if m.Pass {
				koX, koY = -1, -1
				continue
			}
			res, err := board.Play(m.X, m.Y, m.Color, koX, koY)
			if err != nil {
				continue
			}
			koX, koY = KoPoint(board, res.Board, m.X, m.Y, res.Captured)
			board = res.Board
			captured[m.Color] += res.Captured
		}
		g.Board = board
	}

	g.KoX, g.KoY = koX, koY
	g.Captured = captured
}
