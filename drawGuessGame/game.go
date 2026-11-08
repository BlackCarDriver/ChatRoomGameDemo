package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// ==================== 游戏阶段 ====================

const (
	PhaseWaiting  = "waiting"   // 等待开始
	PhaseSelect   = "selecting" // 画手选题中
	PhasePlaying  = "playing"   // 绘画猜词中
	PhaseRoundEnd = "roundEnd"  // 回合结算展示
	PhaseGameOver = "gameOver"  // 整局结束（排名）
)

// 计分规则
const (
	ScoreFirst  = 3 // 第一个答对
	ScoreSecond = 2 // 第二个答对
	ScoreOther  = 1 // 其他答对
	ScoreDrawer = 1 // 画手每有一个答对者得分
)

// Player 参与本局游戏的玩家
type Player struct {
	Nick        string
	Score       int
	HasDrawn    bool // 本轮是否已当过画手
	Answered    bool // 本回合是否已答对
	AnswerOrder int  // 本回合答对顺序（0=未答对）
	EnterIndex  int  // 进房顺序（决定画手轮换起点）
}

// RoundResult 单回合结算数据
type RoundResult struct {
	Word         string   `json:"word"`
	WordCategory string   `json:"wordCategory"`
	Drawer       string   `json:"drawer"`
	Correct      []string `json:"correct"`
	DrawerScore  int      `json:"drawerScore"` // 画手本回合得分
}

// Game 一局画猜游戏
type Game struct {
	Phase          string
	Players        map[string]*Player // 参与玩家（按昵称）
	PlayerOrder    []string           // 画手轮换顺序（进房序）
	Drawer         string             // 当前画手
	Word           string             // 当前题目
	WordCategory   string
	WordHint       string
	Options        []string // 选题选项（3 个）
	CorrectCount   int      // 本回合答对人数
	Round          int      // 当前回合（从 1）
	TotalRounds    int      // 计划总回合（默认 = 参与人数）
	UsedWords      []string // 本局已用过的词
	UsedCategories []string // 本局已用过的分类（轮换分类）
	CategoryIndex  int      // 当前分类下标
	DrawerIndex    int      // 画手轮换下标

	SelectDeadline int64 // 选题截止（Unix 毫秒）
	RoundDeadline  int64 // 绘画截止（Unix 毫秒）
	ResultDeadline int64 // 结算展示截止
	RoundEndedAt   int64 // 回合结束时刻

	Result  *RoundResult // 最近一回合结算
	Winner  string       // 最终冠军（可空）
	Ranking []RankItem   // 最终排名

	// 画布
	CanvasActions []DrawAction // 本回合绘画动作（新进房者重放）
	CanvasBase64  string       // 回合结束时的画布快照
}

// RankItem 最终排名项
type RankItem struct {
	Nick  string `json:"nick"`
	Score int    `json:"score"`
}

// NewGame 创建一局游戏。
// players 为参与玩家昵称列表（已按进房序排好），totalRounds<=0 表示每人当一次画手。
func NewGame(playerNicks []string, totalRounds int) *Game {
	players := make(map[string]*Player, len(playerNicks))
	order := make([]string, 0, len(playerNicks))
	for i, nick := range playerNicks {
		players[nick] = &Player{Nick: nick, EnterIndex: i}
		order = append(order, nick)
	}
	rounds := totalRounds
	if rounds <= 0 || rounds > len(order) {
		rounds = len(order)
	}
	game := &Game{
		Phase:       PhaseSelect,
		Players:     players,
		PlayerOrder: order,
		TotalRounds: rounds,
		Round:       1,
	}
	game.beginRound()
	return game
}

// AllAnswered 本回合是否已无人可答（全部答对）
func (g *Game) AllAnswered() bool {
	for nick, p := range g.Players {
		if nick == g.Drawer {
			continue
		}
		if !p.Answered {
			return false
		}
	}
	return true
}

