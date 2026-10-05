package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var consoleDLL = syscall.NewLazyDLL("kernel32.dll")
var getConsoleMode = consoleDLL.NewProc("GetConsoleMode")
var setConsoleMode = consoleDLL.NewProc("SetConsoleMode")
var getConsoleInfo = consoleDLL.NewProc("GetConsoleScreenBufferInfo")

const fileFlagDeleteOnClose = 0x04000000

func terminalWidth(file *os.File) int {
	var info struct {
		Size, Cursor struct{ X, Y int16 }
		Attributes   uint16
		Window       struct{ Left, Top, Right, Bottom int16 }
		Maximum      struct{ X, Y int16 }
	}
	ok, _, _ := getConsoleInfo.Call(file.Fd(), uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	return int(info.Window.Right - info.Window.Left + 1)
}

func enableColor(files ...*os.File) bool {
	for _, file := range files {
		var mode uint32
		ok, _, _ := getConsoleMode.Call(file.Fd(), uintptr(unsafe.Pointer(&mode)))
		if ok == 0 {
			return false
		}
		ok, _, _ = setConsoleMode.Call(file.Fd(), uintptr(mode|4))
		if ok == 0 {
			return false
		}
	}
	return true
}

func lockSDKRoot(root string) (func(), error) {
	path := filepath.Join(root, ".asm-update.lock")
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagDeleteOnClose, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot lock SDK directory; another install or update may be running: %w", err)
	}
	file := os.NewFile(uintptr(handle), path)
	return func() { file.Close() }, nil
}

func moveNewDirectory(source, destination string) error {
	if occupied(destination) {
		return fmt.Errorf("destination is already occupied: %s", destination)
	}
	return os.Rename(source, destination)
}

func configureSDK(ctx context.Context, directory, arch string) error { return ctx.Err() }
