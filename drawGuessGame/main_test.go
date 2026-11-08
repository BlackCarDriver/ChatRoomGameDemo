package main

import (
	"testing"
)

// 加载词库（测试环境使用默认路径，若不存在则走内置兜底）
func init() {
	_ = LoadWords("config/draw_words.json")
}

func TestWordLibraryLoaded(t *testing.T) {
	if len(library.Categories()) == 0 {
		t.Fatal("词库分类为空")
	}
	if len(library.flat) == 0 {
		t.Fatal("词库词表为空")
	}
	found := false
	for _, c := range library.Categories() {
		if c == "水果" {
			found = true
		}
	}
	if !found {
		t.Log("注意：未找到「水果」分类（可能是内置兜底词库）")
	}
}

func TestRandomWordExcludesUsed(t *testing.T) {
	// 从全词库连续取 20 个词不应重复（词库上千条足够）
	used := []string{}
	for i := 0; i < 20; i++ {
		info, ok := library.RandomWordFromCategory("", used)
		if !ok {
			t.Fatalf("取词失败")
		}
		for _, u := range used {
			if u == info.Word {
				t.Fatalf("重复取词: %s", info.Word)
			}
		}
		used = append(used, info.Word)
	}
}

func TestNewGameStartsSelecting(t *testing.T) {
	g := NewGame([]string{"A", "B"}, 0)
	if g.Phase != PhaseSelect {
		t.Fatalf("开局应为选题阶段，got %s", g.Phase)
	}
	if g.Drawer != "A" {
		t.Fatalf("画手应为 A，got %s", g.Drawer)
	}
	if len(g.Options) != 3 {
		t.Fatalf("选题应为 3 个，got %d", len(g.Options))
	}
}

func TestSelectWordOnlyDrawer(t *testing.T) {
	g := NewGame([]string{"A", "B"}, 0)
	// 非画手选题应失败
	if err := g.SelectWord("B", g.Options[0]); err == nil {
		t.Fatal("非画手选题应报错")
	}
	if err := g.SelectWord("A", g.Options[0]); err != nil {
		t.Fatalf("画手选题失败: %v", err)
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("选题后应为绘画阶段，got %s", g.Phase)
	}
	if g.Word != g.Options[0] {
		t.Fatal("题目未设置")
	}
}

func TestGuessScoring(t *testing.T) {
	g := NewGame([]string{"A", "B", "C"}, 0)
	// A 是画手
	_ = g.SelectWord("A", g.Options[0])
	// 画手自己答不对
	if ok, _, _ := g.TryGuess("A", g.Word); ok {
		t.Fatal("画手不应能答题")
	}
	// B 先答对得 3 分
	ok, score, _ := g.TryGuess("B", g.Word)
	if !ok || score != ScoreFirst {
		t.Fatalf("B 答对应为 %d 分，got ok=%v score=%d", ScoreFirst, ok, score)
	}
	// C 第二个答对得 2 分
	ok, score, _ = g.TryGuess("C", g.Word)
	if !ok || score != ScoreSecond {
		t.Fatalf("C 第二个答对应为 %d 分，got ok=%v score=%d", ScoreSecond, ok, score)
	}
	// 画手 A 得分：2 个答对者 → +2
	if g.Players["A"].Score != 2 {
		t.Fatalf("画手得分应为 2，got %d", g.Players["A"].Score)
	}
	// 全部答对
	if !g.AllAnswered() {
		t.Fatal("应全部答对")
	}
}

func TestGuessFuzzyMatch(t *testing.T) {
	cases := []struct {
		input, answer string
		want          bool
	}{
		{"苹果", "苹果", true},
		{" 苹果 ", "苹果", true},
		{"答案是苹果", "苹果", true},
		{"答案：苹果", "苹果", true},
		{"我猜是苹果", "苹果", true},
		{"我觉得是苹果", "苹果", true},
		{"apple", "apple", true},
		{"APPLE", "apple", true},
		{"ＡＰＰＬＥ", "apple", true},
		{"苹果", "香蕉", false},
		{"苹", "苹果", false},
		{"苹果汁", "苹果", false},
		{"大苹果", "苹果", false},
		{"苹果啊", "苹果", true},
		{"是苹果吧", "苹果", true},
	}
	for _, c := range cases {
		if got := isCorrectGuess(c.input, c.answer); got != c.want {
			t.Errorf("isCorrectGuess(%q, %q) = %v, want %v", c.input, c.answer, got, c.want)
		}
	}
}

