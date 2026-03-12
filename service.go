package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

var errNotLoggedIn = errors.New("x session not found, run start_login first")

type cacheEntry struct {
	result    SearchResult
	expiresAt time.Time
}

type loginSession struct {
	browser              *BrowserSession
	page                 *rod.Page
	process              *exec.Cmd
	processDone          <-chan error
	deadline             time.Time
	resumeManagedBrowser bool
}

type SearchService struct {
	cfg Config

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry

	rateMu        sync.Mutex
	lastQuery     time.Time
	liveSearchLog []time.Time

	loginMu sync.Mutex
	login   *loginSession

	profileUseMu sync.Mutex

	managedBrowser    ManagedBrowserController
	launchManualLogin func(Config) (*exec.Cmd, error)
	waitForRemoteDebug func(context.Context, string) error
	watchLoginFn      func(*loginSession)
	newBrowserSession func(context.Context, Config, bool) (*BrowserSession, error)
	checkRemoteDebugLogin func(context.Context) (bool, error)
}

func NewSearchService(cfg Config) *SearchService {
	s := &SearchService{
		cfg:   cfg,
		cache: make(map[string]cacheEntry),
	}
	s.managedBrowser = NewLaunchdManagedBrowserController(cfg)
	s.launchManualLogin = LaunchManualLoginBrowser
	s.waitForRemoteDebug = WaitForRemoteDebugEndpoint
	s.watchLoginFn = s.watchLogin
	s.newBrowserSession = NewBrowserSession
	s.checkRemoteDebugLogin = s.checkRemoteDebugAuthenticated
	return s
}

func (s *SearchService) CheckLoginStatus(ctx context.Context) (*LoginStatusResponse, error) {
	status := &LoginStatusResponse{
		SessionFile: s.cfg.SessionStoragePath(),
		CheckedAt:   time.Now().UTC(),
		State:       "login_required",
	}

	s.loginMu.Lock()
	if s.login != nil && time.Now().Before(s.login.deadline) {
		status.LoginInProgress = true
		status.State = "login_in_progress"
	}
	s.loginMu.Unlock()

	if status.LoginInProgress && s.cfg.RemoteDebugMode() {
		loggedIn, err := s.checkRemoteDebugAuthenticated(ctx)
		if err == nil && loggedIn {
			status.LoggedIn = true
			status.State = "ready"
		}
		return status, nil
	}

	if s.cfg.CookieMode() {
		if _, err := os.Stat(s.cfg.CookiesPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return status, nil
			}
			return nil, err
		}
	}

	browser, page, err := s.newAuthenticatedPage(ctx)
	if err != nil {
		if errors.Is(err, errNotLoggedIn) {
			return status, nil
		}
		if status.LoginInProgress && strings.Contains(err.Error(), "chrome profile is already in use") {
			return status, nil
		}
		if status.LoginInProgress && s.cfg.RemoteDebugMode() {
			return status, nil
		}
		status.State = "degraded"
		return nil, err
	}
	defer page.Close()
	defer browser.Close()

	if err := page.Navigate("https://x.com/home"); err != nil {
		return nil, err
	}
	if err := page.WaitLoad(); err != nil {
		return nil, err
	}

	loggedIn, err := isLoggedIn(page)
	if err != nil {
		return nil, err
	}
	status.LoggedIn = loggedIn
	if loggedIn {
		status.State = "ready"
	}
	return status, nil
}

