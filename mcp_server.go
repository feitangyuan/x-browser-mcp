package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func InitMCPServer(service *SearchService) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "x-browser-mcp",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "check_login_status",
			Description: "Check whether the maintained local X session is ready. Use this before X search or home timeline reads; no Browser Relay or active X tab is required.",
		},
		withPanicRecovery("check_login_status", func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, LoginStatusResponse, error) {
			status, err := service.CheckLoginStatus(ctx)
			if err != nil {
				return errorResult(err), LoginStatusResponse{}, nil
			}
			return textResult(formatJSON(status)), *status, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "start_login",
			Description: "Open a visible browser window so the user can log into X manually for the maintained local session. Use this only when the local X session is missing; no Browser Relay is required.",
		},
		withPanicRecovery("start_login", func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, StartLoginResponse, error) {
			resp, err := service.StartLogin(ctx)
			if err != nil {
				return errorResult(err), StartLoginResponse{}, nil
			}
			return textResult(resp.Message), *resp, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "read_home_timeline",
			Description: "Read the X home timeline from the maintained local logged-in session and summarize what people are discussing, without Browser Relay and without requiring an active X tab.",
		},
		withPanicRecovery("read_home_timeline", func(ctx context.Context, _ *mcp.CallToolRequest, args HomeTimelineRequest) (*mcp.CallToolResult, HomeTimelineResult, error) {
			result, err := service.ReadHomeTimeline(ctx, args)
			if err != nil {
				return errorResult(err), HomeTimelineResult{}, nil
			}
			return textResult(renderHomeTimelineText(result)), *result, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "search_x",
			Description: "Search recent X discussions using the maintained local logged-in session and return summary plus representative quotes, without Browser Relay and without requiring an active X tab.",
		},
		withPanicRecovery("search_x", func(ctx context.Context, _ *mcp.CallToolRequest, args SearchRequest) (*mcp.CallToolResult, SearchResult, error) {
			result, err := service.SearchX(ctx, args)
			if err != nil {
				return errorResult(err), SearchResult{}, nil
			}
			return textResult(renderSearchText(result)), *result, nil
		}),
	)

	return server
}

func withPanicRecovery[In, Out any](toolName string, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		defer func() {
			if r := recover(); r != nil {
				panic(fmt.Sprintf("%s panic: %v\n%s", toolName, r, debug.Stack()))
			}
		}()
		return handler(ctx, req, input)
	}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: err.Error()},
		},
	}
}

func renderSearchText(result *SearchResult) string {
	if result == nil {
		return ""
	}
	return result.Summary + "\n\n" + formatJSON(result.Posts)
}

func renderHomeTimelineText(result *HomeTimelineResult) string {
	if result == nil {
		return ""
	}
	return result.Summary + "\n\n" + formatJSON(result.Posts)
}

func formatJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", value)
	}
	return string(data)
}
