package internal

import (
	"errors"
	"runtime"
	"syscall"
)

type speculationControlArgsValues struct {
	option   uintptr
	suboption uintptr
	disable  uintptr
}

func speculationControlArgs() speculationControlArgsValues {
	return speculationControlArgsValues{option: 53, suboption: 0, disable: 4}
}

func EnableSpeculationStoreBypassMitigation() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	args := speculationControlArgs()
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, args.option, args.suboption, args.disable, 0, 0, 0)
	if errno != 0 {
		return errors.New(errno.Error())
	}

	return nil
}
