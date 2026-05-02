package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/shayanmahtabi/gramli/internal/exporter"
	"github.com/shayanmahtabi/gramli/internal/instagram"
	"github.com/shayanmahtabi/gramli/internal/posts"
	"github.com/spf13/cobra"
)

func exportCmd(st *appState) *cobra.Command {
	var format, output, collection, owner string
	var stdout, pretty, overwrite bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export local metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := posts.List(cmd.Context(), db.DB, posts.ListOptions{Limit: 100000, Collection: collection, Owner: owner})
			if err != nil {
				return err
			}
			var f *os.File
			w := cmd.OutOrStdout()
			if !stdout {
				if output == "" {
					output = filepath.Join(st.settings.DataDir, "exports", "posts."+format)
				}
				if !overwrite {
					if _, err := os.Stat(output); err == nil {
						return fmt.Errorf("EXPORT_FAILED: output exists; rerun with --overwrite")
					}
				}
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					return err
				}
				f, err = os.Create(output)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			switch format {
			case "json":
				err = exporter.JSON(w, data, pretty)
			case "csv":
				err = exporter.CSV(w, data)
			case "markdown":
				for _, p := range data {
					_, err = fmt.Fprintf(w, "- [%s](%s) %s\n", p.Shortcode, p.PostURL, p.OwnerUsername)
					if err != nil {
						break
					}
				}
			default:
				err = fmt.Errorf("unsupported export format %q", format)
			}
			if err != nil {
				return fmt.Errorf("EXPORT_FAILED: %w", err)
			}
			if !stdout {
				fmt.Fprintf(cmd.OutOrStdout(), "Exported %d posts to %s\n", len(data), output)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "json, csv, markdown, sqlite")
	cmd.Flags().StringVar(&output, "output", "", "Output file path")
	cmd.Flags().StringVar(&collection, "collection", "", "Filter by collection")
	cmd.Flags().StringVar(&owner, "owner", "", "Filter by owner")
	cmd.Flags().Bool("downloaded", false, "Export only downloaded posts")
	cmd.Flags().Bool("include-media", false, "Include media rows")
	cmd.Flags().Bool("include-raw", false, "Include raw JSON")
	cmd.Flags().BoolVar(&pretty, "pretty", true, "Pretty-print JSON")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "Print to stdout")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite output file")
	return cmd
}

func collectionsCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "collections", Short: "Manage collections"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List collections", RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.Query(`SELECT c.name, c.slug, COUNT(pc.post_id) FROM collections c LEFT JOIN post_collections pc ON pc.collection_id = c.id GROUP BY c.id ORDER BY c.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		fmt.Fprintln(cmd.OutOrStdout(), "Name        Slug        Posts")
		for rows.Next() {
			var name, slug string
			var count int
			if err := rows.Scan(&name, &slug, &count); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-11s %-11s %d\n", name, slug, count)
		}
		return rows.Err()
	}})
	cmd.AddCommand(&cobra.Command{Use: "sync", Short: "Sync collections", RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("collections sync is not implemented yet")
	}})
	cmd.AddCommand(&cobra.Command{Use: "show <collection>", Short: "Show collection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runPostsList(cmd, st, posts.ListOptions{Collection: args[0]})
	}})
	cmd.AddCommand(&cobra.Command{Use: "rename-local <old> <new>", Short: "Rename local collection", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.Exec(`UPDATE collections SET name = ?, slug = ?, updated_at = datetime('now') WHERE slug = ? OR name = ?`, args[1], args[1], args[0], args[0])
		return err
	}})
	return cmd
}

func downloadCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Plan and run media downloads",
		RunE: func(cmd *cobra.Command, args []string) error {
			return downloadPlan(cmd, st)
		},
	}
	run := &cobra.Command{Use: "run", Short: "Run downloads", RunE: func(cmd *cobra.Command, args []string) error { return downloadPlan(cmd, st) }}
	for _, c := range []*cobra.Command{cmd, run} {
		c.Flags().String("collection", "", "Download posts from collection")
		c.Flags().String("post", "", "Download one post")
		c.Flags().String("owner", "", "Download posts from owner")
		c.Flags().Int("limit", 0, "Maximum posts to download")
		c.Flags().Bool("all", false, "Download all matching posts")
		c.Flags().Duration("delay", 5*time.Second, "Delay between batch downloads")
		c.Flags().Bool("metadata", false, "Write metadata sidecar files")
		c.Flags().Bool("metadata-only", false, "Do not download media")
		c.Flags().String("output-dir", filepath.Join(st.settings.DataDir, "downloads"), "Output directory")
		c.Flags().Bool("skip-existing", true, "Skip existing files")
		c.Flags().String("strategy", "auto", "Download strategy: auto, direct, yt-dlp")
	}
	cmd.AddCommand(run)
	cmd.AddCommand(&cobra.Command{Use: "status", Short: "Show download status", RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "Downloaded: %d\nPending: %d\nFailed: %d\nSkipped: %d\n", countMedia(db.DB, "downloaded"), countMedia(db.DB, "pending"), countMedia(db.DB, "failed"), countMedia(db.DB, "skipped"))
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "retry", Short: "Retry failed downloads", RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("download retry is not implemented yet")
	}})
	cmd.AddCommand(downloadCleanCmd(st))
	return cmd
}