func (s *SearchService) StartLogin(ctx context.Context) (*StartLoginResponse, error) {
	s.loginMu.Lock()
	if s.login != nil && time.Now().Before(s.login.deadline) {
		resp := &StartLoginResponse{
			Message:     "login browser is already open",
			Deadline:    s.login.deadline.UTC(),
			SessionFile: s.cfg.SessionStoragePath(),
		}
		s.loginMu.Unlock()
		return resp, nil
	}
	s.loginMu.Unlock()

	if s.cfg.RemoteDebugMode() {
		return s.startRemoteDebugLogin(ctx)
	}

	if s.cfg.ProfileMode() {
		if _, err := s.launchManualLogin(s.cfg); err != nil {
			return nil, err
		}
		login := &loginSession{
			deadline: time.Now().Add(s.cfg.LoginTimeout),
		}
		s.loginMu.Lock()
		s.login = login
		s.loginMu.Unlock()
		return &StartLoginResponse{
			Message:     "browser opened in normal mode, complete X login and then close the window",
			Deadline:    login.deadline.UTC(),
			SessionFile: s.cfg.SessionStoragePath(),
		}, nil
	}

	session, err := s.openBrowserSession(context.Background(), false)
	if err != nil {
		return nil, err
	}

	page, err := session.NewPage()
	if err != nil {
		session.Close()
		return nil, err
	}

	if err := page.Navigate("https://x.com/home"); err != nil {
		page.Close()
		session.Close()
		return nil, err
	}

	login := &loginSession{
		browser:  session,
		page:     page,
		deadline: time.Now().Add(s.cfg.LoginTimeout),
	}

	s.loginMu.Lock()
	s.login = login
	s.loginMu.Unlock()

	go s.watchLoginFn(login)

	return &StartLoginResponse{
		Message:     "browser opened, complete X login in the visible window",
		Deadline:    login.deadline.UTC(),
		SessionFile: s.cfg.SessionStoragePath(),
	}, nil
}

func (s *SearchService) startRemoteDebugLogin(ctx context.Context) (*StartLoginResponse, error) {
	if strings.TrimSpace(s.cfg.UserDataDir) == "" {
		return nil, fmt.Errorf("user-data-dir is required for remote-debug login handoff")
	}
	if s.managedBrowser == nil {
		return nil, fmt.Errorf("managed browser controller is not configured for remote-debug login handoff")
	}
	loggedIn, err := s.checkRemoteDebugLogin(ctx)
	if err == nil && loggedIn {
		return &StartLoginResponse{
			Message:     "x profile is already authenticated; continuing to use the background browser",
			Deadline:    time.Now().UTC(),
			SessionFile: s.cfg.SessionStoragePath(),
		}, nil
	}

	if err := s.managedBrowser.Suspend(ctx); err != nil {
		return nil, err
	}
	if err := waitForProfileAvailability(ctx, s.cfg.UserDataDir); err != nil {
		_ = s.managedBrowser.Resume(context.Background())
		return nil, err
	}

	cmd, err := s.launchManualLogin(s.cfg)
	if err != nil {
		_ = s.managedBrowser.Resume(context.Background())
		return nil, err
	}
	if err := s.waitForRemoteDebug(ctx, s.cfg.RemoteDebugURL); err != nil {
		stopCommand(cmd)
		_ = s.managedBrowser.Resume(context.Background())
		return nil, err
	}

	login := &loginSession{
		process:              cmd,
		processDone:          waitForCommand(cmd),
		deadline:             time.Now().Add(s.cfg.LoginTimeout),
		resumeManagedBrowser: true,
	}

	s.loginMu.Lock()
	s.login = login
	s.loginMu.Unlock()

	go s.watchLoginFn(login)

	return &StartLoginResponse{
		Message:     "browser opened in a visible window, complete X login and it will return to background automatically",
		Deadline:    login.deadline.UTC(),
		SessionFile: s.cfg.SessionStoragePath(),
	}, nil
}

func (s *SearchService) SearchX(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	normalized, err := ValidateSearchRequest(req)
	if err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("%s|%s|%d", normalized.Query, normalized.Mode, normalized.Limit)
	if cached, ok := s.getCached(cacheKey); ok {
		cached.Cached = true
		return &cached, nil
	}

	if err := s.waitForTurn(ctx); err != nil {
		return nil, err
	}

	searchCtx, cancel := context.WithTimeout(ctx, s.cfg.SearchTimeout)
	defer cancel()
	captureTimeout, fallbackTimeout := ComputeSearchPhaseTimeouts(s.cfg.SearchTimeout)

	browser, page, err := s.newAuthenticatedPage(searchCtx)
	if err != nil {
		return nil, err
	}
	defer page.Close()
	defer browser.Close()

	loggedIn, err := s.ensureLoggedIn(searchCtx, page)
	if err != nil {
		return nil, err
	}
	if !loggedIn {
		return nil, errNotLoggedIn
	}

	captureCtx, captureCancel := context.WithTimeout(searchCtx, captureTimeout)
	payload, err := s.captureTimelinePayload(captureCtx, page, buildSearchURL(normalized))
	captureCancel()
	if err == nil {
		posts, parseErr := ParseSearchTimeline(payload, normalized.Limit)
		if parseErr == nil {
			summary, users := BuildSummary(posts, normalized.Query, min(3, normalized.Limit))
			result := SearchResult{
				Query:        normalized.Query,
				Mode:         normalized.Mode,
				Summary:      summary,
				Posts:        posts,
				RelatedUsers: users,
				FetchedAt:    time.Now().UTC(),
			}

			s.putCached(cacheKey, result)
			return &result, nil
		}
		logrus.WithError(parseErr).Warn("failed to parse X search timeline response, falling back to DOM extraction")
	}
	if err != nil {
		logrus.WithError(err).Warn("failed to capture X search timeline response, falling back to DOM extraction")
	}

	fallbackCtx, fallbackCancel := context.WithTimeout(searchCtx, fallbackTimeout)
	posts, err := s.extractPostsFromDOM(fallbackCtx, page, normalized.Limit)
	fallbackCancel()
	if err != nil {
		return nil, err
	}

	summary, users := BuildSummary(posts, normalized.Query, min(3, normalized.Limit))
	result := SearchResult{
		Query:        normalized.Query,
		Mode:         normalized.Mode,
		Summary:      summary,
		Posts:        posts,
		RelatedUsers: users,
		FetchedAt:    time.Now().UTC(),
	}

	s.putCached(cacheKey, result)
	return &result, nil
}

