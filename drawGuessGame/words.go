package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
)

// WordEntry 单个词语条目（沿用旧画猜词库 JSON 格式）
type WordEntry struct {
	Word  string   `json:"word"`  // 词语
	Hints []string `json:"hints"` // 提示词数组
}

// WordCategoryJSON 词库分类（沿用旧格式，可直接复用 config/draw_words.json）
type WordCategoryJSON struct {
	Name  string      `json:"name"`
	Words []WordEntry `json:"words"`
}

// WordLibraryJSON 词库文件根结构
type WordLibraryJSON struct {
	Categories []WordCategoryJSON `json:"categories"`
}

// WordInfo 查询单个词后的元信息
type WordInfo struct {
	Word     string
	Category string
	Hints    []string
	Length   int
}

// wordLibrary 内存词库，启动时加载一次（热更新可选，当前保持静态）
type wordLibrary struct {
	lock       sync.RWMutex
	path       string
	categories []WordCategoryJSON
	flat       []WordInfo // 全量词表（去重后的条目）
}

var library = &wordLibrary{path: "config/draw_words.json"}

// LoadWords 加载词库文件到内存。文件缺失或为空时加载内置兜底词库。
func LoadWords(path string) error {
	library.lock.Lock()
	defer library.lock.Unlock()
	if path != "" {
		library.path = path
	}
	raw, err := os.ReadFile(library.path)
	if err != nil {
		if os.IsNotExist(err) {
			library.categories = fallbackCategories()
		} else {
			return fmt.Errorf("read words file: %w", err)
		}
	} else {
		var lib WordLibraryJSON
		if err := json.Unmarshal(raw, &lib); err != nil {
			return fmt.Errorf("parse words file %s: %w", library.path, err)
		}
		library.categories = lib.Categories
	}
	library.flattenLocked()
	if len(library.flat) == 0 {
		library.categories = fallbackCategories()
		library.flattenLocked()
	}
	return nil
}

// flattenLocked 构建全量词表（调用方需持写锁）
func (l *wordLibrary) flattenLocked() {
	l.flat = nil
	seen := map[string]bool{}
	for _, c := range l.categories {
		for _, w := range c.Words {
			word := strings.TrimSpace(w.Word)
			if word == "" || seen[word] {
				continue
			}
			seen[word] = true
			l.flat = append(l.flat, WordInfo{
				Word:     word,
				Category: c.Name,
				Hints:    w.Hints,
				Length:   len([]rune(word)),
			})
		}
	}
}

// Categories 返回全部分类名
func (l *wordLibrary) Categories() []string {
	l.lock.RLock()
	defer l.lock.RUnlock()
	out := make([]string, 0, len(l.categories))
	for _, c := range l.categories {
		out = append(out, c.Name)
	}
	return out
}

// RandomWordFromCategory 从指定分类随机取一个未用过的词；分类不存在则回退全词库
func (l *wordLibrary) RandomWordFromCategory(category string, used []string) (WordInfo, bool) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	usedSet := make(map[string]bool, len(used))
	for _, u := range used {
		usedSet[u] = true
	}
	var candidates []WordInfo
	for _, c := range l.categories {
		if category != "" && c.Name != category {
			continue
		}
		for _, w := range c.Words {
			word := strings.TrimSpace(w.Word)
			if word == "" || usedSet[word] {
				continue
			}
			candidates = append(candidates, WordInfo{
				Word:     word,
				Category: c.Name,
				Hints:    w.Hints,
				Length:   len([]rune(word)),
			})
		}
	}
	if len(candidates) == 0 {
		// 全部用光：忽略 used 再取
		for _, c := range l.categories {
			if category != "" && c.Name != category {
				continue
			}
			for _, w := range c.Words {
				word := strings.TrimSpace(w.Word)
				if word == "" {
					continue
				}
				candidates = append(candidates, WordInfo{
					Word:     word,
					Category: c.Name,
					Hints:    w.Hints,
					Length:   len([]rune(word)),
				})
			}
		}
	}
	if len(candidates) == 0 {
		return WordInfo{}, false
	}
	return candidates[rand.Intn(len(candidates))], true
}

// WordInfo returns metadata for a given word.
func (l *wordLibrary) WordInfo(word string) (WordInfo, bool) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	for _, c := range l.categories {
		for _, w := range c.Words {
			if strings.TrimSpace(w.Word) == word {
				return WordInfo{
					Word:     word,
					Category: c.Name,
					Hints:    w.Hints,
					Length:   len([]rune(word)),
				}, true
			}
		}
	}
	return WordInfo{}, false
}

// Hint 返回某词的提示文本：优先词库 hints，否则按字数生成
func (l *wordLibrary) Hint(word string) string {
	if info, ok := l.WordInfo(word); ok && len(info.Hints) > 0 {
		return info.Hints[rand.Intn(len(info.Hints))]
	}
	n := len([]rune(word))
	if n <= 1 {
		return "1个字"
	}
	return fmt.Sprintf("%d个字", n)
}

