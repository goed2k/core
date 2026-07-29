package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	ed2k "github.com/goed2k/core"
)

func allTransfersFinished(transfers []ed2k.TransferSnapshot) bool {
	if len(transfers) == 0 {
		return false
	}
	for _, tr := range transfers {
		if tr.Status.State != ed2k.Finished {
			return false
		}
	}
	return true
}

func snapshotPaths(transfers []ed2k.TransferSnapshot) []string {
	paths := make([]string, 0, len(transfers))
	for _, tr := range transfers {
		if tr.FilePath != "" {
			paths = append(paths, tr.FilePath)
		}
	}
	return paths
}

// shouldAutoExit 判断是否应自动退出 TUI。
// 当设置了 --timeout 或通过 --link 启动了任务时，全部完成或超时后退出。
func shouldAutoExit(deadline time.Time, targetPaths []string, transfers []ed2k.TransferSnapshot, now time.Time) (exit bool, message string) {
	finished := allTransfersFinished(transfers)
	paths := snapshotPaths(transfers)
	if len(paths) == 0 {
		paths = append([]string(nil), targetPaths...)
	}

	hasDeadline := !deadline.IsZero()
	autoOnComplete := hasDeadline || len(targetPaths) > 0

	if hasDeadline && now.After(deadline) {
		if finished {
			return true, completionMessage(paths)
		}
		return true, timeoutMessage(paths)
	}

	if autoOnComplete && finished {
		return true, completionMessage(paths)
	}
	return false, ""
}

func (m tuiModel) applyAutoExit() (tuiModel, tea.Cmd) {
	exit, message := shouldAutoExit(m.deadline, m.targetPaths, m.transfers, time.Now())
	if !exit {
		return m, nil
	}
	m.nextAction = managerActionQuit
	m.quitMessage = message
	return m, tea.Quit
}
