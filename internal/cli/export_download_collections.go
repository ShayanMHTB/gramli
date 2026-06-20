package cli

import (
	"bytes"
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
	cmd.AddCommand(&cobra.Command{Use: "sync", Short: "Sync saved collections from Instagram", RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		session := auth.Status(db.DB, "")
		if !session.Exists || !session.Authenticated {
			return fmt.Errorf("AUTH_SESSION_MISSING: run gramli auth status --check-remote first")
		}
		client := instagram.NewClient(session.CookieFilePath, filepath.Join(st.settings.DataDir, "cache", "collections"))
		cols, err := client.FetchCollections(cmd.Context())
		if err != nil {
			return err
		}
		if st.settings.DryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Would sync %d collection(s)\n", len(cols))
			return nil
		}
		now := time.Now().UTC()
		synced := 0
		for _, c := range cols {
			name := c.Name
			if name == "" {
				name = c.ID
			}
			if err := upsertCollection(db.DB, c.ID, name, slugify(name), now); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Sync failed for %q: %v\n", name, err)
				continue
			}
			synced++
		}
		if st.settings.JSON {
			return printJSON(cmd.OutOrStdout(), map[string]any{"synced": synced})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced %d collection(s)\n", synced)
		return nil
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
	cmd.AddCommand(&cobra.Command{Use: "create <name>", Short: "Create a local collection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		slug, err := posts.CreateCollection(cmd.Context(), db.DB, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created collection %q (slug: %s)\n", args[0], slug)
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "delete <collection>", Short: "Delete a local collection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		if !st.settings.Yes && !st.settings.DryRun {
			return fmt.Errorf("refusing to delete collection %q without --yes (use --dry-run to preview)", args[0])
		}
		if st.settings.DryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Would delete collection %q\n", args[0])
			return nil
		}
		if err := posts.DeleteCollection(cmd.Context(), db.DB, args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted collection %q\n", args[0])
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "add-post <collection> <shortcode-or-url>", Short: "Add a post to a local collection", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := posts.SetPostCollection(cmd.Context(), db.DB, args[1], args[0], true); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %q\n", args[1], args[0])
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "remove-post <collection> <shortcode-or-url>", Short: "Remove a post from a local collection", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := posts.SetPostCollection(cmd.Context(), db.DB, args[1], args[0], false); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %q\n", args[1], args[0])
		return nil
	}})
	return cmd
}

// slugify converts a collection name into a URL/CLI-friendly slug.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "collection"
	}
	return slug
}

