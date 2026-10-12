package client

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// nativeLaunch describes the owner's PATH entry and its installation. Always
// execute Path: a wrapper/shim may select a project runtime or load settings.
// Resolved is diagnostic/version identity only, never a reason to bypass it.
type nativeLaunch struct{ Path, Resolved, Installation string }

func resolveNativeLaunch(bin string) (nativeLaunch, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nativeLaunch{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nativeLaunch{}, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nativeLaunch{}, err
	}
	v := nativeLaunch{Path: path, Resolved: resolved, Installation: "direct"}
	normalized := filepath.ToSlash(resolved)
	switch {
	case strings.Contains(normalized, "/node_modules/"):
		v.Installation = "npm-compatible"
	case strings.Contains(filepath.ToSlash(path), "/mise/shims/") || strings.Contains(normalized, "/mise/installs/"):
		v.Installation = "mise"
	default:
		// Inspection never executes discovery code or prints a private wrapper.
		f, e := os.Open(path)
		if e == nil {
			prefix, _ := io.ReadAll(io.LimitReader(f, 4096))
			f.Close()
			if strings.HasPrefix(string(prefix), "#!") {
				v.Installation = "script/launcher"
				if strings.Contains(string(prefix), "mise ") {
					v.Installation = "mise launcher"
				}
			}
		}
	}
	return v, nil
}
