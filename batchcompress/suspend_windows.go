//go:build windows

package main

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const processSuspendResume = 0x0800

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

// enumerateDescendants returns root and all of its descendants by walking
// the live ParentProcessID graph from a Toolhelp32 snapshot. Dead PIDs
// in transit are simply absent from the snapshot.
func enumerateDescendants(root uint32) ([]uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)

	children := map[uint32][]uint32{}
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return nil, fmt.Errorf("Process32First: %w", err)
	}
	for {
		children[pe.ParentProcessID] = append(children[pe.ParentProcessID], pe.ProcessID)
		if err := windows.Process32Next(snap, &pe); err != nil {
			break // ERROR_NO_MORE_FILES
		}
	}

	out := []uint32{}
	seen := map[uint32]bool{}
	queue := []uint32{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] || pid == 0 {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
		queue = append(queue, children[pid]...)
	}
	return out, nil
}

func suspendOne(pid uint32) error {
	h, err := windows.OpenProcess(
		processSuspendResume|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	r, _, _ := procNtSuspendProcess.Call(uintptr(h))
	if r != 0 {
		return fmt.Errorf("NtSuspendProcess: NTSTATUS 0x%x", r)
	}
	return nil
}

func resumeOne(pid uint32) error {
	h, err := windows.OpenProcess(processSuspendResume, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	r, _, _ := procNtResumeProcess.Call(uintptr(h))
	if r != 0 {
		return fmt.Errorf("NtResumeProcess: NTSTATUS 0x%x", r)
	}
	return nil
}

func processCpuTime100ns(pid uint32) (uint64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	k := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	u := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	return k + u, nil
}

func suspendPID(pid int) error {
	pids, err := enumerateDescendants(uint32(pid))
	if err != nil {
		return fmt.Errorf("enumerate descendants of %d: %w", pid, err)
	}
	if len(pids) == 0 {
		return fmt.Errorf("no live processes for pid=%d", pid)
	}

	cpuBefore := make(map[uint32]uint64, len(pids))
	for _, p := range pids {
		cpuBefore[p], _ = processCpuTime100ns(p)
	}

	suspended := make([]uint32, 0, len(pids))
	for _, p := range pids {
		if err := suspendOne(p); err != nil {
			log.Printf("WARN: suspend pid=%d failed: %v", p, err)
			continue
		}
		suspended = append(suspended, p)
	}
	if len(suspended) == 0 {
		return fmt.Errorf("no processes could be suspended in tree of pid=%d", pid)
	}

	const sampleWindow = 200 * time.Millisecond
	time.Sleep(sampleWindow)

	var totalDriftMs int64
	worst := struct {
		pid     uint32
		driftMs int64
	}{}
	for _, p := range suspended {
		after, _ := processCpuTime100ns(p)
		drift := int64(after-cpuBefore[p]) / 10000
		if drift > worst.driftMs {
			worst.pid = p
			worst.driftMs = drift
		}
		totalDriftMs += drift
	}

	if worst.driftMs > 20 {
		log.Printf("WARN: process tree of pid=%d may NOT be fully suspended — worst pid=%d advanced %dms during a %dms window (tree=%v)",
			pid, worst.pid, worst.driftMs, sampleWindow.Milliseconds(), suspended)
	} else {
		log.Printf("paused process tree from pid=%d (%d processes: %v, total CPU drift %dms over %dms)",
			pid, len(suspended), suspended, totalDriftMs, sampleWindow.Milliseconds())
	}
	return nil
}

func resumePID(pid int) error {
	pids, err := enumerateDescendants(uint32(pid))
	if err != nil {
		return fmt.Errorf("enumerate descendants of %d: %w", pid, err)
	}
	resumed := make([]uint32, 0, len(pids))
	for _, p := range pids {
		if err := resumeOne(p); err != nil {
			log.Printf("WARN: resume pid=%d failed: %v", p, err)
			continue
		}
		resumed = append(resumed, p)
	}
	log.Printf("resumed process tree from pid=%d (%d processes: %v)", pid, len(resumed), resumed)
	return nil
}

// killTree terminates pid and all descendants. Used by Controller.Kill so a
// launcher chain is fully torn down on skip/quit, not just the wrapper.
func killTree(pid int) {
	pids, err := enumerateDescendants(uint32(pid))
	if err != nil {
		log.Printf("WARN: enumerate descendants of %d for kill: %v", pid, err)
		return
	}
	for _, p := range pids {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, p)
		if err != nil {
			continue
		}
		_ = windows.TerminateProcess(h, 1)
		_ = windows.CloseHandle(h)
	}
}
