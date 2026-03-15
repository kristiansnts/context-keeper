//go:build windows

package hooks

import "os/exec"

func detachCmd(cmd *exec.Cmd) {}
