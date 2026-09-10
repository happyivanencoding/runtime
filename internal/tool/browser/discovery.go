package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Executable struct {
	Path string
	Kind Kind
}

func FindExecutable(configured string, requested Kind) (Executable, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		path := filepath.Clean(configured)
		if isExecutable(path) {
			return Executable{Path: path, Kind: requestedKind(requested)}, nil
		}
		return Executable{}, browserError(ErrNotFound, "configured Chromium browser executable was not found", "discovery", &ErrorDetails{Path: path}, nil)
	}
	if requested == "" {
		requested = BrowserAuto
	}
	for _, candidate := range executableCandidates(runtime.GOOS, requested) {
		if isExecutable(candidate.Path) {
			return candidate, nil
		}
	}
	return Executable{}, browserError(ErrNotFound, "Chrome, Chromium, or Edge was not found", "discovery", &ErrorDetails{Browser: requested}, nil)
}

func requestedKind(requested Kind) Kind {
	if requested == "" || requested == BrowserAuto {
		return BrowserAuto
	}
	return requested
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func executableCandidates(goos string, requested Kind) []Executable {
	kinds := []Kind{BrowserChrome, BrowserChromium, BrowserEdge}
	if requested != "" && requested != BrowserAuto {
		kinds = []Kind{requested}
	}
	var result []Executable
	for _, kind := range kinds {
		for _, path := range pathsForKind(goos, kind) {
			result = append(result, Executable{Path: path, Kind: kind})
		}
	}
	return result
}

func pathsForKind(goos string, kind Kind) []string {
	home, _ := os.UserHomeDir()
	switch goos {
	case "darwin":
		switch kind {
		case BrowserChrome:
			return compactPaths(
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			)
		case BrowserChromium:
			return compactPaths(
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
				filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
			)
		case BrowserEdge:
			return compactPaths(
				"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
				filepath.Join(home, "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
			)
		}
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")
		// Dynamic MCP/stdio hosts may intentionally start Runtime with a narrow
		// environment. Browser discovery must not depend on ProgramFiles or
		// LOCALAPPDATA being forwarded when the standard Windows locations are
		// still unambiguous from the user home drive.
		drive := filepath.VolumeName(home)
		if drive == "" {
			drive = "C:"
		}
		if strings.TrimSpace(programFiles) == "" {
			programFiles = filepath.Join(drive+string(filepath.Separator), "Program Files")
		}
		if strings.TrimSpace(programFilesX86) == "" {
			programFilesX86 = filepath.Join(drive+string(filepath.Separator), "Program Files (x86)")
		}
		if strings.TrimSpace(localAppData) == "" && strings.TrimSpace(home) != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		switch kind {
		case BrowserChrome:
			return compactPaths(
				filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
			)
		case BrowserChromium:
			return compactPaths(
				filepath.Join(programFiles, "Chromium", "Application", "chrome.exe"),
				filepath.Join(localAppData, "Chromium", "Application", "chrome.exe"),
			)
		case BrowserEdge:
			return compactPaths(
				filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
	default:
		switch kind {
		case BrowserChrome:
			return []string{"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/opt/google/chrome/chrome"}
		case BrowserChromium:
			return []string{"/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium"}
		case BrowserEdge:
			return []string{"/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable", "/opt/microsoft/msedge/msedge"}
		}
	}
	return nil
}

func compactPaths(paths ...string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" && path != "." {
			result = append(result, path)
		}
	}
	return result
}
