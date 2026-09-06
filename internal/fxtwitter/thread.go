package fxtwitter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Conversation contains one upstream page. It is not a complete reply archive.
type Conversation struct {
	Status  *Tweet
	Thread  []*Tweet
	Replies []*Tweet
}

func (c *Client) FetchConversation(ctx context.Context, tweetID string) (Conversation, error) {
	var raw struct {
		Code    int         `json:"code"`
		Status  *rawTweet   `json:"status"`
		Thread  []*rawTweet `json:"thread"`
		Replies []*rawTweet `json:"replies"`
	}
	if err := c.fetchThreadJSON(ctx, "/2/conversation/"+url.PathEscape(tweetID), &raw); err != nil {
		return Conversation{}, err
	}
	status := tweetFromRaw(raw.Status)
	if raw.Code == http.StatusNotFound || (raw.Code == http.StatusOK && status == nil) {
		return Conversation{}, ErrNotFound
	}
	if raw.Code != http.StatusOK {
		return Conversation{}, fmt.Errorf("fxtwitter conversation status %d", raw.Code)
	}
	return Conversation{Status: status, Thread: threadTweets(raw.Thread), Replies: threadTweets(raw.Replies)}, nil
}

func (c *Client) FetchQuotingPosts(ctx context.Context, tweetID string) ([]*Tweet, error) {
	var raw struct {
		Code    int         `json:"code"`
		Results []*rawTweet `json:"results"`
	}
	if err := c.fetchThreadJSON(ctx, "/2/status/"+url.PathEscape(tweetID)+"/quotes?count=20", &raw); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil // The quotes endpoint uses 404 for an empty result page.
		}
		return nil, err
	}
	if raw.Code != http.StatusOK {
		return nil, fmt.Errorf("fxtwitter quotes status %d", raw.Code)
	}
	if len(raw.Results) > 20 {
		raw.Results = raw.Results[:20]
	}
	return threadTweets(raw.Results), nil
}

func threadTweets(raw []*rawTweet) []*Tweet {
	out := make([]*Tweet, 0, len(raw))
	for _, item := range raw {
		if tweet := tweetFromRaw(item); tweet != nil {
			out = append(out, tweet)
		}
	}
	return out
}

func (c *Client) fetchThreadJSON(ctx context.Context, path string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("fxtwitter thread request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fxtwitter thread status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode fxtwitter thread: %w", err)
	}
	return nil
}
