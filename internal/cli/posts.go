package cli

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/shayanmahtabi/gramli/internal/instagram"
	"github.com/shayanmahtabi/gramli/internal/posts"
	"github.com/spf13/cobra"
)

func postsCmd(st *appState) *cobra.Command {
	var listAlias bool
	cmd := &cobra.Command{
		Use:   "posts",
		Short: "Manage saved post metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			if listAlias {
				return runPostsList(cmd, st, posts.ListOptions{})
			}
			return cmd.Help()
		},
	}
	cmd.Flags().BoolVar(&listAlias, "list", false, "List posts")
	cmd.AddCommand(postsListCmd(st), postsImportCmd(st), postsShowCmd(st), postsSearchCmd(st), postsMediaCmd(st))
	cmd.AddCommand(postsSyncCmd(st))
	cmd.AddCommand(postsCleanCmd(st))
	return cmd
}

// postsCleanCmd removes orphaned post records (no media rows), optionally scoped
// by source. Destructive, so it requires --yes unless --dry-run is set.
func postsCleanCmd(st *appState) *cobra.Command {
	var source string
	var orphans bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove orphaned post records (no media)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()

			var where []string
			var argv []any
			if orphans {
				where = append(where, "NOT EXISTS (SELECT 1 FROM media m WHERE m.post_id = posts.id)")
			}
			if source != "" {
				where = append(where, "source = ?")
				argv = append(argv, source)
			}
			if len(where) == 0 {
				return fmt.Errorf("nothing to clean: pass --orphans and/or --source")
			}
			cond := strings.Join(where, " AND ")

			var n int
			if err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE "+cond, argv...).Scan(&n); err != nil {
				return err
			}
			if st.settings.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would remove %d post(s)\n", n)
				return nil
			}
			if n > 0 && !st.settings.Yes {
				return fmt.Errorf("refusing to delete %d post(s) without --yes (use --dry-run to preview)", n)
			}
			res, err := db.Exec("DELETE FROM posts WHERE "+cond, argv...)
			if err != nil {
				return err
			}
			removed, _ := res.RowsAffected()
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{"removed": removed})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %d post(s)\n", removed)
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "Only clean posts from this source (saved|manual-import)")
	cmd.Flags().BoolVar(&orphans, "orphans", true, "Clean posts that have no media rows")
	return cmd
}