func (s *SearchService) ReadHomeTimeline(ctx context.Context, req HomeTimelineRequest) (*HomeTimelineResult, error) {
	limit := NormalizeTimelineLimit(req.Limit)
	cacheKey := fmt.Sprintf("home|%d", limit)
	if cached, ok := s.getCached(cacheKey); ok {
		summary, users := BuildHomeTimelineSummary(cached.Posts, min(3, limit))
		return &HomeTimelineResult{
			Summary:      summary,
			Posts:        cached.Posts,
			RelatedUsers: users,
			FetchedAt:    cached.FetchedAt,
			Cached:       true,
		}, nil
	}

	if err := s.waitForTurn(ctx); err != nil {
		return nil, err
	}

	readCtx, cancel := context.WithTimeout(ctx, s.cfg.SearchTimeout)
	defer cancel()

	browser, page, err := s.newAuthenticatedPage(readCtx)
	if err != nil {
		return nil, err
	}
	defer page.Close()
	defer browser.Close()

	loggedIn, err := s.ensureLoggedIn(readCtx, page)
	if err != nil {
		return nil, err
	}
	if !loggedIn {
		return nil, errNotLoggedIn
	}

	if err := page.Navigate("https://x.com/home"); err != nil {
		return nil, err
	}
	if err := page.WaitLoad(); err != nil {
		return nil, err
	}

	posts, err := s.extractHomeTimelinePosts(readCtx, page, limit)
	if err != nil {
		return nil, err
	}

	summary, users := BuildHomeTimelineSummary(posts, min(3, limit))
	cached := SearchResult{
		Query:        "__home__",
		Mode:         "home",
		Summary:      summary,
		Posts:        posts,
		RelatedUsers: users,
		FetchedAt:    time.Now().UTC(),
	}
	s.putCached(cacheKey, cached)

	return &HomeTimelineResult{
		Summary:      summary,
		Posts:        posts,
		RelatedUsers: users,
		FetchedAt:    cached.FetchedAt,
	}, nil
}

func (s *SearchService) newAuthenticatedPage(ctx context.Context) (*BrowserSession, *rod.Page, error) {
	if s.cfg.CookieMode() {
		if _, err := os.Stat(s.cfg.CookiesPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, errNotLoggedIn
			}
			return nil, nil, err
		}
	}

	browser, err := s.openBrowserSession(ctx, s.cfg.Headless)
	if err != nil {
		return nil, nil, err
	}

	page, err := browser.NewPage()
	if err != nil {
		browser.Close()
		return nil, nil, err
	}

	if s.cfg.CookieMode() {
		if err := LoadCookies(page, s.cfg.CookiesPath); err != nil {
			page.Close()
			browser.Close()
			return nil, nil, err
		}
	}
	return browser, page, nil
}

func (s *SearchService) openBrowserSession(ctx context.Context, headless bool) (*BrowserSession, error) {
	if s.cfg.ProfileMode() {
		s.profileUseMu.Lock()
	}

	session, err := s.newBrowserSession(ctx, s.cfg, headless)
	if err != nil {
		if s.cfg.ProfileMode() {
			s.profileUseMu.Unlock()
		}
		return nil, err
	}

	if s.cfg.ProfileMode() {
		session.onClose = func() {
			s.profileUseMu.Unlock()
		}
	}
	return session, nil
}

