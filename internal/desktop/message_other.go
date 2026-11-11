//go:build desktop && !windows

package desktop

// ShowError does nothing on macOS and Linux: the first line of the tray
// menu shows the error there (and the log file has it).
func ShowError(string) {}
