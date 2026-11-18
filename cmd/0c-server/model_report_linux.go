//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processTreeRSS 回傳引擎本身與其子孫行程的 RSS（bytes）。Linux 的 /proc/<pid>/status 有 VmRSS；
// children 檔列出直接子行程，逐層加總，全部算進程序樹。
func processTreeRSS() (self, children int64) {
	self = procRSS(os.Getpid())
	for _, pid := range descendants(os.Getpid()) {
		children += procRSS(pid)
	}
	return self, children
}

func procRSS(pid int) int64 {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb << 10
	}
	return 0
}

func descendants(pid int) []int {
	seen := map[int]bool{}
	queue := []int{pid}
	var out []int
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(parent), "task", strconv.Itoa(parent), "children"))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(b)) {
			child, err := strconv.Atoi(field)
			if err != nil || seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}