func postsSyncCmd(st *appState) *cobra.Command {
	var saved bool
	var limit, maxPages int
	var collection string
	var delay time.Duration
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync saved posts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !saved {
				return fmt.Errorf("posts sync currently requires --saved")
			}
			if collection == "" {
				collection = "saved"
			}
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			session := auth.Status(db.DB, "")
			if !session.Exists || !session.Authenticated {
				return fmt.Errorf("AUTH_SESSION_MISSING: run gramli auth status --check-remote first")
			}
			client := instagram.NewClient(session.CookieFilePath, filepath.Join(st.settings.DataDir, "cache", "saved"))
			fetched, stored, failed := 0, 0, 0
			nextMaxID := ""
			for pageNo := 1; ; pageNo++ {
				if maxPages > 0 && pageNo > maxPages {
					break
				}
				if delay > 0 && pageNo > 1 {
					select {
					case <-cmd.Context().Done():
						return cmd.Context().Err()
					case <-time.After(delay):
					}
				}
				page, err := client.FetchSavedPosts(cmd.Context(), nextMaxID)
				if err != nil {
					return err
				}
				for _, savedPost := range page.Posts {
					if limit > 0 && fetched >= limit {
						break
					}
					fetched++
					if st.settings.DryRun {
						fmt.Fprintf(cmd.OutOrStdout(), "Would store: %s %s\n", savedPost.Shortcode, savedPost.PostURL)
						continue
					}
					err := posts.UpsertSaved(cmd.Context(), db.DB, posts.MetadataUpdate{
						Shortcode:     savedPost.Shortcode,
						OwnerUsername: savedPost.OwnerUsername,
						Caption:       savedPost.Caption,
						MediaType:     savedPost.MediaType,
						IsVideo:       savedPost.MediaType == "video",
						IsAlbum:       savedPost.MediaType == "album",
						ThumbnailURL:  savedPost.ThumbnailURL,
						Media:         convertInstagramMedia(savedPost.Media),
					}, savedPost.PostURL)
					if err != nil {
						failed++
						fmt.Fprintf(cmd.ErrOrStderr(), "Store failed for %s: %v\n", savedPost.Shortcode, err)
						continue
					}
					if err := attachCollection(db.DB, savedPost.Shortcode, collection); err != nil {
						return err
					}
					stored++
				}
				if limit > 0 && fetched >= limit {
					break
				}
				if !page.HasNextPage || page.NextMaxID == "" {
					break
				}
				nextMaxID = page.NextMaxID
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Sync complete\nFetched: %d\nStored: %d\nFailed: %d\nCollection: %s\nDatabase: %s\n", fetched, stored, failed, collection, st.settings.DBPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&saved, "saved", false, "Sync saved posts")
	cmd.Flags().Bool("collections", false, "Reserved for collection sync")
	cmd.Flags().StringVar(&collection, "collection", "saved", "Local collection name for synced saved posts")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum posts to sync")
	cmd.Flags().IntVar(&maxPages, "max-pages", 0, "Maximum API pages to fetch")
	cmd.Flags().Bool("include-media", false, "Reserved; downloads use yt-dlp")
	cmd.Flags().Bool("metadata-only", true, "Store metadata only")
	cmd.Flags().Bool("force", false, "Re-fetch existing posts")
	cmd.Flags().Bool("resume", false, "Resume previous sync")
	cmd.Flags().String("rate-limit", "", "Example: 20rpm")
	cmd.Flags().DurationVar(&delay, "delay", 2*time.Second, "Delay between API pages")
	cmd.Flags().Duration("jitter", 0, "Random delay jitter")
	return cmd
}

func convertInstagramMedia(media []instagram.Media) []posts.Media {
	out := make([]posts.Media, 0, len(media))
	for _, m := range media {
		out = append(out, posts.Media{
			MediaIndex:   m.Index,
			MediaType:    m.Type,
			RemoteURL:    m.URL,
			ThumbnailURL: m.ThumbnailURL,
		})
	}
	return out
}

func postsListCmd(st *appState) *cobra.Command {
	var opt posts.ListOptions
	var format string
	var saved bool
	cmd := &cobra.Command{Use: "list", Short: "List posts", RunE: func(cmd *cobra.Command, args []string) error {
		_ = saved
		if d, _ := cmd.Flags().GetBool("downloaded"); d {
			tv := true
			opt.Downloaded = &tv
		}
		if nd, _ := cmd.Flags().GetBool("not-downloaded"); nd {
			fv := false
			opt.Downloaded = &fv
		}
		return runPostsList(cmd, st, optWithFormat(opt, format))
	}}
	cmd.Flags().IntVar(&opt.Limit, "limit", 50, "Maximum posts to show")
	cmd.Flags().IntVar(&opt.Offset, "offset", 0, "Offset for pagination")
	cmd.Flags().StringVar(&opt.Collection, "collection", "", "Filter by collection")
	cmd.Flags().StringVar(&opt.Owner, "owner", "", "Filter by owner username")
	cmd.Flags().StringVar(&opt.MediaType, "media-type", "any", "image, video, album, any")
	cmd.Flags().BoolVar(&saved, "saved", false, "Only saved posts")
	cmd.Flags().Bool("downloaded", false, "Only downloaded posts")
	cmd.Flags().Bool("not-downloaded", false, "Only posts not downloaded")
	cmd.Flags().StringVar(&opt.Sort, "sort", "discovered_at", "Sort field")
	cmd.Flags().StringVar(&opt.Order, "order", "desc", "asc or desc")
	cmd.Flags().StringVar(&format, "format", "table", "table, json, csv, compact")
	cmd.Flags().Bool("open", false, "Open selected result in browser")
	return cmd
}

func optWithFormat(opt posts.ListOptions, format string) posts.ListOptions {
	opt.Format = format
	return opt
}

func runPostsList(cmd *cobra.Command, st *appState, opt posts.ListOptions) error {
	db, err := openMigratedDB(st)
	if err != nil {
		return err
	}
	defer db.Close()
	data, err := posts.List(cmd.Context(), db.DB, opt)
	if err != nil {
		return err
	}
	if st.settings.JSON || opt.Format == "json" {
		return printJSON(cmd.OutOrStdout(), data)
	}
	if opt.Format == "csv" {
		fmt.Fprintln(cmd.OutOrStdout(), "shortcode,owner,type,downloaded,url")
		for _, p := range data {
			fmt.Fprintf(cmd.OutOrStdout(), "%s,%s,%s,%t,%s\n", p.Shortcode, p.OwnerUsername, p.MediaType, p.Downloaded, p.PostURL)
		}
		return nil
	}
	if opt.Format == "compact" {
		for _, p := range data {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", p.Shortcode, p.PostURL)
		}
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Shortcode       Owner           Type       Downloaded  URL")
	for _, p := range data {
		fmt.Fprintf(cmd.OutOrStdout(), "%-15s %-15s %-10s %-11t %s\n", p.Shortcode, p.OwnerUsername, p.MediaType, p.Downloaded, p.PostURL)
	}
	return nil
}

func postsImportCmd(st *appState) *cobra.Command {
	var stdin, dedupe bool
	var collection string
	var fetchMetadata bool
	cmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import Instagram post URLs",
		Args: func(cmd *cobra.Command, args []string) error {
			if stdin {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			var r io.Reader
			if stdin {
				r = cmd.InOrStdin()
			} else {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			imported, enriched, failed, err := importURLs(cmd, db.DB, r, collection, fetchMetadata, st)
			if err != nil {
				return err
			}
			_ = dedupe
			fmt.Fprintf(cmd.OutOrStdout(), "Import complete\nImported: %d\nMetadata fetched: %d\nFailed: %d\nDatabase: %s\n", imported, enriched, failed, st.settings.DBPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read URLs from stdin")
	cmd.Flags().StringVar(&collection, "collection", "", "Assign imported posts to collection")
	cmd.Flags().StringArray("tag", nil, "Tags")
	cmd.Flags().BoolVar(&fetchMetadata, "fetch-metadata", false, "Fetch metadata")
	cmd.Flags().Bool("metadata-only", true, "Store metadata only")
	cmd.Flags().BoolVar(&dedupe, "dedupe", true, "Dedupe URLs")
	return cmd
}

func importURLs(cmd *cobra.Command, db *sql.DB, r io.Reader, collection string, fetchMetadata bool, st *appState) (int, int, int, error) {
	scanner := bufio.NewScanner(r)
	imported, enriched, failed := 0, 0, 0
	var ig *instagram.Client
	if fetchMetadata {
		session := auth.Status(db, "")
		if !session.Exists || !session.Authenticated {
			return 0, 0, 0, fmt.Errorf("AUTH_SESSION_MISSING: metadata fetch requires an authenticated session; run gramli login --cookie-file first")
		}
		ig = instagram.NewClient(session.CookieFilePath, filepath.Join(st.settings.DataDir, "cache", "posts"))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		shortcode, postURL, err := posts.ParseInstagramURL(line)
		if err != nil {
			failed++
			continue
		}
		if _, err := posts.Upsert(cmd.Context(), db, shortcode, postURL, "manual-import"); err != nil {
			return imported, enriched, failed, err
		}
		if collection != "" {
			if err := attachCollection(db, shortcode, collection); err != nil {
				return imported, enriched, failed, err
			}
		}
		if ig != nil {
			meta, err := ig.FetchPost(cmd.Context(), postURL)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Metadata fetch failed for %s: %v\n", shortcode, err)
				failed++
			} else if err := applyInstagramMetadata(cmd.Context(), db, meta); err != nil {
				return imported, enriched, failed, err
			} else {
				enriched++
			}
		}
		imported++
	}
	return imported, enriched, failed, scanner.Err()
}

func applyInstagramMetadata(ctx context.Context, db *sql.DB, meta instagram.Metadata) error {
	media := make([]posts.Media, 0, len(meta.Media))
	for _, m := range meta.Media {
		media = append(media, posts.Media{
			MediaIndex:   m.Index,
			MediaType:    m.Type,
			RemoteURL:    m.URL,
			ThumbnailURL: m.ThumbnailURL,
		})
	}
	return posts.ApplyMetadata(ctx, db, posts.MetadataUpdate{
		Shortcode:     meta.Shortcode,
		OwnerUsername: meta.OwnerUsername,
		Caption:       meta.Caption,
		MediaType:     meta.MediaType,
		IsVideo:       meta.MediaType == "video",
		IsAlbum:       len(meta.Media) > 1,
		ThumbnailURL:  meta.ThumbnailURL,
		RawPath:       meta.RawPath,
		Media:         media,
	})
}

func attachCollection(db *sql.DB, shortcode, name string) error {
	slug := name
	now := "datetime('now')"
	_, err := db.Exec(fmt.Sprintf(`INSERT INTO collections(name, slug, discovered_at, created_at, updated_at) VALUES(?, ?, %s, %s, %s) ON CONFLICT(slug) DO UPDATE SET name=excluded.name, updated_at=datetime('now')`, now, now, now), name, slug)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO post_collections(post_id, collection_id, added_at)
SELECT p.id, c.id, datetime('now') FROM posts p, collections c WHERE p.shortcode = ? AND c.slug = ?`, shortcode, slug)
	return err
}

func postsShowCmd(st *appState) *cobra.Command {
	return &cobra.Command{Use: "show <shortcode-or-url>", Short: "Show one post", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		p, err := posts.Get(cmd.Context(), db.DB, args[0])
		if err != nil {
			return fmt.Errorf("POST_NOT_FOUND: %w", err)
		}
		if st.settings.JSON {
			return printJSON(cmd.OutOrStdout(), p)
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
		return nil
	}}
}

func postsSearchCmd(st *appState) *cobra.Command {
	var limit int
	return &cobra.Command{Use: "search <query>", Short: "Search posts", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runPostsList(cmd, st, posts.ListOptions{Query: args[0], Limit: limit})
	}}
}

func postsMediaCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "media", Short: "Manage media rows for posts"}
	var mediaURL, mediaType, thumbnailURL string
	var index int
	add := &cobra.Command{
		Use:   "add <shortcode-or-url>",
		Short: "Attach a manually discovered media URL to a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mediaURL == "" {
				return fmt.Errorf("--url is required")
			}
			if _, err := url.ParseRequestURI(mediaURL); err != nil {
				return fmt.Errorf("invalid --url: %w", err)
			}
			if mediaType == "" {
				mediaType = inferMediaType(mediaURL)
			}
			if mediaType != "image" && mediaType != "video" {
				return fmt.Errorf("--type must be image or video")
			}
			if index <= 0 {
				index = 1
			}
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := posts.Get(cmd.Context(), db.DB, args[0])
			if err != nil {
				return fmt.Errorf("POST_NOT_FOUND: %w", err)
			}
			if thumbnailURL == "" && mediaType == "image" {
				thumbnailURL = mediaURL
			}
			if err := posts.ApplyMetadata(cmd.Context(), db.DB, posts.MetadataUpdate{
				Shortcode:    p.Shortcode,
				MediaType:    mediaType,
				IsVideo:      mediaType == "video",
				ThumbnailURL: thumbnailURL,
				Media: []posts.Media{{
					MediaIndex:   index,
					MediaType:    mediaType,
					RemoteURL:    mediaURL,
					ThumbnailURL: thumbnailURL,
				}},
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Media attached\nPost: %s\nIndex: %d\nType: %s\n", p.Shortcode, index, mediaType)
			return nil
		},
	}
	add.Flags().StringVar(&mediaURL, "url", "", "Remote media URL")
	add.Flags().StringVar(&mediaType, "type", "", "image or video")
	add.Flags().IntVar(&index, "index", 1, "Media index")
	add.Flags().StringVar(&thumbnailURL, "thumbnail-url", "", "Optional thumbnail URL")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{
		Use:   "list <shortcode-or-url>",
		Short: "List media rows for a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := posts.Get(cmd.Context(), db.DB, args[0])
			if err != nil {
				return fmt.Errorf("POST_NOT_FOUND: %w", err)
			}
			media, err := posts.ListMedia(cmd.Context(), db.DB, p.ID)
			if err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), media)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Index  Type   Status      Local Path  Remote URL")
			for _, m := range media {
				fmt.Fprintf(cmd.OutOrStdout(), "%-6d %-6s %-11s %-11s %s\n", m.MediaIndex, m.MediaType, m.Status, m.LocalPath, m.RemoteURL)
			}
			return nil
		},
	})
	return cmd
}

func inferMediaType(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, ".mp4"), strings.Contains(lower, "video"):
		return "video"
	default:
		return "image"
	}
}
