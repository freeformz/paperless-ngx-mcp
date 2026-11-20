package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Polling schedule and limits for waiting on paperless-ngx tasks. Variables so
// tests can shrink them.
var (
	taskPollInitial   = time.Second
	taskPollMax       = 5 * time.Second
	taskPollGrowth    = 1.5
	taskNotFoundGrace = 5 * time.Second
)

const (
	defaultTaskWaitTimeout = 60 * time.Second
	maxTaskWaitTimeout     = 600 * time.Second
)

// taskTerminalStatuses are the statuses after which a task no longer changes.
var taskTerminalStatuses = []string{"success", "failure", "revoked"}

// taskIDArg reads a task id argument: a task UUID, or an integer task id given
// as a string or a JSON number.
func taskIDArg(request mcp.CallToolRequest, name string) (string, *mcp.CallToolResult) {
	var id string
	switch v := request.GetArguments()[name].(type) {
	case string:
		id = v
	case float64:
		if v >= 0 && v == float64(int64(v)) {
			id = strconv.FormatInt(int64(v), 10)
		}
	}
	if id == "" {
		return "", errResult(name + " is required: a task UUID or an integer task id")
	}
	if !integerIDPattern.MatchString(id) && !uuidPattern.MatchString(id) {
		return "", errResult(name + " must be a task UUID or an integer task id")
	}
	return id, nil
}

// fetchTask reads one task by UUID or integer id. found is false when a UUID
// matches no task; an unknown integer id is an API error (404).
func fetchTask(ctx context.Context, client *Client, id string) (task map[string]any, raw []byte, found bool, errRes *mcp.CallToolResult) {
	if integerIDPattern.MatchString(id) {
		path := "/api/tasks/" + id + "/"
		resp, err := client.Get(ctx, path, nil)
		body, errRes := doRequestBody(resp, err, "GET", path)
		if errRes != nil {
			return nil, nil, false, errRes
		}
		if err := json.Unmarshal(body, &task); err != nil {
			return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", path, err))
		}
		return task, body, true, nil
	}

	// Celery task ids are lower case and the task_id filter is an exact,
	// case-sensitive match.
	id = strings.ToLower(id)
	path := "/api/tasks/"
	params := url.Values{"task_id": {id}}
	endpoint := path + "?" + params.Encode()
	resp, err := client.Get(ctx, path, params)
	body, errRes := doRequestBody(resp, err, "GET", endpoint)
	if errRes != nil {
		return nil, nil, false, errRes
	}
	var envelope struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", endpoint, err))
	}
	if len(envelope.Results) == 0 {
		return nil, nil, false, nil
	}
	if err := json.Unmarshal(envelope.Results[0], &task); err != nil {
		return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", endpoint, err))
	}
	return task, envelope.Results[0], true, nil
}

// getWaitTimeout reads the timeout argument in seconds.
func getWaitTimeout(request mcp.CallToolRequest) (time.Duration, *mcp.CallToolResult) {
	v, ok := request.GetArguments()["timeout"]
	if !ok || v == nil {
		return defaultTaskWaitTimeout, nil
	}
	secs := request.GetFloat("timeout", 0)
	if secs <= 0 || secs > maxTaskWaitTimeout.Seconds() || math.IsNaN(secs) {
		return 0, errResult(fmt.Sprintf("timeout must be more than 0 and at most %d seconds", int(maxTaskWaitTimeout.Seconds())))
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// getWaitOptions reads wait and timeout for tools that start a task. A timeout
// without wait is an error, so it isn't silently ignored.
func getWaitOptions(request mcp.CallToolRequest) (wait bool, timeout time.Duration, errRes *mcp.CallToolResult) {
	wait = request.GetBool("wait", false)
	if v, ok := request.GetArguments()["timeout"]; ok && v != nil && !wait {
		return false, 0, errResult("timeout only applies with wait: true")
	}
	if !wait {
		return false, 0, nil
	}
	timeout, errRes = getWaitTimeout(request)
	return wait, timeout, errRes
}

// waitToolOptions are the schema options for tools that start a task.
func waitToolOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithBoolean("wait", mcp.Description("Wait for the queued task to finish and return its outcome (default: false, return the task id at once). The MCP host may end a long call before the timeout; task_wait with the same task id continues.")),
		withNumber("timeout", mcp.Description(fmt.Sprintf("With wait: seconds to wait, 1 to %d (default: %d)", int(maxTaskWaitTimeout.Seconds()), int(defaultTaskWaitTimeout.Seconds())))),
	}
}