// beginRound 开始新一轮（选题阶段）。调用方需持锁。
func (g *Game) beginRound() {
	g.Drawer = g.PlayerOrder[g.DrawerIndex%len(g.PlayerOrder)]
	g.CorrectCount = 0
	for _, p := range g.Players {
		p.Answered = false
		p.AnswerOrder = 0
	}
	g.CanvasActions = nil
	g.CanvasBase64 = ""
	g.Result = nil

	// 轮换分类（保证每回合题材不重）
	categories := library.Categories()
	if len(categories) == 0 {
		categories = []string{""}
	}
	g.CategoryIndex = (g.CategoryIndex + 1) % len(categories)
	g.WordCategory = categories[g.CategoryIndex]

	// 生成 3 个选题（同分类，尽量不重复）
	options := make([]string, 0, 3)
	for len(options) < 3 {
		info, ok := library.RandomWordFromCategory(g.WordCategory, append(g.UsedWords, options...))
		if !ok {
			// 词库空了：允许复用已用词
			info, _ = library.RandomWordFromCategory("", nil)
		}
		if info.Word == "" {
			break
		}
		options = append(options, info.Word)
	}
	if len(options) < 3 {
		for _, w := range []string{"苹果", "太阳", "小狗"} {
			if len(options) >= 3 {
				break
			}
			options = append(options, w)
		}
	}
	rand.Shuffle(len(options), func(i, j int) { options[i], options[j] = options[j], options[i] })
	g.Options = options
	g.Phase = PhaseSelect
}

// SelectWord 画手选题。返回选择的词。
func (g *Game) SelectWord(nick, word string) error {
	if g.Phase != PhaseSelect {
		return fmt.Errorf("当前不在选题阶段")
	}
	if nick != g.Drawer {
		return fmt.Errorf("只有画手 %s 可以选题", g.Drawer)
	}
	ok := false
	for _, o := range g.Options {
		if o == word {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("选项无效")
	}
	g.Word = word
	g.UsedWords = append(g.UsedWords, word)
	g.WordHint = library.Hint(word)
	g.Phase = PhasePlaying
	return nil
}

// StartRound 开画：设定绘画截止时间
func (g *Game) StartRound(roundSeconds int) {
	g.RoundDeadline = time.Now().Add(time.Duration(roundSeconds) * time.Second).UnixMilli()
}

// AppendAction 记录画布动作（供新进房者重放）
func (g *Game) AppendAction(act DrawAction) {
	// 防止动作列表无限膨胀：保留最近 2000 笔
	if len(g.CanvasActions) > 2000 {
		g.CanvasActions = g.CanvasActions[len(g.CanvasActions)-2000:]
	}
	g.CanvasActions = append(g.CanvasActions, act)
}

// ClearCanvas 清空画布动作
func (g *Game) ClearCanvas() {
	g.CanvasActions = []DrawAction{}
}

// UndoAction 撤销最后一笔（仅画手在绘画阶段可调用）
func (g *Game) UndoAction() (DrawAction, bool) {
	if len(g.CanvasActions) == 0 {
		return DrawAction{}, false
	}
	last := g.CanvasActions[len(g.CanvasActions)-1]
	g.CanvasActions = g.CanvasActions[:len(g.CanvasActions)-1]
	return last, true
}

// TryGuess 玩家猜词：命中则记录答对并返回得分。
func (g *Game) TryGuess(nick, input string) (correct bool, score int, reason string) {
	if g.Phase != PhasePlaying {
		return false, 0, "当前不在绘画阶段"
	}
	if nick == g.Drawer {
		return false, 0, "画手不能答题"
	}
	p, ok := g.Players[nick]
	if !ok {
		return false, 0, "你不是本局玩家"
	}
	if p.Answered {
		return false, 0, "你已答对本题"
	}
	if !isCorrectGuess(input, g.Word) {
		return false, 0, ""
	}
	g.CorrectCount++
	p.Answered = true
	p.AnswerOrder = g.CorrectCount
	switch g.CorrectCount {
	case 1:
		score = ScoreFirst
	case 2:
		score = ScoreSecond
	default:
		score = ScoreOther
	}
	p.Score += score
	// 画手得分：答对者+1
	if drawer, ok := g.Players[g.Drawer]; ok {
		drawer.Score += ScoreDrawer
	}
	return true, score, ""
}

// EndRound 结束当前回合，进入结算展示阶段（PhaseRoundEnd）。
// 结算展示结束后由 AdvanceRound 决定进入下一轮选题或整局结束。
func (g *Game) EndRound(cause string) {
	g.Phase = PhaseRoundEnd
	g.RoundEndedAt = time.Now().UnixMilli()
	g.RoundDeadline = 0

	result := &RoundResult{
		Word:         g.Word,
		WordCategory: g.WordCategory,
		Drawer:       g.Drawer,
	}
	if drawer, ok := g.Players[g.Drawer]; ok {
		result.DrawerScore = drawer.Score
	}
	// 答对者按顺序记录
	correct := make([]string, 0)
	for _, p := range g.Players {
		if p.AnswerOrder > 0 {
			correct = append(correct, p.Nick)
		}
	}
	sort.Slice(correct, func(i, j int) bool {
		return g.Players[correct[i]].AnswerOrder < g.Players[correct[j]].AnswerOrder
	})
	result.Correct = correct
	g.Result = result
}

// AdvanceRound 结算展示结束后的推进：
// 还有下一轮 → beginRound（回到选题阶段）；否则整局结束（GameOver）。
// 返回 true 表示整局已结束。
func (g *Game) AdvanceRound() bool {
	g.DrawerIndex++
	if g.Round >= g.TotalRounds {
		g.FinishGame()
		return true
	}
	g.Round++
	g.beginRound()
	return false
}

// FinishGame 整局结束：计算排名
func (g *Game) FinishGame() {
	g.Phase = PhaseGameOver
	rank := make([]RankItem, 0, len(g.Players))
	for nick, p := range g.Players {
		rank = append(rank, RankItem{Nick: nick, Score: p.Score})
	}
	sort.Slice(rank, func(i, j int) bool {
		if rank[i].Score != rank[j].Score {
			return rank[i].Score > rank[j].Score
		}
		return rank[i].Nick < rank[j].Nick
	})
	g.Ranking = rank
	if len(rank) > 0 {
		g.Winner = rank[0].Nick
	}
}

// Reset 整局重置（回到等待），保留玩家列表与分数清零
func (g *Game) Reset() {
	*g = Game{
		Phase:       PhaseWaiting,
		Players:     g.Players,
		PlayerOrder: g.PlayerOrder,
		TotalRounds: g.TotalRounds,
		Round:       1,
		DrawerIndex: 0,
	}
	for _, p := range g.Players {
		p.Score = 0
		p.HasDrawn = false
		p.Answered = false
	}
}

// ==================== 画布动作 ====================

// DrawPoint 绘画点
type DrawPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	P float64 `json:"p"` // 压力 0-1
}