func (s *SearchService) watchLogin(login *loginSession) {
	if login.process != nil && login.resumeManagedBrowser {
		s.watchManagedRemoteLogin(login)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithDeadline(context.Background(), login.deadline)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			logrus.Warn("x login timed out before cookies were captured")
			s.clearLogin(login)
			login.page.Close()
			login.browser.Close()
			return
		case <-ticker.C:
			loggedIn, err := isLoggedIn(login.page)
			if err != nil {
				logrus.WithError(err).Warn("x login polling failed")
				continue
			}
			if !loggedIn {
				continue
			}
			if s.cfg.CookieMode() {
				if err := SaveCookies(login.browser.browser, s.cfg.CookiesPath); err != nil {
					logrus.WithError(err).Error("failed to save X cookies")
				} else {
					logrus.Infof("saved X session cookies to %s", s.cfg.CookiesPath)
				}
			} else {
				logrus.Infof("x login completed in persistent profile %s", s.cfg.UserDataDir)
			}
			s.clearLogin(login)
			login.page.Close()
			login.browser.Close()
			return
		}
	}
}

func (s *SearchService) watchManagedRemoteLogin(login *loginSession) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithDeadline(context.Background(), login.deadline)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			logrus.Warn("x login timed out before persistent profile was re-authenticated")
			s.completeManagedRemoteLogin(login, false)
			return
		case <-login.processDone:
			s.completeManagedRemoteLogin(login, false)
			return
		case <-ticker.C:
			loggedIn, err := s.checkRemoteDebugAuthenticated(ctx)
			if err != nil {
				logrus.WithError(err).Warn("x remote login polling failed")
				continue
			}
			if !loggedIn {
				continue
			}
			s.completeManagedRemoteLogin(login, true)
			return
		}
	}
}

func (s *SearchService) completeManagedRemoteLogin(login *loginSession, observedAuthenticated bool) {
	stopCommand(login.process)
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	managedReady := false
	if login.resumeManagedBrowser && s.managedBrowser != nil {
		if err := waitForProfileAvailability(waitCtx, s.cfg.UserDataDir); err != nil {
			logrus.WithError(err).Warn("failed waiting for interactive browser profile to unlock")
		} else if err := s.managedBrowser.Resume(waitCtx); err != nil {
			logrus.WithError(err).Warn("failed to resume managed headless browser")
		} else if err := s.waitForRemoteDebug(waitCtx, s.cfg.RemoteDebugURL); err != nil {
			logrus.WithError(err).Warn("managed headless browser did not come back in time")
		} else {
			managedReady = true
		}
	}

	finalAuthenticated := observedAuthenticated
	if managedReady {
		loggedIn, err := s.checkRemoteDebugAuthenticated(waitCtx)
		if err != nil {
			logrus.WithError(err).Warn("failed to verify login state after managed browser resume")
		} else {
			finalAuthenticated = loggedIn
		}
	}

	if finalAuthenticated {
		logrus.Infof("x login completed in persistent profile %s", s.cfg.UserDataDir)
	} else {
		logrus.Warn("x login window closed before authenticated cookies were confirmed")
	}
	s.clearLogin(login)
}

func (s *SearchService) clearLogin(login *loginSession) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.login == login {
		s.login = nil
	}
}

func (s *SearchService) getCached(key string) (SearchResult, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return SearchResult{}, false
	}
	return entry.result, true
}

func (s *SearchService) putCached(key string, result SearchResult) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache[key] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(s.cfg.CacheTTL),
	}
}

func (s *SearchService) waitForTurn(ctx context.Context) error {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	wait := s.cfg.MinSearchInterval - time.Since(s.lastQuery)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	updated, retryAfter, allowed := AllowLiveSearch(s.liveSearchLog, time.Now(), s.cfg.SearchBudgetWindow, s.cfg.MaxLiveSearches)
	if !allowed {
		return fmt.Errorf("live search budget reached; wait %s before the next uncached X query", retryAfter.Round(time.Second))
	}
	s.liveSearchLog = updated
	s.lastQuery = time.Now()
	return nil
}