func TestMaskAnswer(t *testing.T) {
	got := maskAnswer("今天天气真好，苹果很好吃", "苹果")
	if got != "今天天气真好，**很好吃" {
		t.Fatalf("打码结果不对: %q", got)
	}
}

func TestGameRotation(t *testing.T) {
	g := NewGame([]string{"A", "B", "C"}, 0) // 3 人，每人一次画手 → 3 回合
	// 第 1 回合
	_ = g.SelectWord("A", g.Options[0])
	g.EndRound("时间到")
	g.AdvanceRound()
	if g.Phase != PhaseSelect {
		t.Fatalf("结算后应回到选题阶段，got %s", g.Phase)
	}
	if g.Drawer != "B" {
		t.Fatalf("第 2 回合画手应为 B，got %s", g.Drawer)
	}
	// 第 2 回合
	_ = g.SelectWord("B", g.Options[0])
	g.EndRound("时间到")
	g.AdvanceRound()
	if g.Drawer != "C" {
		t.Fatalf("第 3 回合画手应为 C，got %s", g.Drawer)
	}
	// 第 3 回合
	_ = g.SelectWord("C", g.Options[0])
	g.EndRound("时间到")
	over := g.AdvanceRound()
	if !over {
		t.Fatal("第 3 回合后应整局结束")
	}
	if g.Phase != PhaseGameOver {
		t.Fatalf("应为游戏结束，got %s", g.Phase)
	}
}

func TestGameRanking(t *testing.T) {
	g := NewGame([]string{"A", "B", "C"}, 0)
	_ = g.SelectWord("A", g.Options[0])
	_, _, _ = g.TryGuess("B", g.Word)
	_, _, _ = g.TryGuess("C", g.Word)
	g.Players["B"].Score += 5
	g.EndRound("时间到")
	g.AdvanceRound()
	g.FinishGame()
	if g.Winner != "B" {
		t.Fatalf("冠军应为 B，got %s", g.Winner)
	}
	if len(g.Ranking) != 3 {
		t.Fatalf("排名应为 3 人，got %d", len(g.Ranking))
	}
	// FinishGame 后玩家列表清空（避免残留展示），但排名保留
	if len(g.Players) != 0 {
		t.Fatalf("FinishGame 后 Players 应清空，got %d", len(g.Players))
	}
}

// TestDrawerMessageMasked 验证画手发送的含答案消息对其他玩家也打码（防剧透）
func TestDrawerMessageMasked(t *testing.T) {
	hub := NewHub(nil)
	hub.lock.Lock()
	hub.clients["A"] = &client{nick: "A", send: make(chan []byte, 8)}
	hub.clients["B"] = &client{nick: "B", send: make(chan []byte, 8)}
	hub.game = NewGame([]string{"A", "B"}, 0)
	g := hub.game
	_ = g.SelectWord("A", g.Options[0]) // A 选词，进入 playing
	hub.lock.Unlock()

	// 画手 A 发送含答案的消息：所有玩家（含画手自己）应看到打码
	hub.OnChatFromBridge("A", "今天天气真好", 0)
	// B 猜词者发送含答案的消息：所有玩家应看到打码
	hub.OnChatFromBridge("B", "这个答案是什么", 0)

	readAll := func(c *client) []string {
		var out []string
		for {
			select {
			case data := <-c.send:
				out = append(out, string(data))
			default:
				return out
			}
		}
	}
	msgsA := readAll(hub.clients["A"])
	msgsB := readAll(hub.clients["B"])

	// B 与 A 收到的所有 chat 消息中，答案的每个字都不应以原样出现
	for _, m := range msgsB {
		if !containsStr(m, `"type":"chat"`) {
			continue
		}
		for _, r := range runesOf(g.Word) {
			if containsRune(m, r) {
				t.Fatalf("B 收到的消息包含答案字 %q（应为打码）: %s", string(r), m)
			}
		}
	}
	for _, m := range msgsA {
		if !containsStr(m, `"type":"chat"`) {
			continue
		}
		for _, r := range runesOf(g.Word) {
			if containsRune(m, r) {
				t.Fatalf("画手 A 收到的消息包含答案字 %q（应为打码）: %s", string(r), m)
			}
		}
	}
}