func downloadPlan(cmd *cobra.Command, st *appState) error {
	db, err := openMigratedDB(st)
	if err != nil {
		return err
	}
	defer db.Close()
	post, _ := cmd.Flags().GetString("post")
	collection, _ := cmd.Flags().GetString("collection")
	owner, _ := cmd.Flags().GetString("owner")
	limit, _ := cmd.Flags().GetInt("limit")
	all, _ := cmd.Flags().GetBool("all")
	delay, _ := cmd.Flags().GetDuration("delay")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	writeMetadata, _ := cmd.Flags().GetBool("metadata")
	metadataOnly, _ := cmd.Flags().GetBool("metadata-only")
	skipExisting, _ := cmd.Flags().GetBool("skip-existing")
	strategy, _ := cmd.Flags().GetString("strategy")
	if all && limit > 0 {
		return fmt.Errorf("--all and --limit cannot be used together")
	}
	opt := posts.ListOptions{Collection: collection, Owner: owner, Limit: limit, All: all}
	if post != "" {
		p, err := posts.Get(cmd.Context(), db.DB, post)
		if err != nil {
			return err
		}
		if strategy == "yt-dlp" || strategy == "auto" {
			if used, err := downloadWithYTDLP(cmd, db.DB, st, p, outputDir, writeMetadata, metadataOnly); used || strategy == "yt-dlp" {
				return err
			}
		}
		media, err := posts.ListMedia(cmd.Context(), db.DB, p.ID)
		if err != nil {
			return err
		}
		if len(media) == 0 {
			session := auth.Status(db.DB, "")
			if !session.Exists || !session.Authenticated {
				return fmt.Errorf("AUTH_SESSION_MISSING: download needs media URLs; run gramli auth status --check-remote, then gramli posts import <file> --fetch-metadata")
			}
			ig := instagram.NewClient(session.CookieFilePath, filepath.Join(st.settings.DataDir, "cache", "posts"))
			meta, err := ig.FetchPost(cmd.Context(), p.PostURL)
			if err != nil {
				return err
			}
			if err := applyInstagramMetadata(cmd.Context(), db.DB, meta); err != nil {
				return err
			}
			p, _ = posts.Get(cmd.Context(), db.DB, p.Shortcode)
			media, err = posts.ListMedia(cmd.Context(), db.DB, p.ID)
			if err != nil {
				return err
			}
		}
		if len(media) == 0 {
			return fmt.Errorf("DOWNLOAD_FAILED: no media URLs were found for %s", p.Shortcode)
		}
		downloaded, skipped, err := downloadPostMedia(cmd, db.DB, p, media, outputDir, writeMetadata, metadataOnly, skipExisting)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Download complete\nPost: %s\nDownloaded: %d\nSkipped: %d\nOutput: %s\n", p.Shortcode, downloaded, skipped, outputDir)
		return nil
	}
	data, err := posts.List(cmd.Context(), db.DB, opt)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Download planner found 0 posts")
		return nil
	}
	if strategy == "yt-dlp" || strategy == "auto" {
		processed, failed, skipped := 0, 0, 0
		failures := make([]string, 0)
		for i, p := range data {
			if i > 0 && delay > 0 && !metadataOnly {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(delay):
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Downloading %d/%d %s...\n", i+1, len(data), p.Shortcode)
			if p.MediaType == "image" || p.MediaType == "album" {
				media, err := posts.ListMedia(cmd.Context(), db.DB, p.ID)
				if err == nil && len(media) > 0 {
					d, s, err := downloadPostMedia(cmd, db.DB, p, media, outputDir, writeMetadata, metadataOnly, skipExisting)
					if err != nil {
						failed++
						failures = append(failures, p.Shortcode+": "+err.Error())
						fmt.Fprintf(cmd.ErrOrStderr(), "Download failed for %s: %v\n", p.Shortcode, err)
						continue
					}
					if d > 0 || metadataOnly {
						processed++
					}
					skipped += s
					continue
				}
				failed++
				msg := "no direct media rows found; rerun gramli posts sync --saved to backfill media URLs"
				failures = append(failures, p.Shortcode+": "+msg)
				fmt.Fprintf(cmd.ErrOrStderr(), "Download failed for %s: %s\n", p.Shortcode, msg)
				continue
			}
			used, err := downloadWithYTDLP(cmd, db.DB, st, p, outputDir, writeMetadata, metadataOnly)
			if err != nil {
				media, mediaErr := posts.ListMedia(cmd.Context(), db.DB, p.ID)
				if mediaErr == nil && len(media) > 0 && strings.Contains(err.Error(), "No video formats found") {
					d, s, directErr := downloadPostMedia(cmd, db.DB, p, media, outputDir, writeMetadata, metadataOnly, skipExisting)
					if directErr == nil {
						if d > 0 || metadataOnly {
							processed++
						}
						skipped += s
						continue
					}
					err = directErr
				}
				failed++
				failures = append(failures, p.Shortcode+": "+err.Error())
				fmt.Fprintf(cmd.ErrOrStderr(), "Download failed for %s: %v\n", p.Shortcode, err)
				continue
			}
			if used {
				processed++
				continue
			}
			if strategy == "yt-dlp" {
				failed++
				failures = append(failures, p.Shortcode+": yt-dlp is not installed")
				fmt.Fprintln(cmd.ErrOrStderr(), "Download failed: yt-dlp is not installed")
			} else {
				skipped++
			}
		}
		label := "Downloaded"
		if metadataOnly {
			label = "Processed"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Batch download complete\nPosts: %d\n%s: %d\nSkipped: %d\nFailed: %d\nOutput: %s\n", len(data), label, processed, skipped, failed, outputDir)
		if len(failures) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Failures:")
			for _, failure := range failures {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", failure)
			}
		}
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Download planner found %d posts\nDirect collection downloads require stored media URLs; use --strategy yt-dlp for saved-post URLs.\n", len(data))
	return nil
}

func downloadWithYTDLP(cmd *cobra.Command, db *sql.DB, st *appState, p posts.Post, outputDir string, writeMetadata, metadataOnly bool) (bool, error) {
	ytDLP, err := exec.LookPath("yt-dlp")
	if err != nil {
		if _, ffErr := exec.LookPath("ffmpeg"); ffErr != nil {
			_ = ffErr
		}
		return false, nil
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return true, fmt.Errorf("DOWNLOAD_FAILED: yt-dlp is installed but ffmpeg is missing; install both with: brew install yt-dlp ffmpeg")
	}
	session := auth.Status(db, "")
	if !session.Exists || !session.Authenticated {
		return true, fmt.Errorf("AUTH_SESSION_MISSING: yt-dlp download requires an authenticated session")
	}
	cookies, err := auth.LoadCookies(session.CookieFilePath)
	if err != nil {
		return true, err
	}
	cookiePath := filepath.Join(st.settings.DataDir, "cache", "yt-dlp", "cookies.txt")
	if err := auth.WriteNetscapeCookieFile(cookies, cookiePath); err != nil {
		return true, err
	}
	defer func() {
		_ = os.Remove(cookiePath)
	}()
	info, err := ytdlpInfo(cmd.Context(), ytDLP, cookiePath, p.PostURL)
	if err != nil {
		return true, fmt.Errorf("DOWNLOAD_FAILED: yt-dlp could not inspect post metadata: %w", err)
	}
	owner := sanitizePath(firstNonEmpty(p.OwnerUsername, info.Channel, info.Creator, info.Uploader, info.UploaderID, "unknown-owner"))
	postID := sanitizePath(firstNonEmpty(info.ID, p.Shortcode))
	dir := filepath.Join(outputDir, owner, postID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return true, err
	}
	if writeMetadata {
		sidecar := filepath.Join(dir, "post.json")
		b, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return true, err
		}
		if err := os.WriteFile(sidecar, b, 0o644); err != nil {
			return true, err
		}
	}
	if metadataOnly {
		fmt.Fprintf(cmd.OutOrStdout(), "Metadata sidecar written\nPost: %s\nOutput: %s\n", p.Shortcode, dir)
		return true, nil
	}
	outputTemplate := filepath.Join(dir, "%(title).200B-%(id)s.%(ext)s")
	archivePath := filepath.Join(st.settings.DataDir, "cache", "yt-dlp", "download-archive.txt")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return true, err
	}
	args := []string{
		"--cookies", cookiePath,
		"--no-playlist",
		"--download-archive", archivePath,
		"--format", "bv*[vcodec^=avc1]+ba[acodec^=mp4a]/bv*+ba/b",
		"--merge-output-format", "mp4",
		"--write-info-json",
		"--write-thumbnail",
		"-o", outputTemplate,
		p.PostURL,
	}
	command := exec.CommandContext(cmd.Context(), ytDLP, args...)
	command.Stdout = cmd.OutOrStdout()
	command.Stderr = cmd.ErrOrStderr()
	if err := command.Run(); err != nil {
		_ = posts.RecordDownload(cmd.Context(), db, p.ID, 0, "failed", dir, err.Error())
		return true, fmt.Errorf("DOWNLOAD_FAILED: yt-dlp failed: %w", err)
	}
	converted, err := makeCompatibleMP4(cmd.Context(), dir)
	if err != nil {
		return true, err
	}
	_ = cleanupTransientDownloadFiles(dir)
	_ = posts.RecordDownload(cmd.Context(), db, p.ID, 0, "downloaded", dir, "")
	if converted {
		fmt.Fprintln(cmd.OutOrStdout(), "Compatibility pass: wrote H.264/AAC MP4")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Download complete\nPost: %s\nOwner: %s\nStrategy: yt-dlp\nOutput: %s\n", p.Shortcode, owner, dir)
	return true, nil
}

