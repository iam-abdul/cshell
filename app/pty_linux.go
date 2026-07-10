//go:build linux

package main

import "golang.org/x/sys/unix"

// termios ioctl request numbers differ per OS. Linux uses the TCGETS family.
const (
	ioctlGetTermios      = unix.TCGETS
	ioctlSetTermios      = unix.TCSETS
	ioctlSetTermiosFlush = unix.TCSETSF
)

// disableDelayedSuspend is a no-op on Linux, which has no ^Y delayed-suspend
// control character (VDSUSP).
func disableDelayedSuspend(*unix.Termios) {}
