package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSearchRequestRejectsBlankQuery(t *testing.T) {
	_, err := ValidateSearchRequest(SearchRequest{Query: "   "})
	if err == nil {
		t.Fatal("expected blank query to fail validation")
	}
}

func TestValidateSearchRequestNormalizesDefaults(t *testing.T) {
	got, err := ValidateSearchRequest(SearchRequest{
		Query: "  model context protocol  ",
		Mode:  "LATEST",
		Limit: 99,
	})
	if err != nil {
		t.Fatalf("ValidateSearchRequest returned error: %v", err)
	}

	if got.Query != "model context protocol" {
		t.Fatalf("unexpected query: %q", got.Query)
	}
	if got.Mode != "latest" {
		t.Fatalf("unexpected mode: %q", got.Mode)
	}
	if got.Limit != 20 {
		t.Fatalf("unexpected limit: %d", got.Limit)
	}
}

func TestParseSearchTimelineExtractsPosts(t *testing.T) {
	payload := []byte(`{
		"data": {
			"search_by_raw_query": {
				"search_timeline": {
					"timeline": {
						"instructions": [
							{
								"type": "TimelineAddEntries",
								"entries": [
									{
										"entryId": "tweet-1",
										"content": {
											"entryType": "TimelineTimelineItem",
											"itemContent": {
												"tweet_results": {
													"result": {
														"rest_id": "19001",
														"legacy": {
															"full_text": "MCP on X is getting interesting fast",
															"created_at": "Wed Mar 12 08:00:00 +0000 2026",
															"reply_count": 3,
															"retweet_count": 4,
															"favorite_count": 11
														},
														"views": {
															"count": "3456"
														},
														"core": {
															"user_results": {
																"result": {
																	"rest_id": "u1",
																	"legacy": {
																		"name": "Alice",
																		"screen_name": "aliceai",
																		"description": "AI builder"
																	}
																}
															}
														}
													}
												}
											}
										}
									},
									{
										"entryId": "tweet-2",
										"content": {
											"entryType": "TimelineTimelineItem",
											"itemContent": {
												"tweet_results": {
													"result": {
														"tweet": {
															"rest_id": "19002",
															"legacy": {
																"full_text": "Codex plus MCP makes X research much faster",
																"created_at": "Wed Mar 12 08:05:00 +0000 2026",
																"reply_count": 1,
																"retweet_count": 2,
																"favorite_count": 5
															},
															"core": {
																"user_results": {
																	"result": {
																		"rest_id": "u2",
																		"legacy": {
																			"name": "Bob",
																			"screen_name": "builderbob"
																		}
																	}
																}
															}
														}
													}
												}
											}
										}
									}
								]
							}
						]
					}
				}
			}
		}
	}`)

	posts, err := ParseSearchTimeline(payload, 10)
	if err != nil {
		t.Fatalf("ParseSearchTimeline returned error: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].Author.ScreenName != "aliceai" {
		t.Fatalf("unexpected first author: %q", posts[0].Author.ScreenName)
	}
	if posts[0].Metrics.Views != 3456 {
		t.Fatalf("unexpected views: %d", posts[0].Metrics.Views)
	}
	if posts[1].URL != "https://x.com/builderbob/status/19002" {
		t.Fatalf("unexpected url: %q", posts[1].URL)
	}
}

func TestBuildSummaryIncludesAuthorsAndQuotes(t *testing.T) {
	posts := []XPost{
		{
			ID:        "19001",
			Text:      "MCP on X is getting interesting fast",
			CreatedAt: time.Date(2026, 3, 12, 8, 0, 0, 0, time.UTC),
			URL:       "https://x.com/aliceai/status/19001",
			Author:    XUser{Name: "Alice", ScreenName: "aliceai"},
		},
		{
			ID:        "19002",
			Text:      "Codex plus MCP makes X research much faster",
			CreatedAt: time.Date(2026, 3, 12, 8, 5, 0, 0, time.UTC),
			URL:       "https://x.com/builderbob/status/19002",
			Author:    XUser{Name: "Bob", ScreenName: "builderbob"},
		},
	}

	summary, users := BuildSummary(posts, "mcp", 2)
	if len(users) != 2 {
		t.Fatalf("expected 2 related users, got %d", len(users))
	}
	if !strings.Contains(summary, "@aliceai") {
		t.Fatalf("summary missing first author: %q", summary)
	}
	if !strings.Contains(summary, "\"MCP on X is getting interesting fast\"") {
		t.Fatalf("summary missing quote: %q", summary)
	}
}

func TestBuildHomeTimelineSummaryIncludesAuthorsAndQuotes(t *testing.T) {
	posts := []XPost{
		{
			ID:        "19001",
			Text:      "OpenAI launched another coding workflow update",
			CreatedAt: time.Date(2026, 3, 12, 8, 0, 0, 0, time.UTC),
			URL:       "https://x.com/aliceai/status/19001",
			Author:    XUser{Name: "Alice", ScreenName: "aliceai"},
		},
		{
			ID:        "19002",
			Text:      "MCP adapters are becoming a standard layer for tool calling",
			CreatedAt: time.Date(2026, 3, 12, 8, 5, 0, 0, time.UTC),
			URL:       "https://x.com/builderbob/status/19002",
			Author:    XUser{Name: "Bob", ScreenName: "builderbob"},
		},
	}

	summary, users := BuildHomeTimelineSummary(posts, 2)
	if len(users) != 2 {
		t.Fatalf("expected 2 related users, got %d", len(users))
	}
	if !strings.Contains(summary, "X home timeline") {
		t.Fatalf("summary missing home timeline label: %q", summary)
	}
	if !strings.Contains(summary, "@aliceai") {
		t.Fatalf("summary missing first author: %q", summary)
	}
	if !strings.Contains(summary, "\"OpenAI launched another coding workflow update\"") {
		t.Fatalf("summary missing quote: %q", summary)
	}
}

func TestConvertDOMCandidatesToPosts(t *testing.T) {
	candidates := []DOMPostCandidate{
		{
			Href:      "/aliceai/status/19001",
			Text:      "MCP on X is getting interesting fast",
			CreatedAt: "2026-03-12T08:00:00.000Z",
			Handle:    "@aliceai",
			Name:      "Alice",
			Replies:   7,
			Reposts:   3,
			Likes:     42,
		},
		{
			Href:      "/builderbob/status/19002/analytics",
			Text:      "Codex plus MCP makes X research much faster",
			CreatedAt: "2026-03-12T08:05:00.000Z",
			Handle:    "@builderbob",
			Name:      "Bob",
			Replies:   1,
			Reposts:   2,
			Likes:     5,
		},
	}

	posts, err := ConvertDOMCandidatesToPosts(candidates, 5)
	if err != nil {
		t.Fatalf("ConvertDOMCandidatesToPosts returned error: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].Author.ScreenName != "aliceai" {
		t.Fatalf("unexpected screen name: %q", posts[0].Author.ScreenName)
	}
	if posts[0].Metrics.Replies != 7 || posts[0].Metrics.Reposts != 3 || posts[0].Metrics.Likes != 42 {
		t.Fatalf("unexpected metrics: %+v", posts[0].Metrics)
	}
	if posts[1].URL != "https://x.com/builderbob/status/19002" {
		t.Fatalf("unexpected url: %q", posts[1].URL)
	}
}

func TestNormalizeTimelineLimitAppliesDefaultsAndMax(t *testing.T) {
	if got := NormalizeTimelineLimit(0); got != 8 {
		t.Fatalf("unexpected default limit: %d", got)
	}
	if got := NormalizeTimelineLimit(99); got != 50 {
		t.Fatalf("unexpected capped limit: %d", got)
	}
}

func TestMergePostsPreservesOrderAndDeduplicates(t *testing.T) {
	base := []XPost{
		{ID: "1", Text: "one", Author: XUser{ScreenName: "a"}},
		{ID: "2", Text: "two", Author: XUser{ScreenName: "b"}},
	}
	next := []XPost{
		{ID: "2", Text: "two-new", Author: XUser{ScreenName: "b"}},
		{ID: "3", Text: "three", Author: XUser{ScreenName: "c"}},
	}

	merged := MergePosts(base, next, 10)
	if len(merged) != 3 {
		t.Fatalf("expected 3 posts, got %d", len(merged))
	}
	if merged[0].ID != "1" || merged[1].ID != "2" || merged[2].ID != "3" {
		t.Fatalf("unexpected order: %+v", merged)
	}
}

func TestComputeSearchPhaseTimeoutsLeavesTimeForFallback(t *testing.T) {
	capture, fallback := ComputeSearchPhaseTimeouts(25 * time.Second)
	if capture != 17*time.Second {
		t.Fatalf("unexpected capture timeout: %s", capture)
	}
	if fallback != 8*time.Second {
		t.Fatalf("unexpected fallback timeout: %s", fallback)
	}
}