// progressReporter returns a function that sends MCP progress notifications
// while waiting, or nil when the client didn't ask for progress.
func progressReporter(ctx context.Context, request mcp.CallToolRequest, timeout time.Duration) func(status string, elapsed time.Duration) {
	meta := request.Params.Meta
	if meta == nil || meta.ProgressToken == nil {
		return nil
	}
	srv := server.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	token := meta.ProgressToken
	return func(status string, elapsed time.Duration) {
		// Best effort: a dropped notification only loses a progress update.
		_ = srv.SendNotificationToClient(ctx, "notifications/progress", map[string]any{
			"progressToken": token,
			"progress":      elapsed.Seconds(),
			"total":         timeout.Seconds(),
			"message":       "task status: " + status,
		})
	}
}

// waitForTask polls a task until it reaches a terminal status, the timeout
// passes or ctx is done. grace is how long a task that can't be found yet is
// treated as about to appear; 0 makes an unknown task an error at once.
// The outcome has task, waited_seconds and, when time ran out, timed_out.
func waitForTask(ctx context.Context, client *Client, id string, timeout, grace time.Duration, progress func(string, time.Duration)) (map[string]any, *mcp.CallToolResult) {
	start := time.Now()
	deadline := start.Add(timeout)
	interval := taskPollInitial
	for {
		task, _, found, errRes := fetchTask(ctx, client, id)
		if errRes != nil {
			return nil, errRes
		}
		elapsed := time.Since(start)
		outcome := map[string]any{"task": task, "waited_seconds": math.Round(elapsed.Seconds()*10) / 10}
		if !found {
			if elapsed >= grace {
				return nil, errResult("no task with task_id " + strings.ToLower(id))
			}
		} else {
			status, _ := task["status"].(string)
			if slices.Contains(taskTerminalStatuses, status) {
				return outcome, nil
			}
			if progress != nil {
				progress(status, elapsed)
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if !found {
				return nil, errResult("no task with task_id " + strings.ToLower(id))
			}
			outcome["timed_out"] = true
			return outcome, nil
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errResult(fmt.Sprintf("stopped waiting for task %s: %s", id, ctx.Err()))
		case <-timer.C:
		}
		interval = nextPollInterval(interval)
	}
}

// taskWaitResult renders a wait outcome. A failed or revoked task is a tool
// error, so it can't be mistaken for success; running out of time is not.
func taskWaitResult(outcome map[string]any) (*mcp.CallToolResult, error) {
	result, err := jsonResult(outcome)
	if err != nil {
		return result, err
	}
	if task, ok := outcome["task"].(map[string]any); ok {
		if status, _ := task["status"].(string); status == "failure" || status == "revoked" {
			result.IsError = true
		}
	}
	return result, nil
}

// respondWithWait finishes a tool that starts a task. Without wait it returns
// the API response as before. With wait it reads the task id from the
// response, waits, and returns the outcome with the original response under
// "response". followOn may add a follow_on hint from the task record.
func respondWithWait(ctx context.Context, client *Client, request mcp.CallToolRequest, wait bool, timeout time.Duration,
	resp *httpResponse, err error, method, path string,
	followOn func(task map[string]any) map[string]any,
) (*mcp.CallToolResult, error) {
	if !wait {
		return doRequest(resp, err, method, path)
	}
	body, errRes := doRequestBody(resp, err, method, path)
	if errRes != nil {
		return errRes, nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return rawJSONResult(body)
	}
	id := taskIDFromResponse(v)
	if !uuidPattern.MatchString(id) {
		// Nothing to wait for; return the response as it is.
		return rawJSONResult(body)
	}
	outcome, errRes := waitForTask(ctx, client, id, timeout, taskNotFoundGrace, progressReporter(ctx, request, timeout))
	if errRes != nil {
		// The request itself succeeded: keep its response next to the error.
		msg := resultMessage(errRes)
		res, jerr := jsonResult(map[string]any{"response": v, "task_id": id, "wait_error": msg})
		if res != nil {
			res.IsError = true
		}
		return res, jerr
	}
	outcome["response"] = v
	if followOn != nil {
		if task, ok := outcome["task"].(map[string]any); ok {
			if hint := followOn(task); hint != nil {
				outcome["follow_on"] = hint
			}
		}
	}
	return taskWaitResult(outcome)
}

// nextPollInterval grows the polling interval by taskPollGrowth, up to
// taskPollMax.
func nextPollInterval(cur time.Duration) time.Duration {
	return min(time.Duration(float64(cur)*taskPollGrowth), taskPollMax)
}

// resultMessage returns the text of a result's first text content.
func resultMessage(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// taskIDFromResponse finds the task id in a task-starting response: the
// task_id field, or the bare string document uploads return.
func taskIDFromResponse(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		s, _ := v["task_id"].(string)
		return s
	}
	return ""
}

func handleTaskWait(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := taskIDArg(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		timeout, errRes := getWaitTimeout(request)
		if errRes != nil {
			return errRes, nil
		}
		outcome, errRes := waitForTask(ctx, client, id, timeout, 0, progressReporter(ctx, request, timeout))
		if errRes != nil {
			return errRes, nil
		}
		return taskWaitResult(outcome)
	}
}
