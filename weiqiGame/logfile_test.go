package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 超过上限时只保留末尾内容，并按行对齐
func TestTrimLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "line-%05d 这是一行日志内容用于填充\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("写日志失败: %v", err)
	}
	before, _ := os.Stat(path)
	if before.Size() <= 200*1024 {
		t.Fatalf("测试数据应大于上限，实际 %d", before.Size())
	}

	trimLogFile(path, 200*1024, 120*1024)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if after.Size() > 200*1024 {
		t.Fatalf("裁剪后应小于上限，实际 %d", after.Size())
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("裁剪后不应为空")
	}
	// 首行必须是完整的日志行（按行对齐，不出现半行）
	if !strings.HasPrefix(lines[0], "line-") || !strings.HasSuffix(lines[0], "填充") {
		t.Fatalf("首行未按行对齐: %q", lines[0])
	}
	// 末行应是原始最后一行
	if !strings.Contains(lines[len(lines)-1], "line-04999") {
		t.Fatalf("末行应为最后一条日志: %q", lines[len(lines)-1])
	}
}

// 未超上限不裁剪
func TestTrimLogFileNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.log")
	content := "只有一点点内容\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写日志失败: %v", err)
	}
	trimLogFile(path, 200*1024, 120*1024)
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Fatalf("未超上限不应改动文件")
	}
}

// 文件不存在时不 panic
func TestTrimLogFileMissing(t *testing.T) {
	trimLogFile(filepath.Join(t.TempDir(), "nope.log"), 200*1024, 120*1024)
}

// 关键行为：文件以 O_APPEND 打开持续写入时，裁剪后新写入仍落在文件末尾
func TestTrimLogFileWithAppendWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.log")
	w, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 5000; i++ {
		fmt.Fprintf(w, "old-%05d 填充内容填充内容填充内容\n", i)
	}
	if st, _ := os.Stat(path); st.Size() <= 200*1024 {
		t.Fatalf("前置数据应超上限")
	}

	trimLogFile(path, 200*1024, 120*1024)

	// 裁剪后继续用同一个 O_APPEND 句柄写入
	if _, err := fmt.Fprintf(w, "NEW-LINE-AFTER-TRIM\n"); err != nil {
		t.Fatalf("裁剪后写入失败: %v", err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "NEW-LINE-AFTER-TRIM") {
		t.Fatalf("裁剪后的新写入丢失")
	}
	// 新内容应在文件末尾（裁剪后文件仍然完整可读）
	if !strings.HasSuffix(strings.TrimRight(text, "\n"), "NEW-LINE-AFTER-TRIM") {
		t.Fatalf("新写入未落在末尾")
	}
	if st, _ := os.Stat(path); st.Size() > 200*1024 {
		t.Fatalf("裁剪后大小应受控，实际 %d", st.Size())
	}
}
