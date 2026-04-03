package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/service"
	"github.com/omeyang/xkit/pkg/context/xtenant"
)

// e2eServer 构建一个完整的 HTTP 测试服务器，包含完整中间件链和内存仓储。
func e2eServer(t *testing.T) (*httptest.Server, *e2eRepos) {
	t.Helper()
	repos := newE2ERepos()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	services := &Services{
		Contacts:  service.NewContactSvc(repos.contacts),
		Templates: service.NewTemplateSvc(repos.templates),
		Tasks:     service.NewTaskSvc(repos.tasks),
		Calls:     service.NewCallSvc(repos.calls),
	}

	handler := Router(logger, services)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, repos
}

// doReq 是测试辅助函数，创建带 context 的请求并执行。
func doReq(t *testing.T, client *http.Client, method, url string, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}
	ctx, _ := xtenant.WithTenantID(context.Background(), "test-tenant")
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// TestE2E_HealthCheck 验证健康检查端点在完整服务栈下工作。
func TestE2E_HealthCheck(t *testing.T) {
	srv, _ := e2eServer(t)

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/healthz", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

// TestE2E_ContactCRUD 端到端测试联系人的创建、查询、列表、状态更新完整流程。
func TestE2E_ContactCRUD(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	// 1. 创建联系人。
	createBody := `{"phone_masked":"138****5678","phone_hash":"hash456","source":"api","profile_json":{"name":"李四"}}`
	resp := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/contacts", createBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created schema.ContactResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, "138****5678", created.PhoneMasked)
	assert.Equal(t, "new", created.CurrentStatus)
	assert.True(t, created.ID > 0)

	// 2. 按 ID 查询。
	resp2 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/contacts/%d", srv.URL, created.ID), "")
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var fetched schema.ContactResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&fetched))
	assert.Equal(t, created.ID, fetched.ID)

	// 3. 列表查询。
	resp3 := doReq(t, client, http.MethodGet, srv.URL+"/api/v1/contacts?offset=0&limit=10", "")
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	var list schema.ListResponse[schema.ContactResponse]
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&list))
	assert.Equal(t, 1, list.Total)
	assert.Len(t, list.Items, 1)

	// 4. 更新状态。
	resp4 := doReq(t, client, http.MethodPatch, fmt.Sprintf("%s/api/v1/contacts/%d/status", srv.URL, created.ID), `{"status":"contacted"}`)
	defer resp4.Body.Close()
	assert.Equal(t, http.StatusOK, resp4.StatusCode)

	// 5. 验证状态已更新。
	resp5 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/contacts/%d", srv.URL, created.ID), "")
	defer resp5.Body.Close()
	var updated schema.ContactResponse
	require.NoError(t, json.NewDecoder(resp5.Body).Decode(&updated))
	assert.Equal(t, "contacted", updated.CurrentStatus)
}

// TestE2E_ContactBulkCreate 端到端测试联系人批量创建。
func TestE2E_ContactBulkCreate(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	body := `{"contacts":[
		{"phone_masked":"138****0001","phone_hash":"h1","source":"import"},
		{"phone_masked":"138****0002","phone_hash":"h2","source":"import"},
		{"phone_masked":"138****0003","phone_hash":"h3","source":"import"}
	]}`
	resp := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/contacts/bulk", body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var result map[string]int
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 3, result["inserted"])
}

// TestE2E_TemplateCRUD 端到端测试模板的完整生命周期：创建→更新→发布→查快照。
func TestE2E_TemplateCRUD(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	// 1. 创建模板。
	createBody := `{
		"name":"销售话术v1",
		"domain":"sales",
		"opening_script":"您好，我是XX公司的小李",
		"state_machine_config":{"states":["opening"]},
		"extraction_schema":{"fields":["company"]},
		"grading_rules":{},
		"prompt_templates":{},
		"notification_config":{},
		"call_protection_config":{},
		"precompiled_audios":{}
	}`
	resp := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/templates", createBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var tmpl schema.TemplateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tmpl))
	assert.Equal(t, "销售话术v1", tmpl.Name)
	assert.Equal(t, "draft", tmpl.Status)
	assert.Equal(t, 1, tmpl.Version)

	// 2. 更新模板。
	updateBody := `{
		"name":"销售话术v2",
		"domain":"sales",
		"opening_script":"您好！很高兴联系到您"
	}`
	resp2 := doReq(t, client, http.MethodPut, fmt.Sprintf("%s/api/v1/templates/%d", srv.URL, tmpl.ID), updateBody)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// 3. 发布模板。
	resp3 := doReq(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/templates/%d/publish", srv.URL, tmpl.ID), "")
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusCreated, resp3.StatusCode)

	var snap schema.SnapshotResponse
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&snap))
	assert.Equal(t, tmpl.ID, snap.TemplateID)
	assert.True(t, snap.ID > 0)

	// 4. 查快照。
	resp4 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/templates/snapshots/%d", srv.URL, snap.ID), "")
	defer resp4.Body.Close()
	assert.Equal(t, http.StatusOK, resp4.StatusCode)

	var fetchedSnap schema.SnapshotResponse
	require.NoError(t, json.NewDecoder(resp4.Body).Decode(&fetchedSnap))
	assert.Equal(t, snap.ID, fetchedSnap.ID)
}