// DrawAction 一笔绘画动作（工具 + 颜色 + 粗细 + 点序列）
type DrawAction struct {
	Tool   string      `json:"tool"`   // pencil | marker | brush | eraser
	Color  string      `json:"color"`  // 十六进制颜色
	Size   float64     `json:"size"`   // 笔宽
	Points []DrawPoint `json:"points"` // 轨迹点
}

// DrawerNick 画手昵称
func (g *Game) DrawerNick() string {
	return g.Drawer
}

// playerScoreSummary 生成各玩家当前总分的展示行
func (g *Game) playerScoreSummary() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(g.Players))
	for nick, p := range g.Players {
		out = append(out, map[string]interface{}{
			"nick":  nick,
			"score": p.Score,
			"drawn": p.HasDrawn,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]["score"].(int)
		b := out[j]["score"].(int)
		if a != b {
			return a > b
		}
		return out[i]["nick"].(string) < out[j]["nick"].(string)
	})
	return out
}

// rankingText 最终排名文本（用于结果弹窗）
func (g *Game) rankingText() string {
	if len(g.Ranking) == 0 {
		return ""
	}
	lines := make([]string, 0, len(g.Ranking))
	for i, r := range g.Ranking {
		medal := []string{"🥇", "🥈", "🥉"}
		prefix := ""
		if i < len(medal) {
			prefix = medal[i]
		} else {
			prefix = fmt.Sprintf("%d.", i+1)
		}
		lines = append(lines, fmt.Sprintf("%s %s · %d分", prefix, r.Nick, r.Score))
	}
	return strings.Join(lines, "\n")
}