// TestSetRoundsHostOnly 验证回合数设置：仅房主、非游戏进行中
func TestSetRoundsHostOnly(t *testing.T) {
	hub := NewHub(nil)
	hub.lock.Lock()
	hub.enterSeq++
	hub.clients["A"] = &client{nick: "A", enterSeq: 1}
	hub.enterSeq++
	hub.clients["B"] = &client{nick: "B", enterSeq: 2}
	hub.lock.Unlock()

	// 非房主设置：拒绝
	if err := hub.SetRounds("B", 5); err == nil {
		t.Fatal("非房主设置回合数应被拒绝")
	}
	// 房主设置：成功
	if err := hub.SetRounds("A", 5); err != nil {
		t.Fatalf("房主设置回合数失败: %v", err)
	}
	if hub.totalRounds != 5 {
		t.Fatalf("totalRounds 应为 5，got %d", hub.totalRounds)
	}
	// 上限校验
	if err := hub.SetRounds("A", 999); err == nil {
		t.Fatal("回合数超上限应被拒绝")
	}
	// 游戏进行中锁定：开局后不能改
	hub.lock.Lock()
	hub.clients["A"].ready = true
	hub.clients["B"].ready = true
	hub.lock.Unlock()
	if err := hub.StartGame("A"); err != nil {
		t.Fatalf("开局失败: %v", err)
	}
	if err := hub.SetRounds("A", 3); err == nil {
		t.Fatal("游戏进行中修改回合数应被拒绝")
	}
}

// TestHostCanStartOnly 验证房主机制：仅进房最早者（房主）可开始游戏
func TestHostCanStartOnly(t *testing.T) {
	hub := NewHub(nil)
	hub.lock.Lock()
	hub.enterSeq++
	hub.clients["A"] = &client{nick: "A", enterSeq: 1, ready: true}
	hub.enterSeq++
	hub.clients["B"] = &client{nick: "B", enterSeq: 2, ready: true}
	hub.lock.Unlock()

	if host := hub.hostNick(); host != "A" {
		t.Fatalf("房主应为 A（进房最早），got %s", host)
	}
	// B（非房主）尝试开始：应失败
	if err := hub.StartGame("B"); err == nil {
		t.Fatal("非房主开始游戏应被拒绝")
	}
	// A（房主）开始：应成功
	if err := hub.StartGame("A"); err != nil {
		t.Fatalf("房主开始游戏失败: %v", err)
	}
}

// TestDropLastPlayerResetsGame 验证最后一名玩家断开后房间重置（无残留状态）
func TestDropLastPlayerResetsGame(t *testing.T) {
	hub := NewHub(nil)
	hub.lock.Lock()
	hub.enterSeq++
	hub.clients["A"] = &client{nick: "A", enterSeq: 1, ready: true, send: make(chan []byte, 4)}
	hub.enterSeq++
	hub.clients["B"] = &client{nick: "B", enterSeq: 2, ready: true, send: make(chan []byte, 4)}
	hub.lock.Unlock()
	if err := hub.StartGame("A"); err != nil {
		t.Fatalf("开局失败: %v", err)
	}
	// A、B 都断开（模拟 readPump 顺序：先删 clients，再 handlePlayerDrop）→ 房间重置 game=nil
	hub.lock.Lock()
	delete(hub.clients, "A")
	hub.lock.Unlock()
	hub.handlePlayerDrop("A")
	hub.lock.Lock()
	delete(hub.clients, "B")
	hub.lock.Unlock()
	hub.handlePlayerDrop("B")
	hub.lock.Lock()
	gameNil := hub.game == nil
	clients := len(hub.clients)
	hub.lock.Unlock()
	if !gameNil {
		t.Fatal("最后玩家离开后 game 应为 nil（无残留）")
	}
	if clients != 0 {
		t.Fatalf("clients 应为空，got %d", clients)
	}
}

// 测试辅助
func containsStr(s, sub string) bool {
	return indexOf(s, sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func runesOf(s string) []rune { return []rune(s) }
func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
