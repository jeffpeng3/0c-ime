//go:build linux || darwin

package neural

import (
	"os"
	"os/exec"
	"syscall"
)

// configureJaProcess 讓 worker 自成一個 process group。llama-server 是
// worker 的子行程，硬逾時時一起結束，不會留下孤兒。
func configureJaProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateJaProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	// 負 PID 代表整個 process group；worker 與 llama-server 一起收掉。
	if err := syscall.Kill(-proc.Pid, syscall.SIGKILL); err != nil {
		_ = proc.Kill()
	}
}
