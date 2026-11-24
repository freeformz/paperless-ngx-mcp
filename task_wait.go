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
	"sync"
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
	task, raw, found, errRes, _ = fetchTaskRetryable(ctx, client, id)
	return task, raw, found, errRes
}

// fetchTaskRetryable is fetchTask that also reports whether an error is worth
// polling through: a network error or a 5xx.
func fetchTaskRetryable(ctx context.Context, client *Client, id string) (task map[string]any, raw []byte, found bool, errRes *mcp.CallToolResult, retryable bool) {
	get := func(path string, params url.Values, endpoint string) ([]byte, *mcp.CallToolResult, bool) {
		resp, err := client.Get(ctx, path, params)
		transient := err != nil || resp.StatusCode >= 500
		body, errRes := doRequestBody(resp, err, "GET", endpoint)
		return body, errRes, transient
	}
	if integerIDPattern.MatchString(id) {
		path := "/api/tasks/" + id + "/"
		body, errRes, transient := get(path, nil, path)
		if errRes != nil {
			return nil, nil, false, errRes, transient
		}
		if err := json.Unmarshal(body, &task); err != nil {
			return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", path, err)), false
		}
		return task, body, true, nil, false
	}

	// Celery task ids are lower case and the task_id filter is an exact,
	// case-sensitive match.
	id = strings.ToLower(id)
	path := "/api/tasks/"
	params := url.Values{"task_id": {id}}
	endpoint := path + "?" + params.Encode()
	body, errRes, transient := get(path, params, endpoint)
	if errRes != nil {
		return nil, nil, false, errRes, transient
	}
	var envelope struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", endpoint, err)), false
	}
	if len(envelope.Results) == 0 {
		return nil, nil, false, nil, false
	}
	if err := json.Unmarshal(envelope.Results[0], &task); err != nil {
		return nil, nil, false, errResult(fmt.Sprintf("unexpected response from GET %s: %s", endpoint, err)), false
	}
	return task, envelope.Results[0], true, nil, false
}

// getWaitTimeout reads the timeout argument in seconds.
func getWaitTimeout(request mcp.CallToolRequest) (time.Duration, *mcp.CallToolResult) {
	secs, given, errRes := optionalInt(request, "timeout")
	if !given && errRes == nil {
		return defaultTaskWaitTimeout, nil
	}
	if errRes != nil || secs < 1 || secs > int(maxTaskWaitTimeout.Seconds()) {
		return 0, errResult(fmt.Sprintf("timeout must be a whole number of seconds from 1 to %d", int(maxTaskWaitTimeout.Seconds())))
	}
	return time.Duration(secs) * time.Second, nil
}

// getWaitOptions reads wait and timeout for tools that start a task. A timeout
// without wait is an error, so it isn't silently ignored.
func getWaitOptions(request mcp.CallToolRequest) (wait bool, timeout time.Duration, errRes *mcp.CallToolResult) {
	wait, errRes = optionalBool(request, "wait", false)
	if errRes != nil {
		return false, 0, errRes
	}
	if argIsSet(request.GetArguments()["timeout"]) && !wait {
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
func progressReporter(ctx context.Context, request mcp.CallToolRequest, timeout time.Duration) func(message string, elapsed time.Duration) {
	meta := request.Params.Meta
	if meta == nil || meta.ProgressToken == nil {
		return nil
	}
	srv := server.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	token := meta.ProgressToken
	return func(message string, elapsed time.Duration) {
		// Best effort: a dropped notification only loses a progress update.
		_ = srv.SendNotificationToClient(ctx, "notifications/progress", map[string]any{
			"progressToken": token,
			"progress":      elapsed.Seconds(),
			"total":         timeout.Seconds(),
			"message":       message,
		})
	}
}

// maxTransientPollErrors is how many polls in a row may fail with a network
// error or a 5xx before waiting gives up.
const maxTransientPollErrors = 2

// waitForTask polls a task until it reaches a terminal status, the timeout
// passes or ctx is done. grace is how long a task that can't be found yet is
// treated as about to appear; 0 makes an unknown task an error at once.
// The outcome has task, waited_seconds and, when time ran out, timed_out.
func waitForTask(ctx context.Context, client *Client, id string, timeout, grace time.Duration, progress func(string, time.Duration)) (map[string]any, *mcp.CallToolResult) {
	start := time.Now()
	deadline := start.Add(timeout)
	interval := taskPollInitial
	var last map[string]any // the last task record seen
	transientErrors := 0
	stopped := func() *mcp.CallToolResult {
		return errResult(fmt.Sprintf("stopped waiting for task %s: %s", id, ctx.Err()))
	}
	notFound := func() *mcp.CallToolResult { return errResult("no task with task_id " + strings.ToLower(id)) }
	outcome := func(task map[string]any, timedOut bool) map[string]any {
		o := map[string]any{"task": task, "waited_seconds": math.Round(time.Since(start).Seconds()*10) / 10}
		if timedOut {
			o["timed_out"] = true
		}
		return o
	}
	timedOut := func() (map[string]any, *mcp.CallToolResult) {
		if last == nil {
			return nil, notFound()
		}
		return outcome(last, true), nil
	}

	for {
		// Bound each poll by the deadline, so a hung request can't overrun it.
		pollCtx, cancel := context.WithDeadline(ctx, deadline)
		task, _, found, errRes, retryable := fetchTaskRetryable(pollCtx, client, id)
		pollExpired := pollCtx.Err() != nil
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil, stopped()
		case errRes != nil && pollExpired:
			return timedOut()
		case errRes != nil && retryable && transientErrors < maxTransientPollErrors:
			transientErrors++
		case errRes != nil:
			return nil, errRes
		default:
			transientErrors = 0
			if found {
				last = task
				if status, _ := task["status"].(string); slices.Contains(taskTerminalStatuses, status) {
					return outcome(task, false), nil
				}
			} else if time.Since(start) >= grace {
				return nil, notFound()
			}
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return timedOut()
		}
		// Report progress only while the call goes on, so no notification
		// follows the response.
		if progress != nil && last != nil {
			status, _ := last["status"].(string)
			progress("task status: "+status, time.Since(start))
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, stopped()
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
		return jsonResult(map[string]any{"response": v, "wait_skipped": "the response has no task id to wait for"})
	}
	outcome, errRes := waitForTask(ctx, client, id, timeout, taskNotFoundGrace, progressReporter(ctx, request, timeout))
	if errRes != nil {
		// The request itself succeeded and its task is queued, so this is not
		// a tool error: resending could repeat a merge or an upload.
		return jsonResult(map[string]any{
			"response":   v,
			"task_id":    id,
			"wait_error": resultMessage(errRes),
			"note":       "The request was accepted and its task queued; only waiting for it failed. Do not resend it; call task_wait with task_id to check on it.",
		})
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

// aiProgressInterval is how often progressTicker reports.
var aiProgressInterval = 5 * time.Second

// progressTicker reports progress every aiProgressInterval while a long
// request runs, with message() as the text. stop ends the reports and waits
// for the goroutine, so no notification follows the call's response. It does
// nothing when the call has no progress token.
func progressTicker(ctx context.Context, request mcp.CallToolRequest, total time.Duration, message func() string) (stop func()) {
	report := progressReporter(ctx, request, total)
	if report == nil {
		return func() {}
	}
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(aiProgressInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				// Stop may have been called while the tick was pending.
				select {
				case <-done:
					return
				default:
				}
				report(message(), time.Since(start))
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
}
