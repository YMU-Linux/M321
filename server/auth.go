package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxSessionResponseBytes = 64 << 10

type validatedSession struct {
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

type sessionContextKey struct{}
type sessionResponseContextKey struct{}

func authServiceURLFromEnv() (string, error) {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_SERVICE_URL")), "/")
	if value == "" {
		return "", &configurationError{message: "set AUTH_SERVICE_URL before starting"}
	}

	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", &configurationError{message: "AUTH_SERVICE_URL must be an absolute HTTP or HTTPS URL"}
	}
	return value, nil
}

type configurationError struct {
	message string
}

func (e *configurationError) Error() string { return e.message }

func corsAllowedOrigins() []string {
	value := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if value == "" {
		return []string{"*"}
	}
	origins := strings.Split(value, ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return origins
}

func corsMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		wildcard := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"
		if origin != "" {
			allowed := wildcard
			for _, candidate := range allowedOrigins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
				return
			}
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-ID")
		}

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Correlation-ID")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs" {
			next.ServeHTTP(w, r)
			return
		}

		session, responseBody, status, code, message := h.validateSession(r)
		if status != http.StatusOK {
			writeError(w, status, code, message)
			return
		}

		ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
		ctx = context.WithValue(ctx, sessionResponseContextKey{}, responseBody)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) validateSession(r *http.Request) (validatedSession, []byte, int, string, string) {
	var session validatedSession
	if h.authServiceURL == "" {
		return session, nil, http.StatusServiceUnavailable, "auth_service_unavailable", "authentication service is not configured"
	}
	if r.Header.Get("Authorization") == "" && r.Header.Get("Cookie") == "" {
		return session, nil, http.StatusUnauthorized, "invalid_session", "a valid session is required"
	}

	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.authServiceURL+"/sessions/current", nil)
	if err != nil {
		return session, nil, http.StatusServiceUnavailable, "auth_service_unavailable", "could not reach authentication service"
	}
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		request.Header.Set("Cookie", cookie)
	}

	client := h.authHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return session, nil, http.StatusServiceUnavailable, "auth_service_unavailable", "authentication service is unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			return session, nil, http.StatusUnauthorized, "invalid_session", "invalid or expired session"
		}
		if response.StatusCode == http.StatusForbidden {
			return session, nil, http.StatusForbidden, "request_forbidden", "authentication service denied the request"
		}
		return session, nil, http.StatusServiceUnavailable, "auth_service_unavailable", "authentication service did not validate the session"
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes+1))
	if err != nil || len(body) > maxSessionResponseBytes {
		return session, nil, http.StatusBadGateway, "invalid_auth_response", "authentication service returned an invalid response"
	}
	if err := json.Unmarshal(body, &session); err != nil || strings.TrimSpace(session.User.ID) == "" {
		return session, nil, http.StatusBadGateway, "invalid_auth_response", "authentication service returned an invalid response"
	}
	return session, body, http.StatusOK, "", ""
}

func (h *handler) getCurrentSession(w http.ResponseWriter, r *http.Request) {
	responseBody, _ := r.Context().Value(sessionResponseContextKey{}).([]byte)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)
}

func currentUserID(r *http.Request) string {
	session, _ := r.Context().Value(sessionContextKey{}).(validatedSession)
	return session.User.ID
}