func AllowLiveSearch(history []time.Time, now time.Time, window time.Duration, maxSearches int) ([]time.Time, time.Duration, bool) {
	if maxSearches <= 0 || window <= 0 {
		return append(history[:0:0], now), 0, true
	}

	cutoff := now.Add(-window)
	filtered := make([]time.Time, 0, len(history)+1)
	for _, ts := range history {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}

	if len(filtered) >= maxSearches {
		retryAfter := filtered[0].Add(window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return filtered, retryAfter, false
	}

	filtered = append(filtered, now)
	return filtered, 0, true
}

func (s *SearchService) captureTimelinePayload(ctx context.Context, page *rod.Page, searchURL string) ([]byte, error) {
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		return nil, err
	}

	bodyCh := make(chan []byte, 4)
	requests := struct {
		sync.Mutex
		ids map[proto.NetworkRequestID]struct{}
	}{
		ids: make(map[proto.NetworkRequestID]struct{}),
	}

	eventPage := page.Context(ctx)
	go eventPage.EachEvent(
		func(e *proto.NetworkResponseReceived) {
			if !strings.Contains(e.Response.URL, "SearchTimeline") {
				return
			}
			requests.Lock()
			requests.ids[e.RequestID] = struct{}{}
			requests.Unlock()
		},
		func(e *proto.NetworkLoadingFinished) {
			requests.Lock()
			_, ok := requests.ids[e.RequestID]
			requests.Unlock()
			if !ok {
				return
			}

			res, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err != nil {
				return
			}

			body := []byte(res.Body)
			if res.Base64Encoded {
				decoded, err := base64.StdEncoding.DecodeString(res.Body)
				if err == nil {
					body = decoded
				}
			}

			select {
			case bodyCh <- body:
			default:
			}
		},
	)()

	if err := page.Navigate(searchURL); err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for X search response: %w", ctx.Err())
		case body := <-bodyCh:
			posts, err := ParseSearchTimeline(body, 1)
			if err == nil && len(posts) > 0 {
				return body, nil
			}
		}
	}
}

func (s *SearchService) ensureLoggedIn(ctx context.Context, page *rod.Page) (bool, error) {
	if err := page.Navigate("https://x.com/home"); err != nil {
		return false, err
	}
	if err := page.WaitLoad(); err != nil {
		return false, err
	}

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	return isLoggedIn(page)
}

