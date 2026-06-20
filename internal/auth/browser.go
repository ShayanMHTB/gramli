package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// BrowserLogin opens a visible Chrome window to Instagram's login page and waits
// for the user to complete authentication (email, password, 2FA — whatever the
// account requires). Once a valid sessionid cookie is detected, all
// instagram.com cookies are captured, the browser is closed, and the cookies are
// returned for saving.
func BrowserLogin(ctx context.Context, headless bool, timeout time.Duration) ([]Cookie, error) {
	chromePath, err := findChrome()
	if err != nil {
		return nil, err
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(taskCtx, timeout)
	defer timeoutCancel()

	if err := chromedp.Run(timeoutCtx, chromedp.Navigate("https://www.instagram.com/accounts/login/")); err != nil {
		return nil, fmt.Errorf("BROWSER_FAILED: could not open Instagram login page: %w", err)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			if timeoutCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("BROWSER_LOGIN_TIMEOUT: login not completed within %s", timeout)
			}
			return nil, fmt.Errorf("BROWSER_CANCELLED: %w", timeoutCtx.Err())

		case <-ticker.C:
			var all []*network.Cookie
			err := chromedp.Run(timeoutCtx, chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				all, err = storage.GetCookies().Do(ctx)
				return err
			}))
			if err != nil {
				// transient error (e.g. page still loading); keep polling
				continue
			}
			if !hasSessionCookie(all) {
				continue
			}
			return filterInstagramCookies(all), nil
		}
	}
}

func hasSessionCookie(cookies []*network.Cookie) bool {
	for _, c := range cookies {
		if c.Name == "sessionid" && c.Value != "" && strings.Contains(c.Domain, "instagram.com") {
			return true
		}
	}
	return false
}

func filterInstagramCookies(all []*network.Cookie) []Cookie {
	out := make([]Cookie, 0, len(all))
	for _, c := range all {
		if !strings.Contains(c.Domain, "instagram.com") {
			continue
		}
		out = append(out, Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			Expires:  float64(c.Expires),
		})
	}
	return out
}

// findChrome returns the path to a Chrome or Chromium executable, or an error
// with install instructions if neither is found.
func findChrome() (string, error) {
	var absolute []string
	var inPath []string

	switch runtime.GOOS {
	case "darwin":
		absolute = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		}
		inPath = []string{"google-chrome", "chromium-browser", "chromium"}
	case "linux":
		inPath = []string{"google-chrome", "google-chrome-stable", "chromium-browser", "chromium"}
	case "windows":
		absolute = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
		inPath = []string{"chrome", "chromium"}
	}

	for _, p := range absolute {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range inPath {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	install := "brew install --cask google-chrome"
	if runtime.GOOS == "linux" {
		install = "sudo apt install chromium-browser  # or equivalent"
	}
	return "", fmt.Errorf(
		"BROWSER_NOT_FOUND: Chrome or Chromium is required for --web login\n\n"+
			"Install: %s\n"+
			"Or use:  gramli login --cookie-file ./cookies.json",
		install,
	)
}
