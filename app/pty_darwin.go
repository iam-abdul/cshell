//go:build darwin

package main

import "golang.org/x/sys/unix"

// termios ioctl request numbers differ per OS. Darwin/BSD use the TIOC* names.
const (
	ioctlGetTermios      = unix.TIOCGETA
	ioctlSetTermios      = unix.TIOCSETA
	ioctlSetTermiosFlush = unix.TIOCSETAF
)

// disableDelayedSuspend turns off ^Y (delayed suspend), which exists on
// Darwin/BSD but not Linux.
func disableDelayedSuspend(tio *unix.Termios) {
	tio.Cc[unix.VDSUSP] = posixVDisable
}
