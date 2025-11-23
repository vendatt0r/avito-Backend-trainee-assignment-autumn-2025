package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var baseURL = detectURL()

func detectURL() string {
	candidates := []string{
		"http://localhost:8080",
		"http://host.docker.internal:8080",
	}

	for _, u := range candidates {
		resp, err := http.Get(u + "/health")
		if err == nil && resp.StatusCode == 200 {
			return u
		}
	}
	return "http://localhost:8080"
}

func waitForServer(t *testing.T) {
	for i := 0; i < 60; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("server did not become ready at " + baseURL)
}

func post(t *testing.T, path string, body interface{}) *http.Response {
	b, _ := json.Marshal(body)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewBuffer(b))
	require.NoError(t, err)
	return resp
}

func get(t *testing.T, path string) *http.Response {
	resp, err := http.Get(baseURL + path)
	require.NoError(t, err)
	return resp
}

func decode(t *testing.T, resp *http.Response, v interface{}) {
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(v))
}

func Test_FullFlow(t *testing.T) {
	waitForServer(t)

	_ = post(t, "/debug/reset", nil)

	resp := post(t, "/team/add", map[string]interface{}{
		"team_name": "test_command",
		"members": []map[string]interface{}{
			{"user_id": "u1", "username": "alice", "is_active": true},
			{"user_id": "u2", "username": "bob", "is_active": true},
			{"user_id": "u3", "username": "charlie", "is_active": true},
			{"user_id": "u4", "username": "vasya", "is_active": true},
		},
	})
	require.Equal(t, 201, resp.StatusCode)

	resp = post(t, "/pullRequest/create", map[string]interface{}{
		"pull_request_id":   "test_pr1",
		"pull_request_name": "New login",
		"author_id":         "u1",
	})
	require.Equal(t, 201, resp.StatusCode)

	var prResp struct {
		PR struct {
			ID        string   `json:"pull_request_id"`
			Reviewers []string `json:"assigned_reviewers"`
		} `json:"pr"`
	}
	decode(t, resp, &prResp)
	require.GreaterOrEqual(t, len(prResp.PR.Reviewers), 1)

	oldReviewer := prResp.PR.Reviewers[0]

	resp = post(t, "/pullRequest/reassign", map[string]string{
		"pull_request_id": "test_pr1",
		"old_user_id":     oldReviewer,
	})
	require.Equal(t, 200, resp.StatusCode)

	var reassigned struct {
		Replaced string `json:"replaced_by"`
	}
	decode(t, resp, &reassigned)
	require.NotEqual(t, oldReviewer, reassigned.Replaced)

	resp = get(t, "/users/getReview?user_id="+reassigned.Replaced)
	require.Equal(t, 200, resp.StatusCode)

	var list struct {
		PullRequests []interface{} `json:"pull_requests"`
	}
	decode(t, resp, &list)
	require.True(t, len(list.PullRequests) > 0)

	resp = post(t, "/pullRequest/merge", map[string]string{
		"pull_request_id": "test_pr1",
	})
	require.Equal(t, 200, resp.StatusCode)

	resp = post(t, "/pullRequest/reassign", map[string]string{
		"pull_request_id": "test_pr1",
		"old_user_id":     reassigned.Replaced,
	})
	require.Equal(t, 409, resp.StatusCode)
}
