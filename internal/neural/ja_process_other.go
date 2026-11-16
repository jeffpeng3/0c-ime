//go:build !linux && !darwin

package neural

import (
	"os"
	"os/exec"
)

// 沒有 process group 等價訊號的平台（含 Windows）由宿主直接結束 worker。
// Windows 的 llama-server 孤兒清理需要 Job Object，留給 T34 打包。
func configureJaProcess(*exec.Cmd) {}

func terminateJaProcess(proc *os.Process) {
	if proc != nil {
		_ = proc.Kill()
	}
}
