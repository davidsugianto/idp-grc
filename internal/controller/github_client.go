/*
Copyright 2026 David Sugianto.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const githubAPIBase = "https://api.github.com"

// githubTokenResponse is the GitHub API response for registration token endpoints.
type githubTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// githubRunner represents a runner in GitHub's runner list response.
type githubRunner struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// githubRunnersResponse is the GitHub API response for listing runners.
type githubRunnersResponse struct {
	TotalCount int            `json:"total_count"`
	Runners    []githubRunner `json:"runners"`
}

// GitHubClient wraps GitHub REST API calls needed by the reconciler.
type GitHubClient struct {
	httpClient *http.Client
}

// NewGitHubClient returns a GitHubClient with a 10s timeout.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ParseGitHubURL parses a GitHub URL into (owner, repo, isOrg).
// https://github.com/owner/repo → (owner, repo, false)
// https://github.com/org         → (org, "", true)
func ParseGitHubURL(githubURL string) (owner, repo string, isOrg bool, err error) {
	githubURL = strings.TrimSuffix(githubURL, "/")
	const prefix = "https://github.com/"
	if !strings.HasPrefix(githubURL, prefix) {
		return "", "", false, fmt.Errorf("githubURL must start with https://github.com/, got: %s", githubURL)
	}
	path := strings.TrimPrefix(githubURL, prefix)
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 1 {
		return parts[0], "", true, nil
	}
	return parts[0], parts[1], false, nil
}

// GetRegistrationToken fetches a runner registration token from GitHub.
// It automatically determines repo vs. org scope from githubURL.
func (c *GitHubClient) GetRegistrationToken(ctx context.Context, githubURL, pat string) (string, error) {
	owner, repo, isOrg, err := ParseGitHubURL(githubURL)
	if err != nil {
		return "", err
	}
	if isOrg {
		return c.GetOrgRegistrationToken(ctx, owner, pat)
	}
	return c.GetRepoRegistrationToken(ctx, owner, repo, pat)
}

// GetRepoRegistrationToken fetches a registration token for a repository runner.
func (c *GitHubClient) GetRepoRegistrationToken(ctx context.Context, owner, repo, pat string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runners/registration-token", githubAPIBase, owner, repo)
	return c.postForToken(ctx, url, pat)
}

// GetOrgRegistrationToken fetches a registration token for an organization runner.
func (c *GitHubClient) GetOrgRegistrationToken(ctx context.Context, org, pat string) (string, error) {
	url := fmt.Sprintf("%s/orgs/%s/actions/runners/registration-token", githubAPIBase, org)
	return c.postForToken(ctx, url, pat)
}

func (c *GitHubClient) postForToken(ctx context.Context, url, pat string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		retryAfter := resp.Header.Get("Retry-After")
		return "", &RateLimitError{RetryAfter: retryAfter, StatusCode: resp.StatusCode}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", &AuthError{Message: "GitHub API returned 401 Unauthorized — check PAT scope and validity"}
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %d from GitHub token API: %s", resp.StatusCode, string(body))
	}

	var tokenResp githubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	return tokenResp.Token, nil
}

// FindRunnerID searches for a runner by name and returns its GitHub-assigned ID.
// Returns 0 if the runner is not found (not an error — it may not have registered yet).
func (c *GitHubClient) FindRunnerID(ctx context.Context, githubURL, runnerName, pat string) (int64, error) {
	owner, repo, isOrg, err := ParseGitHubURL(githubURL)
	if err != nil {
		return 0, err
	}

	var url string
	if isOrg {
		url = fmt.Sprintf("%s/orgs/%s/actions/runners", githubAPIBase, owner)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/actions/runners", githubAPIBase, owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("building list runners request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("list runners request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return 0, &AuthError{Message: "GitHub API returned 401 Unauthorized — check PAT scope"}
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d listing runners: %s", resp.StatusCode, string(body))
	}

	var runners githubRunnersResponse
	if err := json.Unmarshal(body, &runners); err != nil {
		return 0, fmt.Errorf("parsing runners list: %w", err)
	}

	for _, r := range runners.Runners {
		if r.Name == runnerName {
			return r.ID, nil
		}
	}
	return 0, nil
}

// DeleteRunner removes a runner from GitHub by ID.
// A 404 response is treated as success (runner already removed).
func (c *GitHubClient) DeleteRunner(ctx context.Context, githubURL string, runnerID int64, pat string) error {
	owner, repo, isOrg, err := ParseGitHubURL(githubURL)
	if err != nil {
		return err
	}

	var url string
	if isOrg {
		url = fmt.Sprintf("%s/orgs/%s/actions/runners/%s", githubAPIBase, owner, strconv.FormatInt(runnerID, 10))
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/actions/runners/%s", githubAPIBase, owner, repo, strconv.FormatInt(runnerID, 10))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("building delete runner request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete runner request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return &AuthError{Message: "GitHub API returned 401 Unauthorized on runner delete"}
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("unexpected status %d deleting runner: %s", resp.StatusCode, string(body))
}

// RateLimitError is returned when GitHub responds with 429 or 403 rate-limit.
type RateLimitError struct {
	RetryAfter string
	StatusCode int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit (status %d), retry-after: %s", e.StatusCode, e.RetryAfter)
}

// AuthError is returned on 401 Unauthorized responses.
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}