// fallbackCategories 内置兜底词库（词库文件缺失时保证可玩）
func fallbackCategories() []WordCategoryJSON {
	return []WordCategoryJSON{
		{Name: "水果", Words: []WordEntry{{Word: "苹果", Hints: []string{"2个字"}}, {Word: "香蕉", Hints: []string{"2个字"}}, {Word: "西瓜", Hints: []string{"2个字"}}, {Word: "草莓", Hints: []string{"2个字"}}, {Word: "葡萄", Hints: []string{"2个字"}}}},
		{Name: "动物", Words: []WordEntry{{Word: "小狗", Hints: []string{"2个字"}}, {Word: "猫咪", Hints: []string{"2个字"}}, {Word: "兔子", Hints: []string{"2个字"}}, {Word: "大象", Hints: []string{"2个字"}}, {Word: "熊猫", Hints: []string{"2个字"}}}},
		{Name: "职业", Words: []WordEntry{{Word: "医生", Hints: []string{"2个字"}}, {Word: "护士", Hints: []string{"2个字"}}, {Word: "老师", Hints: []string{"2个字"}}, {Word: "警察", Hints: []string{"2个字"}}}},
		{Name: "运动", Words: []WordEntry{{Word: "篮球", Hints: []string{"2个字"}}, {Word: "足球", Hints: []string{"2个字"}}, {Word: "游泳", Hints: []string{"2个字"}}}},
		{Name: "交通", Words: []WordEntry{{Word: "汽车", Hints: []string{"2个字"}}, {Word: "火车", Hints: []string{"2个字"}}, {Word: "飞机", Hints: []string{"2个字"}}}},
	}
}

// ==================== 答题判定 ====================

// normalizeAnswer 规范化用户输入：去首尾空白、全角转半角、统一小写、去常见标点
func normalizeAnswer(s string) string {
	s = strings.TrimSpace(s)
	// 全角转半角
	var b strings.Builder
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		} else if r == 0x3000 {
			r = ' '
		}
		b.WriteRune(r)
	}
	s = b.String()
	// 去除空格与常见标点
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '，', '。', '！', '？', '、', '；', '：', '“', '”', '‘', '’', '（', '）', '【', '】', '.', ',', '!', '?', ';', ':', '(', ')', '-', '_', '~':
			return -1
		}
		return r
	}, s)
	return strings.ToLower(s)
}

// isCorrectGuess 判定是否答对。
// 支持：完全相等（忽略空格/全半角/大小写/标点）、"答案是xxx" 等前缀动词形式、xxx 后带语气后缀。
// 拒绝过短输入与含混描述（如「苹果汁」不算答对「苹果」），避免误判。
func isCorrectGuess(input, answer string) bool {
	if input == "" || answer == "" {
		return false
	}
	normInput := normalizeAnswer(input)
	normAnswer := normalizeAnswer(answer)
	if normAnswer == "" {
		return false
	}
	if len([]rune(normInput)) < len([]rune(normAnswer)) {
		// 输入比答案短：只有 "答案是xxx" 这类带前缀的才可能命中
		return hasAnswerPrefix(normInput, normAnswer)
	}
	// 完全相等
	if normInput == normAnswer {
		return true
	}
	// 前缀动词形式："答案是苹果" / "答案苹果" / "我猜苹果" / "我觉得是苹果"
	if hasAnswerPrefix(normInput, normAnswer) {
		return true
	}
	// 答案后带语气后缀（≤2 字）："苹果啊" / "苹果吧"
	if strings.HasPrefix(normInput, normAnswer) {
		tail := []rune(normInput)[len([]rune(normAnswer)):]
		if len(tail) <= 2 && isToneSuffix(string(tail)) {
			return true
		}
	}
	return false
}

var answerPrefixes = []string{"答案是", "答案", "我猜", "我猜是", "我觉得", "我觉得是", "应该", "应该是", "可能是", "就是", "是"}

// toneSuffixes 常见语气词集合
var toneSuffixes = map[string]bool{"啊": true, "呀": true, "吧": true, "呢": true, "啦": true, "哦": true, "嘛": true, "哈": true, "嗯": true}

func isToneSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !toneSuffixes[string(r)] {
			return false
		}
	}
	return true
}

func hasAnswerPrefix(normInput, normAnswer string) bool {
	for _, p := range answerPrefixes {
		prefix := p + normAnswer
		if strings.HasPrefix(normInput, prefix) {
			return true
		}
	}
	return false
}

// maskAnswer 遮挡消息中与答案重叠的字（防止剧透）。
// 答案本身会被完整遮挡；消息中单个命中答案的字也会被替换为 *
func maskAnswer(content, answer string) string {
	if content == "" || answer == "" {
		return content
	}
	contentRunes := []rune(content)
	answerRunes := []rune(answer)
	for i, c := range contentRunes {
		for _, a := range answerRunes {
			if c == a {
				contentRunes[i] = '*'
				break
			}
		}
	}
	return string(contentRunes)
}
