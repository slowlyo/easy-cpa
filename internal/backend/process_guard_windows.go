//go:build windows

package backend

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsManagedProcessGuard 通过 Job Object 绑定宿主与核心进程树。
type windowsManagedProcessGuard struct {
	job windows.Handle
}

// attachManagedProcessGuard 让宿主退出时自动终止托管核心进程树。
func attachManagedProcessGuard(cmd *exec.Cmd) (managedProcessGuard, error) {
	// 进程对象不存在时无法建立守护关系。
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("托管进程未启动，无法建立退出联动保护")
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Job Object 失败: %w", err)
	}

	limitInfo := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limitInfo.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limitInfo)),
		uint32(unsafe.Sizeof(limitInfo)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("配置 Job Object 失败: %w", err)
	}

	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("打开核心进程句柄失败: %w", err)
	}
	defer windows.CloseHandle(processHandle)

	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("绑定核心进程到 Job Object 失败: %w", err)
	}
	return &windowsManagedProcessGuard{job: job}, nil
}

// Close 释放 Job Object 句柄。
func (g *windowsManagedProcessGuard) Close() error {
	// 守护句柄为空时说明已释放。
	if g == nil || g.job == 0 {
		return nil
	}
	err := windows.CloseHandle(g.job)
	g.job = 0
	if err != nil {
		return fmt.Errorf("释放 Job Object 失败: %w", err)
	}
	return nil
}
