package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"github.com/geodispatch/supervisor/internal/models"
)

type Client struct {
	url string
}

func NewClient(url string) *Client {
	return &Client{url: url}
}

func (c *Client) Decide(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
	body, _ := json.Marshal(req)
	resp, err := http.Post(c.url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var agentResp models.AgentResponse
	return &agentResp, json.NewDecoder(resp.Body).Decode(&agentResp)
}