package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

// ============================ 日志自动裁剪 ============================
//
// 启动脚本用 `>>` 把 stdout/stderr 追加到日志文件（fd 带 O_APPEND）。
// 日志长期运行会无限增长，这里定期检查文件大小：超过上限时只保留末尾一段，
// 按行边界对齐写回；因为写入方是 O_APPEND，裁剪后新的日志仍会追加到文件末尾。
//
// 只做“保留尾巴”的裁剪，不滚动生成 .1/.2 备份文件，避免磁盘占用翻倍。

const (
	// logTrimInterval 检查日志大小的间隔
	logTrimInterval = 60 * time.Second
	// logKeepRatio 裁剪后保留的比例（相对上限），留出空间避免频繁裁剪
	logKeepRatio = 0.6
)

// startLogTrim 启动日志裁剪协程。
// path 为空表示不启用；maxKB<=0 时使用默认 200KB。
func startLogTrim(path string, maxKB int) {
	if path == "" {
		return
	}
	if maxKB <= 0 {
		maxKB = 200
	}
	maxBytes := int64(maxKB) * 1024
	keepBytes := int64(float64(maxBytes) * logKeepRatio)

	// 启动时先裁剪一次，避免历史遗留的超大日志
	trimLogFile(path, maxBytes, keepBytes)

	go func() {
		ticker := time.NewTicker(logTrimInterval)
		defer ticker.Stop()
		for range ticker.C {
			trimLogFile(path, maxBytes, keepBytes)
		}
	}()
}

// trimLogFile 文件超过上限时只保留末尾 keepBytes（按行边界对齐）
func trimLogFile(path string, maxBytes, keepBytes int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxBytes {
		return
	}
	size := info.Size()
	start := size - keepBytes
	if start < 0 {
		start = 0
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return
	}
	buf := make([]byte, size-start)
	n, readErr := io.ReadFull(f, buf)
	f.Close()
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return
	}
	buf = buf[:n]

	// 对齐到下一行开头，避免留下半行
	if idx := bytes.IndexByte(buf, '\n'); idx >= 0 && idx+1 < len(buf) {
		buf = buf[idx+1:]
	}

	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return
	}
	log.Printf("[log] 日志已达 %dKB（上限 %dKB），已裁剪为最后 %dKB",
		size/1024, maxBytes/1024, int64(len(buf))/1024)
}

var _ = fmt.Sprintf
