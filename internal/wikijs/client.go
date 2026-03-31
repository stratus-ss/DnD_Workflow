package wikijs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"dnd-workflow/internal/config"
)

type Client struct {
	cfg        config.WikiJSConfig
	token      string
	httpClient *http.Client
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type createPageResponse struct {
	Data struct {
		Pages struct {
			Create struct {
				ResponseResult struct {
					Succeeded bool   `json:"succeeded"`
					ErrorCode int    `json:"errorCode"`
					Message   string `json:"message"`
				} `json:"responseResult"`
				Page struct {
					ID   int    `json:"id"`
					Path string `json:"path"`
				} `json:"page"`
			} `json:"create"`
		} `json:"pages"`
	} `json:"data"`
}

type treeResponse struct {
	Data struct {
		Pages struct {
			Tree []struct {
				ID    int    `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
			} `json:"tree"`
		} `json:"pages"`
	} `json:"data"`
}

func NewClient(cfg config.WikiJSConfig, token string) *Client {
	return &Client{
		cfg:        cfg,
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *Client) CreatePage(ctx context.Context, title, path, content string, tags []string) (int, error) {
	isPublished := true
	if c.cfg.IsPublished != nil {
		isPublished = *c.cfg.IsPublished
	}

	variables := map[string]interface{}{
		"content":     content,
		"description": title,
		"editor":      c.cfg.Editor,
		"isPublished": isPublished,
		"isPrivate":   false,
		"locale":      c.cfg.Locale,
		"path":        path,
		"title":       title,
		"tags":        tags,
	}

	reqBody := graphQLRequest{
		Query:     createPageMutation,
		Variables: variables,
	}

	respBody, err := c.doGraphQL(ctx, reqBody)
	if err != nil {
		return 0, err
	}

	var result createPageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	cr := result.Data.Pages.Create
	if !cr.ResponseResult.Succeeded {
		return 0, fmt.Errorf("create page failed: %s (code %d)", cr.ResponseResult.Message, cr.ResponseResult.ErrorCode)
	}

	return cr.Page.ID, nil
}

func (c *Client) CheckPageExists(ctx context.Context, pagePath string) (bool, error) {
	// pages.tree lists pages under a given parent path; we pass the parent
	// directory and then look for an exact match on the full path.
	parent := parentPath(pagePath)
	reqBody := graphQLRequest{
		Query: pageTreeQuery,
		Variables: map[string]interface{}{
			"path":   parent,
			"locale": c.cfg.Locale,
		},
	}

	respBody, err := c.doGraphQL(ctx, reqBody)
	if err != nil {
		return false, err
	}

	var result treeResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	for _, p := range result.Data.Pages.Tree {
		if p.Path == pagePath {
			return true, nil
		}
	}

	return false, nil
}

func parentPath(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return ""
	}
	return p[:idx]
}

func (c *Client) doGraphQL(ctx context.Context, reqBody graphQLRequest) ([]byte, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL+"/graphql", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

const createPageMutation = `mutation ($content: String!, $description: String!, $editor: String!, $isPublished: Boolean!, $isPrivate: Boolean!, $locale: String!, $path: String!, $title: String!, $tags: [String]!) {
  pages {
    create(content: $content, description: $description, editor: $editor, isPublished: $isPublished, isPrivate: $isPrivate, locale: $locale, path: $path, title: $title, tags: $tags) {
      responseResult {
        succeeded
        errorCode
        message
      }
      page {
        id
        path
        title
      }
    }
  }
}`

const pageTreeQuery = `query ($path: String!, $locale: String!) {
  pages {
    tree(path: $path, mode: ALL, locale: $locale, includeAncestors: false) {
      id
      path
      title
    }
  }
}`
