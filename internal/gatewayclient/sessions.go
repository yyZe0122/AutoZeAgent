package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type sessionListResponse struct {
	Sessions []Session `json:"sessions"`
}

type messageListResponse struct {
	Messages []TranscriptMessage `json:"messages"`
}

func (c *Client) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	path := "/v1/sessions"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var response sessionListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return response.Sessions, nil
}

func (c *Client) GetSession(ctx context.Context, id SessionID) (Session, error) {
	var session Session
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(string(id)), nil, &session); err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

func (c *Client) SessionMessages(ctx context.Context, id SessionID, limit int) ([]TranscriptMessage, error) {
	path := "/v1/sessions/" + url.PathEscape(string(id)) + "/messages"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var response messageListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("session messages: %w", err)
	}
	return response.Messages, nil
}

func (c *Client) TaskMessages(ctx context.Context, id TaskID, limit int) ([]TranscriptMessage, error) {
	path := "/v1/tasks/" + url.PathEscape(string(id)) + "/messages"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var response messageListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("task messages: %w", err)
	}
	return response.Messages, nil
}
