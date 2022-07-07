package installcni

import (
	"fmt"
	"os"
	"path/filepath"
)

// copyFile copies a file from a source directory to a destination directory.
func copyFile(srcFile, destDir string) error {
	// If the src file does not exist then either the incorrect command line argument was used or
	// the docker container we built is broken somehow.
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		return fmt.Errorf("source %s binary does not exist: %v", srcFile, err)
	}

	filename := filepath.Base(srcFile)
	// If the destDir does not exist then the incorrect command line argument was used or
	// the CNI settings for the kubelet are not correct.
	info, err := os.Stat(destDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("destination directory %s does not exist: %v", destDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("destination directory %s is not a directory: %v", destDir, err)
	}

	// Check if the user bit is enabled in file permission.
	if info.Mode().Perm()&(1<<(uint(7))) == 0 {
		return fmt.Errorf("cannot write to destination directory %s: %v", destDir, err)
	}

	srcBytes, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("could not read %s file: %v", srcFile, err)
	}

	err = os.WriteFile(filepath.Join(destDir, filename), srcBytes, info.Mode())
	if err != nil {
		return fmt.Errorf("error copying %s binary to %s: %v", filename, destDir, err)
	}
	return nil
}

func removeFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Nothing to delete.
		return nil
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("error removing file %s: %v", path, err)
	}
	return nil
}