func downloadCleanCmd(st *appState) *cobra.Command {
	var emptyDirs, cache, allDownloads, resetDB bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean transient download files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !emptyDirs && !cache && !allDownloads {
				return fmt.Errorf("select what to clean: --all, --empty-dirs and/or --cache")
			}
			if !st.settings.Yes && !st.settings.DryRun {
				return fmt.Errorf("download clean removes local files; rerun with --dry-run or --yes")
			}
			removed := 0
			if allDownloads {
				n, err := cleanAllDownloads(filepath.Join(st.settings.DataDir, "downloads"), st.settings.DryRun)
				if err != nil {
					return err
				}
				removed += n
				if resetDB && !st.settings.DryRun {
					db, err := openMigratedDB(st)
					if err != nil {
						return err
					}
					defer db.Close()
					if _, err := db.Exec(`DELETE FROM downloads; UPDATE media SET local_path = NULL, file_size_bytes = NULL, download_status = 'pending', updated_at = datetime('now')`); err != nil {
						return err
					}
				}
			}
			if cache {
				n, err := cleanDownloadCache(st.settings.DataDir, st.settings.DryRun)
				if err != nil {
					return err
				}
				removed += n
			}
			if emptyDirs {
				n, err := cleanEmptyDirs(filepath.Join(st.settings.DataDir, "downloads"), st.settings.DryRun)
				if err != nil {
					return err
				}
				removed += n
			}
			action := "Removed"
			if st.settings.DryRun {
				action = "Would remove"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d paths\n", action, removed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allDownloads, "all", false, "Remove all files under the downloads directory")
	cmd.Flags().BoolVar(&emptyDirs, "empty-dirs", false, "Remove empty directories under downloads")
	cmd.Flags().BoolVar(&cache, "cache", false, "Remove transient downloader cache files")
	cmd.Flags().BoolVar(&resetDB, "reset-db", false, "Reset local download/media status rows when used with --all")
	cmd.Flags().Bool("orphans", false, "Reserved for DB-aware orphan cleanup")
	cmd.Flags().Bool("missing-db-records", false, "Reserved for DB-aware cleanup")
	return cmd
}

type ytdlpMetadata struct {
	ID         string `json:"id"`
	Uploader   string `json:"uploader"`
	UploaderID string `json:"uploader_id"`
	Channel    string `json:"channel"`
	Creator    string `json:"creator"`
}

func ytdlpInfo(ctx context.Context, ytDLP, cookiePath, postURL string) (ytdlpMetadata, error) {
	args := []string{"--cookies", cookiePath, "--no-playlist", "--dump-single-json", "--skip-download", postURL}
	out, err := exec.CommandContext(ctx, ytDLP, args...).CombinedOutput()
	if err != nil {
		return ytdlpMetadata{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var info ytdlpMetadata
	if err := json.Unmarshal(out, &info); err != nil {
		return ytdlpMetadata{}, err
	}
	if info.Uploader == "" {
		info.Uploader = firstNonEmpty(info.Channel, info.Creator)
	}
	return info, nil
}

func makeCompatibleMP4(ctx context.Context, dir string) (bool, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return false, fmt.Errorf("DOWNLOAD_FAILED: ffmpeg is required for compatibility conversion: %w", err)
	}
	mp4, err := newestFileWithExt(dir, ".mp4")
	if err != nil || mp4 == "" {
		return false, nil
	}
	tmp := strings.TrimSuffix(mp4, ".mp4") + ".compat.tmp.mp4"
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", mp4,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "libx264",
		"-profile:v", "high",
		"-level:v", "4.0",
		"-tag:v", "avc1",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-profile:a", "aac_low",
		"-b:a", "192k",
		"-ar", "48000",
		"-ac", "2",
		"-disposition:a:0", "default",
		"-brand", "mp42",
		"-movflags", "+faststart",
		tmp,
	}
	if err := exec.CommandContext(ctx, ffmpeg, args...).Run(); err != nil {
		return false, fmt.Errorf("DOWNLOAD_FAILED: ffmpeg compatibility conversion failed: %w", err)
	}
	if err := os.Rename(tmp, mp4); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func cleanupTransientDownloadFiles(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".part") ||
			strings.HasSuffix(name, ".ytdl") ||
			strings.Contains(name, ".compat.tmp.") ||
			strings.HasSuffix(name, ".tmp") {
			return os.Remove(path)
		}
		return nil
	})
}