// upsertCollection inserts or updates a synced Instagram collection, keying on
// the Instagram collection id and ensuring the local slug stays unique.
func upsertCollection(db *sql.DB, igID, name, slug string, now time.Time) error {
	if igID != "" {
		res, err := db.Exec(`UPDATE collections SET name=?, updated_at=? WHERE instagram_collection_id=?`, name, now, igID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	finalSlug := slug
	for i := 2; ; i++ {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM collections WHERE slug=?`, finalSlug).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			break
		}
		finalSlug = fmt.Sprintf("%s-%d", slug, i)
	}
	var igVal any
	if igID != "" {
		igVal = igID
	}
	_, err := db.Exec(`INSERT INTO collections(instagram_collection_id,name,slug,discovered_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`, igVal, name, finalSlug, now, now, now)
	return err
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
		c.Flags().String("output-dir", "", "Output directory (default: <data-dir>/downloads)")
		c.Flags().Bool("skip-existing", true, "Skip existing files")
		c.Flags().String("strategy", "auto", "Download strategy: auto, direct, yt-dlp")
		c.Flags().Bool("no-reconcile", false, "Skip the post-run reconcile that syncs download statuses from disk")
	}
	cmd.AddCommand(run)
	cmd.AddCommand(&cobra.Command{Use: "status", Short: "Show download status", RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "Downloaded: %d\nPending: %d\nFailed: %d\nSkipped: %d\nMissing: %d\nUnsupported: %d\n", countMedia(db.DB, "downloaded"), countMedia(db.DB, "pending"), countMedia(db.DB, "failed"), countMedia(db.DB, "skipped"), countMedia(db.DB, "missing"), countMedia(db.DB, "unsupported"))
		return nil
	}})
	cmd.AddCommand(downloadRetryCmd(st))
	cmd.AddCommand(downloadReconcileCmd(st))
	cmd.AddCommand(downloadCleanCmd(st))
	return cmd
}

// downloadRetryCmd re-queues failed and/or missing media back to 'pending' so
// the next `download run` will attempt them again. Reversible, so no --yes gate.
func downloadRetryCmd(st *appState) *cobra.Command {
	var failed, missing bool
	cmd := &cobra.Command{
		Use:   "retry",
		Short: "Re-queue failed/missing media for the next download run",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()

			var statuses []string
			if failed {
				statuses = append(statuses, "failed")
			}
			if missing {
				statuses = append(statuses, "missing")
			}
			if len(statuses) == 0 {
				return fmt.Errorf("nothing to retry: pass --failed and/or --missing")
			}
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
			argv := make([]any, len(statuses))
			for i, s := range statuses {
				argv[i] = s
			}
			var n int
			if err := db.QueryRow("SELECT COUNT(*) FROM media WHERE download_status IN ("+placeholders+")", argv...).Scan(&n); err != nil {
				return err
			}
			if st.settings.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would re-queue %d media item(s)\n", n)
				return nil
			}
			updateArgs := append([]any{time.Now().UTC()}, argv...)
			if _, err := db.Exec("UPDATE media SET download_status='pending', updated_at=? WHERE download_status IN ("+placeholders+")", updateArgs...); err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{"requeued": n})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Re-queued %d media item(s). Run: gramli download run\n", n)
			return nil
		},
	}
	cmd.Flags().BoolVar(&failed, "failed", true, "Re-queue media marked failed")
	cmd.Flags().BoolVar(&missing, "missing", false, "Re-queue media marked missing")
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
	if outputDir == "" {
		outputDir = filepath.Join(st.settings.DataDir, "downloads")
	}
	// yt-dlp downloads only log to the downloads table; media.download_status is
	// synced from disk by reconcile. Run it automatically at the end so the
	// status output and web UI reflect reality without a manual step.
	if noReconcile, _ := cmd.Flags().GetBool("no-reconcile"); !noReconcile {
		defer func() {
			report, rerr := reconcileDownloads(cmd, db.DB, outputDir, true)
			if rerr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Post-run reconcile failed: %v\n", rerr)
				return
			}
			if report.DBRowsUpdated > 0 || report.MissingMedia > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reconcile: synced %d media row(s) to downloaded, %d marked missing.\n", report.DBRowsUpdated, report.MissingMedia)
			}
		}()
	}
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
	var emptyDirs, cache, responseCache, archive, allDownloads, resetDB bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean transient download files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !emptyDirs && !cache && !responseCache && !archive && !allDownloads {
				return fmt.Errorf("select what to clean: --all, --empty-dirs, --cache, --response-cache and/or --archive")
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
			if responseCache {
				n, err := cleanResponseCache(st.settings.DataDir, st.settings.DryRun)
				if err != nil {
					return err
				}
				removed += n
			}
			if archive {
				n, err := cleanArchive(st.settings.DataDir, st.settings.DryRun)
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
	cmd.Flags().BoolVar(&cache, "cache", false, "Remove transient downloader cache files, preserving the yt-dlp archive")
	cmd.Flags().BoolVar(&responseCache, "response-cache", false, "Remove cached Instagram API/page responses")
	cmd.Flags().BoolVar(&archive, "archive", false, "Remove yt-dlp download archive")
	cmd.Flags().BoolVar(&resetDB, "reset-db", false, "Reset local download/media status rows when used with --all")
	cmd.Flags().Bool("orphans", false, "Reserved for DB-aware orphan cleanup")
	cmd.Flags().Bool("missing-db-records", false, "Reserved for DB-aware cleanup")
	return cmd
}

type downloadReconcileFolder struct {
	Owner     string   `json:"owner"`
	Shortcode string   `json:"shortcode"`
	Path      string   `json:"path"`
	Files     []string `json:"files"`
}

type downloadReconcileReport struct {
	OutputDir       string                    `json:"outputDir"`
	PostFolders     int                       `json:"postFolders"`
	MediaFiles      int                       `json:"mediaFiles"`
	MatchedPosts    int                       `json:"matchedPosts"`
	OrphanFolders   int                       `json:"orphanFolders"`
	DBRowsUpdated   int                       `json:"dbRowsUpdated"`
	DownloadRecords int                       `json:"downloadRecords"`
	MissingMedia    int                       `json:"missingMedia"`
	Applied         bool                      `json:"applied"`
	Orphans         []downloadReconcileFolder `json:"orphans,omitempty"`
}

func downloadReconcileCmd(st *appState) *cobra.Command {
	var outputDir string
	var apply bool
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Compare downloaded files with the local database",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			if outputDir == "" {
				outputDir = filepath.Join(st.settings.DataDir, "downloads")
			}
			report, err := reconcileDownloads(cmd, db.DB, outputDir, apply)
			if err != nil {
				return err
			}
			if st.settings.JSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			action := "Would update"
			if apply {
				action = "Updated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Download reconcile complete\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Output: %s\n", report.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Post folders: %d\n", report.PostFolders)
			fmt.Fprintf(cmd.OutOrStdout(), "Media files: %d\n", report.MediaFiles)
			fmt.Fprintf(cmd.OutOrStdout(), "Matched DB posts: %d\n", report.MatchedPosts)
			fmt.Fprintf(cmd.OutOrStdout(), "Orphan folders: %d\n", report.OrphanFolders)
			fmt.Fprintf(cmd.OutOrStdout(), "%s media rows: %d\n", action, report.DBRowsUpdated)
			if report.MissingMedia > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s missing media rows: %d\n", action, report.MissingMedia)
			}
			if report.DownloadRecords > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s download records: %d\n", action, report.DownloadRecords)
			}
			if !apply {
				fmt.Fprintln(cmd.OutOrStdout(), "Run again with --apply to update local download statuses.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Downloads directory to scan (default: <data-dir>/downloads)")
	cmd.Flags().BoolVar(&apply, "apply", false, "Update local DB rows from files found on disk")
	return cmd
}

func reconcileDownloads(cmd *cobra.Command, db *sql.DB, outputDir string, apply bool) (downloadReconcileReport, error) {
	folders, err := scanDownloadFolders(outputDir)
	if err != nil {
		return downloadReconcileReport{}, err
	}
	report := downloadReconcileReport{OutputDir: outputDir, PostFolders: len(folders), Applied: apply}
	for _, folder := range folders {
		report.MediaFiles += len(folder.Files)
		p, err := posts.Get(cmd.Context(), db, folder.Shortcode)
		if err != nil {
			if err == sql.ErrNoRows {
				report.OrphanFolders++
				report.Orphans = append(report.Orphans, folder)
				continue
			}
			return report, err
		}
		report.MatchedPosts++
		mediaRows, err := posts.ListMedia(cmd.Context(), db, p.ID)
		if err != nil {
			return report, err
		}
		if len(mediaRows) == 0 {
			if apply {
				if err := posts.RecordDownload(cmd.Context(), db, p.ID, 0, "downloaded", folder.Path, ""); err != nil {
					return report, err
				}
			}
			report.DownloadRecords++
			continue
		}
		used := map[string]bool{}
		for _, media := range mediaRows {
			if mediaAlreadyDownloaded(media) {
				continue
			}
			if placeholderMediaURL(media.RemoteURL) {
				if apply {
					if err := posts.MarkMediaStatus(cmd.Context(), db, media.ID, "missing"); err != nil {
						return report, err
					}
					if err := posts.RecordDownload(cmd.Context(), db, p.ID, media.ID, "missing", folder.Path, "Instagram metadata returned a placeholder media URL"); err != nil {
						return report, err
					}
				}
				report.MissingMedia++
				report.DownloadRecords++
				continue
			}
			if len(folder.Files) == 0 {
				continue
			}
			localPath := chooseReconciledFile(media, folder.Files, used)
			if localPath == "" {
				continue
			}
			if apply {
				info, err := os.Stat(localPath)
				if err != nil {
					return report, err
				}
				if err := posts.MarkMediaDownloaded(cmd.Context(), db, media.ID, localPath, info.Size()); err != nil {
					return report, err
				}
				if err := posts.RecordDownload(cmd.Context(), db, p.ID, media.ID, "downloaded", localPath, ""); err != nil {
					return report, err
				}
			}
			used[localPath] = true
			report.DBRowsUpdated++
			report.DownloadRecords++
		}
	}
	return report, nil
}

func scanDownloadFolders(root string) ([]downloadReconcileFolder, error) {
	owners, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var folders []downloadReconcileFolder
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}
		ownerPath := filepath.Join(root, owner.Name())
		postsDirs, err := os.ReadDir(ownerPath)
		if err != nil {
			return nil, err
		}
		for _, postDir := range postsDirs {
			if !postDir.IsDir() {
				continue
			}
			dir := filepath.Join(ownerPath, postDir.Name())
			files, err := completedMediaFiles(dir)
			if err != nil {
				return nil, err
			}
			folders = append(folders, downloadReconcileFolder{
				Owner:     owner.Name(),
				Shortcode: postDir.Name(),
				Path:      dir,
				Files:     files,
			})
		}
	}
	return folders, nil
}

func mediaAlreadyDownloaded(media posts.Media) bool {
	if media.Status != "downloaded" || media.LocalPath == "" {
		return false
	}
	info, err := os.Stat(media.LocalPath)
	return err == nil && info.Size() > 0
}

func placeholderMediaURL(remoteURL string) bool {
	return strings.Contains(remoteURL, "/rsrc.php/null.jpg") || strings.HasSuffix(remoteURL, "/null.jpg")
}

func completedMediaFiles(dir string) ([]string, error) {
	var videos, images []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".part") ||
			strings.HasSuffix(name, ".ytdl") ||
			strings.HasSuffix(name, ".tmp") ||
			strings.Contains(name, ".compat.tmp.") ||
			strings.HasSuffix(name, ".info.json") ||
			name == "post.json" ||
			name == "post.md" {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".mp4", ".mov", ".m4v", ".webm":
			videos = append(videos, path)
		case ".jpg", ".jpeg", ".png", ".webp", ".heic":
			images = append(images, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(videos) > 0 {
		return videos, nil
	}
	return images, nil
}

func chooseReconciledFile(media posts.Media, files []string, used map[string]bool) string {
	prefix := fmt.Sprintf("%02d_", media.MediaIndex)
	for _, file := range files {
		if used[file] {
			continue
		}
		if strings.HasPrefix(filepath.Base(file), prefix) {
			return file
		}
	}
	for _, file := range files {
		if used[file] {
			continue
		}
		ext := strings.ToLower(filepath.Ext(file))
		if media.MediaType == "video" && (ext == ".mp4" || ext == ".mov" || ext == ".m4v" || ext == ".webm") {
			return file
		}
		if media.MediaType == "image" && (ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".heic") {
			return file
		}
	}
	for _, file := range files {
		if !used[file] {
			return file
		}
	}
	return ""
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
	command := exec.CommandContext(ctx, ytDLP, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return ytdlpMetadata{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var info ytdlpMetadata
	if err := json.Unmarshal(out, &info); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return ytdlpMetadata{}, err
		}
		return ytdlpMetadata{}, fmt.Errorf("%w: %s", err, message)
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

func cleanResponseCache(dataDir string, dryRun bool) (int, error) {
	targets := []string{
		filepath.Join(dataDir, "cache", "saved"),
		filepath.Join(dataDir, "cache", "posts"),
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
			if err := os.RemoveAll(target); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

func cleanArchive(dataDir string, dryRun bool) (int, error) {
	target := filepath.Join(dataDir, "cache", "yt-dlp", "download-archive.txt")
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !dryRun {
		if err := os.Remove(target); err != nil {
			return 0, err
		}
	}
	return 1, nil
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
