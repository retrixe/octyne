package system

import "os"

// IsFileLocked checks if a file is locked.
func IsFileLocked(err error) bool {
	linkErr, ok := err.(*os.LinkError)
	if !ok {
		pathErr, ok := err.(*os.PathError)
		if !ok {
			return false
		}
		return pathErr.Err != nil && pathErr.Err.Error() ==
			"The process cannot access the file because it is being used by another process."
	}
	return linkErr.Err != nil && linkErr.Err.Error() ==
		"The process cannot access the file because it is being used by another process."
}

// CanDeleteFolder checks if a folder can be deleted, checking its children recursively.
func CanDeleteFolder(_ string) (int, string) {
	return 0, "" // TODO: Currently Windows ACLs are not supported.
}