func cleanDownloadCache(dataDir string, dryRun bool) (int, error) {
	targets := []string{
		filepath.Join(dataDir, "cache", "yt-dlp", "cookies.txt"),
		filepath.Join(dataDir, "cache", "yt-dlp", "download-archive.txt"),
		filepath.Join(dataDir, "cache", "audio-check-DQLq3MEkgg4.wav"),
		filepath.Join(dataDir, "cache", "DQLq3MEkgg4-audio-check.m4a"),
	}
	removed := 0
	for _, target := range targets {
		if _, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed++
		if !dryRun {
			if err := os.Remove(target); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

func cleanAllDownloads(root string, dryRun bool) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		target := filepath.Join(root, entry.Name())
		removed++
		if !dryRun {
			if err := os.RemoveAll(target); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

func cleanEmptyDirs(root string, dryRun bool) (int, error) {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	removed := 0
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		if len(entries) != 0 {
			continue
		}
		removed++
		if !dryRun {
			if err := os.Remove(dirs[i]); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

func newestFileWithExt(dir, ext string) (string, error) {
	var newest string
	var newestTime time.Time
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(path)) != ext {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = path
			newestTime = info.ModTime()
		}
		return nil
	})
	return newest, err
}

func downloadPostMedia(cmd *cobra.Command, db *sql.DB, p posts.Post, media []posts.Media, outputDir string, writeMetadata, metadataOnly, skipExisting bool) (int, int, error) {
	dir := filepath.Join(outputDir, sanitizePath(firstNonEmpty(p.OwnerUsername, "unknown-owner")), sanitizePath(p.Shortcode))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, 0, err
	}
	if writeMetadata {
		sidecar := filepath.Join(dir, "post.json")
		b, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return 0, 0, err
		}
		if err := os.WriteFile(sidecar, b, 0o644); err != nil {
			return 0, 0, err
		}
	}
	if metadataOnly {
		return 0, len(media), nil
	}

	session := auth.Status(db, "")
	cookieHeader := ""
	if session.Exists {
		if cookies, err := auth.LoadCookies(session.CookieFilePath); err == nil {
			cookieHeader = auth.CookieHeader(cookies)
		}
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	downloaded, skipped := 0, 0
	for _, m := range media {
		ext := extFromURL(m.RemoteURL, m.MediaType)
		dst := filepath.Join(dir, fmt.Sprintf("%02d_%s%s", m.MediaIndex, sanitizePath(firstNonEmpty(m.MediaType, "media")), ext))
		if skipExisting {
			if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
				_ = posts.MarkMediaDownloaded(cmd.Context(), db, m.ID, dst, info.Size())
				skipped++
				continue
			}
		}
		size, err := downloadFile(cmd.Context(), client, m.RemoteURL, dst, cookieHeader)
		if err != nil {
			_ = posts.RecordDownload(cmd.Context(), db, p.ID, m.ID, "failed", dst, err.Error())
			return downloaded, skipped, fmt.Errorf("DOWNLOAD_FAILED: %w", err)
		}
		if err := posts.MarkMediaDownloaded(cmd.Context(), db, m.ID, dst, size); err != nil {
			return downloaded, skipped, err
		}
		if err := posts.RecordDownload(cmd.Context(), db, p.ID, m.ID, "downloaded", dst, ""); err != nil {
			return downloaded, skipped, err
		}
		downloaded++
	}
	return downloaded, skipped, nil
}

func downloadFile(ctx context.Context, client *http.Client, remoteURL, dst, cookieHeader string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("media request returned %s", resp.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	size, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return size, nil
}

func extFromURL(remoteURL, mediaType string) string {
	uPath := remoteURL
	if idx := strings.IndexAny(uPath, "?#"); idx >= 0 {
		uPath = uPath[:idx]
	}
	ext := path.Ext(uPath)
	if ext != "" && len(ext) <= 6 {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		return exts[0]
	}
	switch mediaType {
	case "video":
		return ".mp4"
	default:
		return ".jpg"
	}
}

func sanitizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "_")
	return replacer.Replace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func countMedia(db *sql.DB, status string) int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM media WHERE download_status = ?`, status).Scan(&n)
	return n
}
