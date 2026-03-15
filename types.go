package main

import (
	"encoding/json"
	"time"
)

type SearchRequest struct {
	Query string `json:"query"`
	Mode  string `json:"mode,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type HomeTimelineRequest struct {
	Limit int `json:"limit,omitempty"`
}

type NormalizedSearchRequest struct {
	Query string
	Mode  string
	Limit int
}

type HomeTimelineResult struct {
	Summary      string        `json:"summary"`
	Posts        []XPost       `json:"posts"`
	RelatedUsers []RelatedUser `json:"related_users"`
	FetchedAt    time.Time     `json:"fetched_at"`
	Cached       bool          `json:"cached"`
}

type SearchResult struct {
	Query        string        `json:"query"`
	Mode         string        `json:"mode"`
	Summary      string        `json:"summary"`
	Posts        []XPost       `json:"posts"`
	RelatedUsers []RelatedUser `json:"related_users"`
	FetchedAt    time.Time     `json:"fetched_at"`
	Cached       bool          `json:"cached"`
}

func (r SearchResult) MarshalJSON() ([]byte, error) {
	type alias SearchResult
	payload := alias(r)
	if payload.Posts == nil {
		payload.Posts = []XPost{}
	}
	if payload.RelatedUsers == nil {
		payload.RelatedUsers = []RelatedUser{}
	}
	return json.Marshal(payload)
}

type RelatedUser struct {
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	PostCount int    `json:"post_count"`
}

type XPost struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url"`
	Author    XUser     `json:"author"`
	Metrics   XMetrics  `json:"metrics"`
}

type XUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ScreenName  string `json:"screen_name"`
	Description string `json:"description,omitempty"`
}

type XMetrics struct {
	Replies int `json:"replies"`
	Reposts int `json:"reposts"`
	Likes   int `json:"likes"`
	Views   int `json:"views,omitempty"`
}

type DOMPostCandidate struct {
	Href      string `json:"href"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	Replies   int    `json:"replies"`
	Reposts   int    `json:"reposts"`
	Likes     int    `json:"likes"`
}

type LoginStatusResponse struct {
	LoggedIn        bool      `json:"logged_in"`
	LoginInProgress bool      `json:"login_in_progress"`
	State           string    `json:"state,omitempty"`
	Username        string    `json:"username,omitempty"`
	SessionFile     string    `json:"session_file"`
	CheckedAt       time.Time `json:"checked_at"`
}

type StartLoginResponse struct {
	Message     string    `json:"message"`
	Deadline    time.Time `json:"deadline"`
	SessionFile string    `json:"session_file"`
}

type HealthResponse struct {
	OK bool `json:"ok"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

func (r HomeTimelineResult) MarshalJSON() ([]byte, error) {
	type alias HomeTimelineResult
	payload := alias(r)
	if payload.Posts == nil {
		payload.Posts = []XPost{}
	}
	if payload.RelatedUsers == nil {
		payload.RelatedUsers = []RelatedUser{}
	}
	return json.Marshal(payload)
}