// TestE2E_TaskLifecycle 端到端测试任务的完整生命周期：创建→启动→暂停→取消。
func TestE2E_TaskLifecycle(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	// 先创建模板（任务依赖模板 ID）。
	resp := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/templates", `{"name":"模板","domain":"test","opening_script":"hi"}`)
	resp.Body.Close()

	// 1. 创建任务。
	taskBody := `{
		"name":"测试外呼任务",
		"scenario_template_id":1,
		"daily_limit":100,
		"max_concurrent":5
	}`
	resp2 := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/tasks", taskBody)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusCreated, resp2.StatusCode)

	var task schema.TaskResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&task))
	assert.Equal(t, "测试外呼任务", task.Name)
	assert.Equal(t, "draft", task.Status)

	// 2. 启动任务。
	resp3 := doReq(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/tasks/%d/start", srv.URL, task.ID), "")
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	// 验证状态变为 running。
	resp4 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/tasks/%d", srv.URL, task.ID), "")
	defer resp4.Body.Close()
	var running schema.TaskResponse
	require.NoError(t, json.NewDecoder(resp4.Body).Decode(&running))
	assert.Equal(t, "running", running.Status)

	// 3. 暂停任务。
	resp5 := doReq(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/tasks/%d/pause", srv.URL, task.ID), "")
	defer resp5.Body.Close()
	assert.Equal(t, http.StatusOK, resp5.StatusCode)

	// 4. 取消任务。
	resp6 := doReq(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/tasks/%d/cancel", srv.URL, task.ID), "")
	defer resp6.Body.Close()
	assert.Equal(t, http.StatusOK, resp6.StatusCode)

	// 验证最终状态。
	resp7 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/tasks/%d", srv.URL, task.ID), "")
	defer resp7.Body.Close()
	var cancelled schema.TaskResponse
	require.NoError(t, json.NewDecoder(resp7.Body).Decode(&cancelled))
	assert.Equal(t, "cancelled", cancelled.Status)
}

// TestE2E_TaskList 测试任务列表分页。
func TestE2E_TaskList(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	// 创建多个任务。
	for i := range 3 {
		body := fmt.Sprintf(`{"name":"任务%d","scenario_template_id":1}`, i+1)
		resp := doReq(t, client, http.MethodPost, srv.URL+"/api/v1/tasks", body)
		resp.Body.Close()
	}

	resp := doReq(t, client, http.MethodGet, srv.URL+"/api/v1/tasks?offset=0&limit=2", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var list schema.ListResponse[schema.TaskResponse]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	assert.Equal(t, 3, list.Total)
	assert.Len(t, list.Items, 2)
	assert.Equal(t, 2, list.Limit)
}

// TestE2E_CallReadFlow 端到端测试通话记录的查询流程。
func TestE2E_CallReadFlow(t *testing.T) {
	srv, repos := e2eServer(t)
	client := srv.Client()

	// 预置通话数据（通话是只读资源，通过 worker 创建）。
	repos.seedCallWithTurns()

	// 1. 按任务列表查询通话。
	resp := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/tasks/%d/calls", srv.URL, 1), "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var list schema.ListResponse[schema.CallResponse]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	assert.Equal(t, 1, list.Total)

	// 2. 查通话详情（含对话轮次）。
	callID := list.Items[0].ID
	resp2 := doReq(t, client, http.MethodGet, fmt.Sprintf("%s/api/v1/calls/%d/detail", srv.URL, callID), "")
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var detail schema.CallDetailResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&detail))
	assert.Equal(t, callID, detail.Call.ID)
	assert.Len(t, detail.Turns, 2)
	assert.Equal(t, "bot", detail.Turns[0].Speaker)
	assert.Equal(t, "user", detail.Turns[1].Speaker)
}

// TestE2E_NotFound 验证请求不存在的资源返回 404。
func TestE2E_NotFound(t *testing.T) {
	srv, _ := e2eServer(t)
	client := srv.Client()

	endpoints := []string{
		"/api/v1/contacts/99999",
		"/api/v1/templates/99999",
		"/api/v1/tasks/99999",
		"/api/v1/calls/99999",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp := doReq(t, client, http.MethodGet, srv.URL+ep, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

// TestE2E_InvalidID 验证非法 ID 参数返回 400。
func TestE2E_InvalidID(t *testing.T) {
	srv, _ := e2eServer(t)

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/contacts/abc", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestE2E_InvalidJSON 验证非法 JSON 请求体返回 400。
func TestE2E_InvalidJSON(t *testing.T) {
	srv, _ := e2eServer(t)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/contacts", `{invalid`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestE2E_MissingRequiredField 验证缺少必填字段返回 400。
func TestE2E_MissingRequiredField(t *testing.T) {
	srv, _ := e2eServer(t)

	// 缺少 phone_hash。
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/contacts", `{"phone_masked":"138****0000"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestE2E_MiddlewareChain 验证中间件链正常工作（CORS 头、Request ID）。
func TestE2E_MiddlewareChain(t *testing.T) {
	srv, _ := e2eServer(t)

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/healthz", "")
	defer resp.Body.Close()

	// CORS 头。
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	// Request ID。
	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))
}

// TestE2E_CORSPreflight 验证 OPTIONS 预检请求返回 204。
func TestE2E_CORSPreflight(t *testing.T) {
	srv, _ := e2eServer(t)

	resp := doReq(t, srv.Client(), http.MethodOptions, srv.URL+"/api/v1/contacts", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
