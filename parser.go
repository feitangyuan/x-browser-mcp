package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSearchMode  = "latest"
	defaultSearchLimit = 8
	maxSearchLimit     = 20
	maxTimelineLimit   = 50
	xTimeLayout        = "Mon Jan 02 15:04:05 -0700 2006"
)

func ValidateSearchRequest(req SearchRequest) (NormalizedSearchRequest, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return NormalizedSearchRequest{}, errors.New("query is required")
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = defaultSearchMode
	}
	if mode != "latest" && mode != "top" {
		return NormalizedSearchRequest{}, fmt.Errorf("unsupported mode: %s", mode)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	return NormalizedSearchRequest{
		Query: query,
		Mode:  mode,
		Limit: limit,
	}, nil
}

func NormalizeTimelineLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxTimelineLimit {
		return maxTimelineLimit
	}
	return limit
}

func ParseSearchTimeline(payload []byte, limit int) ([]XPost, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}

	instructions := asSlice(dig(root, "data", "search_by_raw_query", "search_timeline", "timeline", "instructions"))
	if len(instructions) == 0 {
		return nil, errors.New("search timeline instructions not found")
	}

	posts := make([]XPost, 0, min(limit, len(instructions)))
	seen := map[string]struct{}{}

	for _, instruction := range instructions {
		instMap := asMap(instruction)
		entries := asSlice(instMap["entries"])
		for _, entry := range entries {
			entryMap := asMap(entry)
			result := asMap(dig(entryMap, "content", "itemContent", "tweet_results", "result"))
			post, ok := parsePostResult(result)
			if !ok || post.ID == "" {
				continue
			}
			if _, exists := seen[post.ID]; exists {
				continue
			}
			seen[post.ID] = struct{}{}
			posts = append(posts, post)
			if limit > 0 && len(posts) >= limit {
				return posts, nil
			}
		}
	}

	if len(posts) == 0 {
		return nil, errors.New("no posts found in search timeline")
	}

	return posts, nil
}

func BuildSummary(posts []XPost, query string, quoteCount int) (string, []RelatedUser) {
	if len(posts) == 0 {
		return "No recent X posts found.", nil
	}
	if quoteCount <= 0 {
		quoteCount = 3
	}

	type userStats struct {
		user  RelatedUser
		order int
	}

	counts := map[string]*userStats{}
	order := 0
	for _, post := range posts {
		handle := strings.TrimSpace(post.Author.ScreenName)
		if handle == "" {
			continue
		}
		if _, ok := counts[handle]; !ok {
			counts[handle] = &userStats{
				user: RelatedUser{
					Handle:    handle,
					Name:      post.Author.Name,
					PostCount: 0,
				},
				order: order,
			}
			order++
		}
		counts[handle].user.PostCount++
	}

	users := make([]RelatedUser, 0, len(counts))
	stats := make([]*userStats, 0, len(counts))
	for _, item := range counts {
		stats = append(stats, item)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].user.PostCount == stats[j].user.PostCount {
			return stats[i].order < stats[j].order
		}
		return stats[i].user.PostCount > stats[j].user.PostCount
	})
	for _, item := range stats {
		users = append(users, item.user)
	}

	handles := make([]string, 0, min(3, len(users)))
	for i, user := range users {
		if i >= 3 {
			break
		}
		handles = append(handles, "@"+user.Handle)
	}

	quotes := make([]string, 0, min(quoteCount, len(posts)))
	for i, post := range posts {
		if i >= quoteCount {
			break
		}
		quotes = append(quotes, fmt.Sprintf("@%s: %q", post.Author.ScreenName, truncateText(post.Text, 180)))
	}

	summary := fmt.Sprintf(
		"Found %d recent posts for %q. Active accounts: %s. Representative quotes: %s.",
		len(posts),
		query,
		strings.Join(handles, ", "),
		strings.Join(quotes, " | "),
	)
	return summary, users
}

func BuildHomeTimelineSummary(posts []XPost, quoteCount int) (string, []RelatedUser) {
	if len(posts) == 0 {
		return "No posts found on the X home timeline.", nil
	}
	if quoteCount <= 0 {
		quoteCount = 3
	}

	_, users := BuildSummary(posts, "home", quoteCount)

	handles := make([]string, 0, min(3, len(users)))
	for i, user := range users {
		if i >= 3 {
			break
		}
		handles = append(handles, "@"+user.Handle)
	}

	quotes := make([]string, 0, min(quoteCount, len(posts)))
	for i, post := range posts {
		if i >= quoteCount {
			break
		}
		quotes = append(quotes, fmt.Sprintf("@%s: %q", post.Author.ScreenName, truncateText(post.Text, 180)))
	}

	summary := fmt.Sprintf(
		"Read %d posts from the X home timeline. Active accounts: %s. Representative posts: %s.",
		len(posts),
		strings.Join(handles, ", "),
		strings.Join(quotes, " | "),
	)
	return summary, users
}

