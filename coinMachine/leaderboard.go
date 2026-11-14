package main

import "sort"

// Leaderboard 返回净资产榜。排序：净资产降序 -> 总资产降序 -> 昵称升序。
// 返回前 100 名，以及当前玩家自己的条目（即使不在前 100）。
func (h *hub) Leaderboard(nick string) (top []LeaderboardEntry, self *LeaderboardEntry, total int) {
	accounts := h.store.All()
	entries := make([]LeaderboardEntry, 0, len(accounts))
	for _, acc := range accounts {
		entries = append(entries, leaderboardEntryOf(acc))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].NetWorthCents != entries[j].NetWorthCents {
			return entries[i].NetWorthCents > entries[j].NetWorthCents
		}
		if entries[i].TotalAssetsCents != entries[j].TotalAssetsCents {
			return entries[i].TotalAssetsCents > entries[j].TotalAssetsCents
		}
		return entries[i].Nick < entries[j].Nick
	})
	for i := range entries {
		entries[i].Rank = i + 1
	}
	total = len(entries)
	limit := 100
	if len(entries) < limit {
		limit = len(entries)
	}
	top = entries[:limit]
	for i := range entries {
		if entries[i].Nick == nick {
			copied := entries[i]
			self = &copied
			break
		}
	}
	return top, self, total
}
