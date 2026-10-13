package ui

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func (s *Server) openImage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.p.(ImageOpener)
	if !ok || s.mobile {
		writeErr(w, NotFound("Open in default app is available on a computer."))
		return
	}
	var v struct {
		ID    string `json:"id"`
		Dir   string `json:"dir"`
		Index *int   `json:"index"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if v.ID == "" || v.Index == nil || *v.Index < 0 || (v.Dir != "in" && v.Dir != "out") {
		writeErr(w, NotFound("no such image"))
		return
	}
	writeResult(w, map[string]bool{"ok": true}, p.OpenImage(r.Context(), v.Dir, v.ID, *v.Index))
}

// OpenImage reuses the ordinary manifest-checked, decrypted attachment read.
// Keep a private copy for the asynchronous OS app for one hour; CleanStaging
// removes any copy left by a crash or restart. The sender's filename never
// chooses a path, extension, program or command argument.
func (l *Live) OpenImage(ctx context.Context, dir, msgID string, index int) error {
	r, name, err := l.OpenFile(ctx, dir, msgID, index)
	if err != nil {
		return err
	}
	defer r.Close()
	b := bufio.NewReader(r)
	header, err := b.Peek(512)
	if err != nil && err != io.EOF {
		return Refuse("Couldn’t read this image.")
	}
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[http.DetectContentType(header)]
	if ext == "" {
		return Refuse("Only PNG, JPEG, GIF and WebP images open in the default image app. Use Download for other files.")
	}
	path, cleanup, err := l.a.StageUpload(name, b)
	if err != nil {
		return Refuse("Couldn’t prepare this image for your image app.")
	}
	defer cleanup()
	imagePath := path + ext
	if err := os.Rename(path, imagePath); err != nil {
		return Refuse("Couldn’t prepare this image for your image app.")
	}
	if err := startImageApp(imagePath); err != nil {
		os.Remove(imagePath)
		return Refuse("Couldn’t start your default image app. Try Download.")
	}
	time.AfterFunc(stagedFor, func() { os.Remove(imagePath) })
	return nil
}

func imageAppCommand(goos, path string) []string {
	switch goos {
	case "darwin":
		return []string{"open", path}
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{filepath.Join(root, "System32", "rundll32.exe"), "url.dll,FileProtocolHandler", path}
	default:
		return []string{"xdg-open", path}
	}
}

func startImageApp(path string) error {
	args := imageAppCommand(runtime.GOOS, path)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap it; launching an app is not proof that it displayed a file
	return nil
}
