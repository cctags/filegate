package gate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func hasParentComponent(name string) bool {
	for _, part := range strings.Split(name, string(filepath.Separator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

// Open under the pinned root, then compare actual directory identities and
// ancestors. String prefixes alone cannot detect aliases through symlinks.
func openDirectories(base *os.Root, directories map[string]string) (roots map[string]*os.Root, err error) {
	roots = make(map[string]*os.Root, len(directories))
	defer func() {
		if err != nil {
			closeRoots(roots)
		}
	}()
	baseInfo, err := base.Stat(".")
	if err != nil {
		return roots, err
	}
	infos := make(map[string]os.FileInfo, len(directories))
	for alias, name := range directories {
		if !filepath.IsLocal(name) || filepath.Clean(name) == "." || hasParentComponent(name) {
			return roots, fmt.Errorf("directory %s must be a proper subdirectory without parent components", alias)
		}
		root, e := base.OpenRoot(name)
		if e != nil {
			return roots, fmt.Errorf("directory %s: %w", alias, e)
		}
		roots[alias] = root
		info, e := root.Stat(".")
		if e != nil {
			return roots, e
		}
		if os.SameFile(baseInfo, info) {
			return roots, fmt.Errorf("directory %s resolves to the content root", alias)
		}
		infos[alias] = info
	}
	for alias, root := range roots {
		if err = checkAncestors(root, alias, baseInfo, infos); err != nil {
			return roots, err
		}
	}
	return roots, nil
}

func checkAncestors(root *os.Root, alias string, baseInfo os.FileInfo, infos map[string]os.FileInfo) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { directory.Close() }()
	for range 1024 {
		info, err := directory.Stat()
		if err != nil {
			return err
		}
		if os.SameFile(info, baseInfo) {
			return nil
		}
		for other, candidate := range infos {
			if other != alias && os.SameFile(info, candidate) {
				return fmt.Errorf("directory aliases %s and %s overlap", alias, other)
			}
		}
		// Metadata-only parent traversal is deliberately outside os.Root: it verifies
		// ancestry of an already confined directory, never serves a requested file.
		fd, err := syscall.Openat(int(directory.Fd()), "..", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		parent := os.NewFile(uintptr(fd), "directory ancestor")
		parentInfo, err := parent.Stat()
		if err != nil {
			parent.Close()
			return err
		}
		if os.SameFile(info, parentInfo) {
			parent.Close()
			return errors.New("mapped directory is no longer inside the content root")
		}
		directory.Close()
		directory = parent
	}
	return errors.New("mapped directory ancestry exceeds the depth limit")
}
