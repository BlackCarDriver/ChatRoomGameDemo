package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// errNoChange 表示回调没有修改账号，Store 不需要写盘。
var errNoChange = errors.New("no change")

// Store 负责账号数据和 JSON 持久化。
//
// 所有金币变动都会同步写盘：写临时文件 -> fsync -> rename。这样服务器重启时
// 磁盘上一定是最新状态，最多丢失正在写的那一次操作。每 5 分钟额外刷新一次
// players.json.bak，主文件损坏时回退。
type Store struct {
	mu         sync.Mutex
	dir        string
	path       string
	bakPath    string
	lastBackup time.Time
	data       StoreData
}

func NewStore(dir string) *Store {
	if dir == "" {
		dir = "data"
	}
	return &Store{
		dir:     dir,
		path:    filepath.Join(dir, "players.json"),
		bakPath: filepath.Join(dir, "players.json.bak"),
		data: StoreData{
			Version:  storeVersion,
			Accounts: map[string]*Account{},
		},
	}
}

// Load 读取存档。主文件损坏时回退 .bak；两者都不存在则从空存档开始。
func (s *Store) Load() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	mainErr := s.loadFile(s.path)
	if mainErr == nil {
		log.Printf("[store] 已加载 %s（%d 个账号）", s.path, len(s.data.Accounts))
		return nil
	}
	if os.IsNotExist(mainErr) {
		// 主文件不存在，尝试备份
		bakErr := s.loadFile(s.bakPath)
		if bakErr == nil {
			log.Printf("[store] 主文件不存在，已从 %s 恢复（%d 个账号）", s.bakPath, len(s.data.Accounts))
			return nil
		}
		if os.IsNotExist(bakErr) {
			log.Printf("[store] 新存档 %s", s.path)
			return nil
		}
		return fmt.Errorf("读取备份存档失败: %w", bakErr)
	}

	// 主文件存在但损坏
	bakErr := s.loadFile(s.bakPath)
	if bakErr == nil {
		log.Printf("[store] 主文件损坏（%v），已从 %s 恢复（%d 个账号）", mainErr, s.bakPath, len(s.data.Accounts))
		return nil
	}
	return fmt.Errorf("主存档损坏（%v），备份也不可用（%v）", mainErr, bakErr)
}

func (s *Store) loadFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return fmt.Errorf("空文件")
	}
	var data StoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data.Accounts == nil {
		data.Accounts = map[string]*Account{}
	}
	// 补全缺失字段，避免旧存档出现零值异常
	for nick, acc := range data.Accounts {
		if acc == nil {
			delete(data.Accounts, nick)
			continue
		}
		if acc.Nick == "" {
			acc.Nick = nick
		}
		if acc.Round.Phase == "" {
			acc.Round.Phase = phaseIdle
		}
	}
	s.data = data
	return nil
}

// Save 显式写盘（优雅退出时使用）。
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// Backup 刷新 players.json.bak。
func (s *Store) Backup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backupLocked()
}

func (s *Store) saveLocked() error {
	if s.data.Accounts == nil {
		s.data.Accounts = map[string]*Account{}
	}
	s.data.Version = storeVersion
	s.data.SavedAt = time.Now().Format(time.RFC3339)

	raw, err := json.MarshalIndent(&s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return err
	}
	if time.Since(s.lastBackup) > 5*time.Minute {
		s.backupLocked()
		s.lastBackup = time.Now()
	}
	return nil
}

func (s *Store) backupLocked() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	if err := os.WriteFile(s.bakPath, raw, 0o644); err != nil {
		log.Printf("[store] 写备份失败: %v", err)
	}
}

// AccountNicks 返回账号昵称快照，用于遍历时避免长时间持锁。
func (s *Store) AccountNicks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data.Accounts))
	for nick := range s.data.Accounts {
		out = append(out, nick)
	}
	sort.Strings(out)
	return out
}

// Get 返回账号副本。
func (s *Store) Get(nick string) (*Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.data.Accounts[nick]
	if !ok {
		return nil, false
	}
	copied := *acc
	return &copied, true
}

// All 返回全部账号副本。
func (s *Store) All() []*Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Account, 0, len(s.data.Accounts))
	for _, acc := range s.data.Accounts {
		copied := *acc
		out = append(out, &copied)
	}
	return out
}

// Ensure 取账号，不存在则创建。返回账号副本和是否新建。
func (s *Store) Ensure(nick, avatar, frame string, roleID int, now time.Time) (*Account, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acc, ok := s.data.Accounts[nick]; ok {
		changed := false
		if avatar != "" && acc.Avatar != avatar {
			acc.Avatar = avatar
			changed = true
		}
		if frame != "" && acc.Frame != frame {
			acc.Frame = frame
			changed = true
		}
		if roleID != 0 && acc.RoleID != roleID {
			acc.RoleID = roleID
			changed = true
		}
		if changed {
			acc.UpdatedAt = now.UnixMilli()
			if err := s.saveLocked(); err != nil {
				return nil, false, err
			}
		}
		copied := *acc
		return &copied, false, nil
	}

	acc := &Account{
		Nick:      nick,
		Avatar:    avatar,
		Frame:     frame,
		RoleID:    roleID,
		CreatedAt: now.UnixMilli(),
		UpdatedAt: now.UnixMilli(),
		Round:     Round{Phase: phaseIdle},
	}
	s.data.Accounts[nick] = acc
	if err := s.saveLocked(); err != nil {
		return nil, false, err
	}
	copied := *acc
	return &copied, true, nil
}

// Update 在锁内修改一个账号并写盘。回调返回 errNoChange 时不写盘。
func (s *Store) Update(nick string, fn func(*Account) error) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.data.Accounts[nick]
	if !ok {
		return nil, fmt.Errorf("账号不存在")
	}
	err := fn(acc)
	if err == errNoChange {
		copied := *acc
		return &copied, nil
	}
	if err != nil {
		return nil, err
	}
	acc.UpdatedAt = time.Now().UnixMilli()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copied := *acc
	return &copied, nil
}

// UpdateAll 批量修改并只写一次盘。返回发生变更的账号副本。
func (s *Store) UpdateAll(fn func(*Account) bool) ([]*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := make([]*Account, 0)
	for _, acc := range s.data.Accounts {
		if fn(acc) {
			acc.UpdatedAt = time.Now().UnixMilli()
			copied := *acc
			changed = append(changed, &copied)
		}
	}
	if len(changed) == 0 {
		return changed, nil
	}
	if err := s.saveLocked(); err != nil {
		return changed, err
	}
	return changed, nil
}

// ResetForTests 仅测试使用。
func (s *Store) ResetForTests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = StoreData{Version: storeVersion, Accounts: map[string]*Account{}}
}