func ConvertDOMCandidatesToPosts(candidates []DOMPostCandidate, limit int) ([]XPost, error) {
	posts := make([]XPost, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		handle := strings.TrimPrefix(strings.TrimSpace(candidate.Handle), "@")
		href := strings.TrimSpace(candidate.Href)
		text := normalizeText(candidate.Text)
		if handle == "" || href == "" || text == "" {
			continue
		}

		postID := statusIDFromHref(href)
		if postID == "" {
			continue
		}

		post := XPost{
			ID:   postID,
			Text: text,
			URL:  normalizeStatusURL(href),
			Author: XUser{
				Name:       strings.TrimSpace(candidate.Name),
				ScreenName: handle,
			},
			Metrics: XMetrics{
				Replies: candidate.Replies,
				Reposts: candidate.Reposts,
				Likes:   candidate.Likes,
			},
		}

		if createdAt := strings.TrimSpace(candidate.CreatedAt); createdAt != "" {
			if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
				post.CreatedAt = parsed.UTC()
			}
		}

		posts = append(posts, post)
		if limit > 0 && len(posts) >= limit {
			break
		}
	}

	if len(posts) == 0 {
		return nil, errors.New("no posts found in DOM search results")
	}
	return posts, nil
}

func MergePosts(existing []XPost, next []XPost, limit int) []XPost {
	if limit <= 0 {
		limit = len(existing) + len(next)
	}

	merged := make([]XPost, 0, min(limit, len(existing)+len(next)))
	seen := make(map[string]struct{}, len(existing)+len(next))

	for _, post := range existing {
		if post.ID == "" {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		merged = append(merged, post)
		if len(merged) >= limit {
			return merged
		}
	}

	for _, post := range next {
		if post.ID == "" {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		merged = append(merged, post)
		if len(merged) >= limit {
			return merged
		}
	}

	return merged
}

func ComputeSearchPhaseTimeouts(total time.Duration) (capture time.Duration, fallback time.Duration) {
	if total <= 10*time.Second {
		return total / 2, total - (total / 2)
	}

	fallback = 8 * time.Second
	capture = total - fallback
	if capture < 5*time.Second {
		capture = 5 * time.Second
		fallback = total - capture
	}
	return capture, fallback
}

func parsePostResult(result map[string]any) (XPost, bool) {
	if len(result) == 0 {
		return XPost{}, false
	}
	if tweet := asMap(result["tweet"]); len(tweet) > 0 {
		result = tweet
	}

	legacy := asMap(result["legacy"])
	core := asMap(result["core"])
	userResult := asMap(dig(core, "user_results", "result"))
	userLegacy := asMap(userResult["legacy"])
	if len(legacy) == 0 || len(userLegacy) == 0 {
		return XPost{}, false
	}

	post := XPost{
		ID:   asString(result["rest_id"]),
		Text: normalizeText(asString(legacy["full_text"])),
		Author: XUser{
			ID:          asString(userResult["rest_id"]),
			Name:        asString(userLegacy["name"]),
			ScreenName:  asString(userLegacy["screen_name"]),
			Description: asString(userLegacy["description"]),
		},
		Metrics: XMetrics{
			Replies: asInt(legacy["reply_count"]),
			Reposts: asInt(legacy["retweet_count"]),
			Likes:   asInt(legacy["favorite_count"]),
			Views:   parseViewCount(result),
		},
	}

	if createdAt := asString(legacy["created_at"]); createdAt != "" {
		parsedTime, err := time.Parse(xTimeLayout, createdAt)
		if err == nil {
			post.CreatedAt = parsedTime.UTC()
		}
	}
	if post.Author.ScreenName != "" && post.ID != "" {
		post.URL = fmt.Sprintf("https://x.com/%s/status/%s", post.Author.ScreenName, post.ID)
	}
	return post, post.ID != "" && post.Text != "" && post.Author.ScreenName != ""
}

func parseViewCount(result map[string]any) int {
	if count := asString(dig(result, "views", "count")); count != "" {
		return asInt(count)
	}
	if count := asString(dig(result, "view_count_info", "count")); count != "" {
		return asInt(count)
	}
	return 0
}

func dig(value any, path ...string) any {
	current := value
	for _, segment := range path {
		currentMap := asMap(current)
		if len(currentMap) == 0 {
			return nil
		}
		current = currentMap[segment]
	}
	return current
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func asSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func asInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		i, _ := typed.Int64()
		return int(i)
	case string:
		cleaned := strings.ReplaceAll(typed, ",", "")
		i, _ := strconv.Atoi(cleaned)
		return i
	default:
		return 0
	}
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.Join(strings.Fields(text), " ")
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func absoluteXURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if !strings.HasPrefix(href, "/") {
		href = "/" + href
	}
	return "https://x.com" + href
}

func normalizeStatusURL(href string) string {
	postID := statusIDFromHref(href)
	if postID == "" {
		return absoluteXURL(href)
	}

	trimmed := strings.TrimSpace(href)
	parts := strings.Split(trimmed, "/")
	handle := ""
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "status" && i > 0 {
			handle = parts[i-1]
			break
		}
	}
	if handle == "" {
		return absoluteXURL(href)
	}
	return absoluteXURL("/" + handle + "/status/" + postID)
}

func statusIDFromHref(href string) string {
	href = strings.TrimSpace(href)
	parts := strings.Split(href, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "status" {
			return parts[i+1]
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
