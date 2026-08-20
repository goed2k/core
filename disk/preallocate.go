package disk

// PreallocateKind 描述 DesktopFileHandler.Preallocate 在当前 GOOS 上的真实能力。
// Settings.UseSparseFiles / PreallocateDiskSpace 只表达意图；实际效果以本值为准。
type PreallocateKind int

const (
	// PreallocateTruncateOnly 仅 Truncate 扩展逻辑大小，不保证稀疏孔或占盘。
	// 用于 darwin 及其他非 Linux/Windows 平台。
	PreallocateTruncateOnly PreallocateKind = iota
	// PreallocateLinuxFallocate 非稀疏时调用 unix.Fallocate 尽量占盘；
	// 稀疏时只 Truncate（Linux 空洞文件通常不占满簇）。
	PreallocateLinuxFallocate
	// PreallocateWindowsNTFS 稀疏时先 FSCTL_SET_SPARSE 再 Truncate；
	// 非稀疏时用 FileAllocationInfo 尽量占盘。不假装已 fallocate。
	PreallocateWindowsNTFS
)

// PreallocateSemantics 返回当前平台 Preallocate 的真实语义。
func PreallocateSemantics() PreallocateKind {
	return preallocateKind
}

func (k PreallocateKind) String() string {
	switch k {
	case PreallocateLinuxFallocate:
		return "linux-fallocate"
	case PreallocateWindowsNTFS:
		return "windows-ntfs"
	default:
		return "truncate-only"
	}
}
