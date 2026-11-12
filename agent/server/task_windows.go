//go:build windows

package server

import (
	"os/exec"
	"strconv"
	"time"
)

func configureTaskCommand(cmd *exec.Cmd) {
	// powershell 脚本会再拉起 SRBMiner 等子进程。只杀 powershell 本体时
	// 孙子进程仍占着 stdout，cmd.Wait 会一直等到它们自己退出。Windows 没有
	// 进程组，用 taskkill /T 按进程树整棵杀；WaitDelay 作为 taskkill 失效
	// （权限不足等）时的兜底。
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		return kill.Run()
	}
	cmd.WaitDelay = 2 * time.Second
}
