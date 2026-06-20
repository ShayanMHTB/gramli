package web

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// mediaRow is the minimal media info the media/thumb handlers need.
type mediaRow struct {
	LocalPath string
	Type      string
	Status    string
	Thumbnail string
	RemoteURL string
}

func (s *Server) lookupMedia(ctx context.Context, id int64) (mediaRow, bool) {
	var m mediaRow
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(local_path,''), COALESCE(media_type,''), download_status, COALESCE(thumbnail_url,''), COALESCE(remote_url,'')
FROM media WHERE id=?`, id).Scan(&m.LocalPath, &m.Type, &m.Status, &m.Thumbnail, &m.RemoteURL)
	if err != nil {
		return mediaRow{}, false
	}
	return m, true
}

// safeLocalPath resolves a stored local_path and confirms it lives inside the
// downloads directory, defeating path traversal. Stored paths are repo-relative
// (e.g. ".gramli/downloads/owner/shortcode/file.mp4").
func (s *Server) safeLocalPath(stored string) (string, bool) {
	if stored == "" {
		return "", false
	}
	abs, err := filepath.Abs(stored)
	if err != nil {
		return "", false
	}
	root, err := filepath.Abs(s.downloadsDir)
	if err != nil {
		return "", false
	}
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", false
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return "", false
	}
	return abs, true
}

// handleMedia streams a downloaded media file (image or video, with range
// support for video scrubbing).
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	m, ok := s.lookupMedia(r.Context(), id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	abs, ok := s.safeLocalPath(m.LocalPath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filepath.Base(abs), fi.ModTime(), f)
}

// handleThumb returns a thumbnail for a media row:
//   - downloaded image  -> the image file itself
//   - downloaded video  -> an ffmpeg-extracted poster frame (cached)
//   - otherwise         -> remote thumbnail (if enabled) or an inline placeholder
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.placeholder(w)
		return
	}
	m, ok := s.lookupMedia(r.Context(), id)
	if !ok {
		s.placeholder(w)
		return
	}

	if m.Status == "downloaded" {
		if abs, ok := s.safeLocalPath(m.LocalPath); ok {
			if isImageFile(abs) {
				http.ServeFile(w, r, abs)
				return
			}
			if thumb, ok := s.videoThumb(id, abs); ok {
				http.ServeFile(w, r, thumb)
				return
			}
		}
	}

	remote := m.Thumbnail
	if remote == "" {
		remote = m.RemoteURL
	}
	if s.remoteFallback && strings.HasPrefix(remote, "http") {
		http.Redirect(w, r, remote, http.StatusFound)
		return
	}
	s.placeholder(w)
}

func isImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".heic", ".bmp":
		return true
	}
	return false
}

// videoThumb extracts (and caches) a poster frame from a downloaded video using
// ffmpeg. Returns false if ffmpeg is unavailable or extraction fails.
func (s *Server) videoThumb(id int64, videoPath string) (string, bool) {
	out := filepath.Join(s.thumbCacheDir, strconv.FormatInt(id, 10)+".jpg")
	if fi, err := os.Stat(out); err == nil && fi.Size() > 0 {
		return out, true
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", false
	}
	if err := os.MkdirAll(s.thumbCacheDir, 0o755); err != nil {
		return "", false
	}
	cmd := exec.Command(ffmpeg,
		"-y", "-loglevel", "error",
		"-ss", "1",
		"-i", videoPath,
		"-frames:v", "1",
		"-vf", "scale=480:-2",
		out,
	)
	if err := cmd.Run(); err != nil {
		// A 1s seek can overshoot very short clips; retry from the start.
		cmd = exec.Command(ffmpeg, "-y", "-loglevel", "error", "-i", videoPath, "-frames:v", "1", "-vf", "scale=480:-2", out)
		if err := cmd.Run(); err != nil {
			return "", false
		}
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		return "", false
	}
	return out, true
}

// placeholder renders a neutral inline SVG when no image is available.
func (s *Server) placeholder(w http.ResponseWriter) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="480" height="480" viewBox="0 0 480 480"><rect width="480" height="480" fill="#1a1a22"/><path d="M170 210a30 30 0 1 1 0 .1zM120 340l80-90 50 55 50-65 60 100z" fill="#2e2e3a"/></svg>`
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(svg))
}
