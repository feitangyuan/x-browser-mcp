package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

type AppServer struct {
	service    *SearchService
	mcpServer  *mcp.Server
	httpServer *http.Server
}

func NewAppServer(service *SearchService) *AppServer {
	return &AppServer{
		service:   service,
		mcpServer: InitMCPServer(service),
	}
}

func (s *AppServer) Start(port string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/api/v1/login/status", s.loginStatusHandler)
	mux.HandleFunc("/api/v1/login/start", s.loginStartHandler)
	mux.HandleFunc("/api/v1/home", s.homeTimelineHandler)
	mux.HandleFunc("/api/v1/search", s.searchHandler)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)

	s.httpServer = &http.Server{
		Addr:              port,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logrus.Infof("x-browser-mcp listening on %s", port)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Error("http server failed")
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}

func (s *AppServer) healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{OK: true})
}

func (s *AppServer) loginStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status, err := s.service.CheckLoginStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true, Data: status})
}

func (s *AppServer) loginStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resp, err := s.service.StartLogin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true, Data: resp})
}

func (s *AppServer) searchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	result, err := s.service.SearchX(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNotLoggedIn) {
			status = http.StatusPreconditionFailed
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true, Data: result})
}

func (s *AppServer) homeTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req := HomeTimelineRequest{}
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		var err error
		req.Limit, err = strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
	}

	result, err := s.service.ReadHomeTimeline(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNotLoggedIn) {
			status = http.StatusPreconditionFailed
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true, Data: result})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logrus.Infof("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