func (s *SearchService) extractPostsFromDOM(ctx context.Context, page *rod.Page, limit int) ([]XPost, error) {
	deadline := time.Now().Add(8 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		candidates, err := readDOMCandidates(page, limit)
		if err == nil {
			posts, convertErr := ConvertDOMCandidatesToPosts(candidates, limit)
			if convertErr == nil {
				return posts, nil
			}
		}

		if time.Now().After(deadline) {
			if err != nil {
				return nil, err
			}
			return nil, errors.New("no X posts found from DOM fallback")
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *SearchService) extractHomeTimelinePosts(ctx context.Context, page *rod.Page, limit int) ([]XPost, error) {
	deadline := time.Now().Add(18 * time.Second)
	collected := make([]XPost, 0, limit)
	lastCount := 0
	stalledRounds := 0

	for {
		select {
		case <-ctx.Done():
			if len(collected) > 0 {
				return collected, nil
			}
			return nil, ctx.Err()
		default:
		}

		candidates, err := readDOMCandidates(page, limit)
		if err == nil {
			posts, convertErr := ConvertDOMCandidatesToPosts(candidates, limit)
			if convertErr == nil {
				collected = MergePosts(collected, posts, limit)
				if len(collected) >= limit {
					return collected, nil
				}
				if len(collected) > lastCount {
					lastCount = len(collected)
					stalledRounds = 0
				} else {
					stalledRounds++
				}
			}
		}

		if len(collected) > 0 && (stalledRounds >= 3 || time.Now().After(deadline)) {
			return collected, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("no X posts found from home timeline")
		}

		if _, err := page.Eval(`() => {
			const step = Math.max(window.innerHeight * 0.9, 700);
			window.scrollBy(0, step);
			return document.documentElement.scrollTop || document.body.scrollTop || 0;
		}`); err != nil && len(collected) > 0 {
			return collected, nil
		}
		time.Sleep(1200 * time.Millisecond)
	}
}

func readDOMCandidates(page *rod.Page, limit int) ([]DOMPostCandidate, error) {
	if limit <= 0 {
		limit = 10
	}

	result, err := page.Eval(`limit => {
		const parseMetric = node => {
			if (!node) return 0;
			const text = (node.getAttribute('aria-label') || node.innerText || '').trim();
			if (!text) return 0;
			const normalized = text.toLowerCase().replace(/,/g, '');
			const match = normalized.match(/([\d.]+)\s*([kmb])?/);
			if (!match) return 0;
			const value = Number.parseFloat(match[1]);
			if (Number.isNaN(value)) return 0;
			const unit = match[2] || '';
			if (unit === 'k') return Math.round(value * 1000);
			if (unit === 'm') return Math.round(value * 1000000);
			if (unit === 'b') return Math.round(value * 1000000000);
			return Math.round(value);
		};

		const articles = Array.from(document.querySelectorAll('article[data-testid="tweet"]'));
		return articles.slice(0, limit).map(article => {
			const timeEl = article.querySelector('time');
			const statusLink = timeEl?.closest('a[href*="/status/"]') || article.querySelector('a[href*="/status/"]');
			const userName = article.querySelector('[data-testid="User-Name"]');
			const spans = userName ? Array.from(userName.querySelectorAll('span')).map(node => (node.textContent || '').trim()).filter(Boolean) : [];
			const handle = spans.find(text => text.startsWith('@')) || '';
			const name = spans.find(text => !text.startsWith('@')) || handle.replace(/^@/, '');
			const replyNode = article.querySelector('[data-testid="reply"]');
			const repostNode = article.querySelector('[data-testid="retweet"], [data-testid="unretweet"]');
			const likeNode = article.querySelector('[data-testid="like"], [data-testid="unlike"]');
			return {
				href: statusLink ? (statusLink.getAttribute('href') || '') : '',
				text: article.querySelector('[data-testid="tweetText"]')?.innerText || '',
				created_at: timeEl ? (timeEl.getAttribute('datetime') || '') : '',
				handle,
				name,
				replies: parseMetric(replyNode),
				reposts: parseMetric(repostNode),
				likes: parseMetric(likeNode)
			};
		});
	}`, limit)
	if err != nil {
		return nil, err
	}

	var candidates []DOMPostCandidate
	if err := result.Value.Unmarshal(&candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}

func isLoggedIn(page *rod.Page) (bool, error) {
	info, err := page.Info()
	if err == nil && strings.Contains(info.URL, "/i/flow/login") {
		return false, nil
	}

	for _, selector := range []string{
		`[data-testid="SideNav_AccountSwitcher_Button"]`,
		`[data-testid="AppTabBar_Profile_Link"]`,
		`a[href="/home"]`,
	} {
		found, err := hasSelector(page, selector)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}

	cookies, err := page.Browser().GetCookies()
	if err != nil {
		return false, err
	}
	if hasAuthenticatedXCookies(cookies) {
		return true, nil
	}

	return false, nil
}

func hasAuthenticatedXCookies(cookies []*proto.NetworkCookie) bool {
	hasAuthToken := false
	hasCT0 := false

	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		switch cookie.Name {
		case "auth_token":
			hasAuthToken = true
		case "ct0":
			hasCT0 = true
		}
	}

	return hasAuthToken && hasCT0
}

func (s *SearchService) checkRemoteDebugAuthenticated(ctx context.Context) (bool, error) {
	session, err := s.openBrowserSession(ctx, false)
	if err != nil {
		return false, err
	}
	defer session.Close()

	cookies, err := session.browser.GetCookies()
	if err != nil {
		return false, err
	}
	return hasAuthenticatedXCookies(cookies), nil
}

func hasSelector(page *rod.Page, selector string) (bool, error) {
	timed := page.Timeout(2 * time.Second)
	defer timed.CancelTimeout()

	el, err := timed.Element(selector)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return false, nil
		}
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return false, nil
		}
		return false, nil
	}
	return el != nil, nil
}

func buildSearchURL(req NormalizedSearchRequest) string {
	values := url.Values{}
	values.Set("q", req.Query)
	values.Set("src", "typed_query")
	if req.Mode == "latest" {
		values.Set("f", "live")
	}
	return "https://x.com/search?" + values.Encode()
}

func waitForCommand(cmd *exec.Cmd) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	return done
}

func stopCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func waitForProfileAvailability(ctx context.Context, userDataDir string) error {
	if strings.TrimSpace(userDataDir) == "" {
		return nil
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := CheckUserDataDirAvailability(userDataDir); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
