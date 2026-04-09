package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
)

type openAPIContract struct {
	doc    *openapi3.T
	router routers.Router
}

var (
	openAPIContractOnce sync.Once
	openAPIContractDoc  *openAPIContract
	openAPIContractErr  error
)

func TestOpenAPIContractRepresentativeEndpoints(t *testing.T) {
	contract := loadOpenAPIContract(t)
	handler, cfg := newTestHandler(t)

	loginBody := map[string]any{
		"password": "secret",
	}
	loginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", loginBody, nil)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/auth/login", loginBody, nil, loginResponse)
	cookies := loginResponse.Result().Cookies()

	sessionResponse := performJSONRequest(t, handler, http.MethodGet, "/api/session", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/session", nil, cookies, sessionResponse)

	metaResponse := performJSONRequest(t, handler, http.MethodGet, "/api/meta", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/meta", nil, cookies, metaResponse)

	dockerOverviewResponse := performJSONRequest(t, handler, http.MethodGet, "/api/docker/admin/overview", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/docker/admin/overview", nil, cookies, dockerOverviewResponse)

	dockerConfigResponse := performJSONRequest(t, handler, http.MethodGet, "/api/docker/admin/daemon-config", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/docker/admin/daemon-config", nil, cookies, dockerConfigResponse)

	dockerValidateBody := map[string]any{
		"settings": map[string]any{
			"dns": []string{"192.168.1.2"},
		},
	}
	dockerValidateResponse := performJSONRequest(t, handler, http.MethodPost, "/api/docker/admin/daemon-config/validate", dockerValidateBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/docker/admin/daemon-config/validate", dockerValidateBody, cookies, dockerValidateResponse)

	configRoot := filepath.Join(cfg.RootDir, "config")
	if err := os.MkdirAll(filepath.Join(configRoot, "nextcloud"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config nextcloud) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "nextcloud", "app.conf"), []byte("PORT=8080\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config app.conf) error = %v", err)
	}

	configTreeResponse := performJSONRequest(t, handler, http.MethodGet, "/api/config/workspace/tree", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/config/workspace/tree", nil, cookies, configTreeResponse)

	configFileResponse := performJSONRequest(t, handler, http.MethodGet, "/api/config/workspace/file?path=nextcloud%2Fapp.conf", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/config/workspace/file?path=nextcloud%2Fapp.conf", nil, cookies, configFileResponse)

	configSaveBody := map[string]any{
		"path":                      "nextcloud/app.conf",
		"content":                   "PORT=9090\n",
		"create_parent_directories": false,
	}
	configSaveResponse := performJSONRequest(t, handler, http.MethodPut, "/api/config/workspace/file", configSaveBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPut, "/api/config/workspace/file", configSaveBody, cookies, configSaveResponse)

	runGit(t, cfg.RootDir, "init", "-b", "main")
	runGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.com")
	runGit(t, cfg.RootDir, "add", ".")
	runGit(t, cfg.RootDir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(configRoot, "nextcloud", "app.conf"), []byte("PORT=9091\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated config app.conf) error = %v", err)
	}

	gitStatusResponse := performJSONRequest(t, handler, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/git/workspace/status", nil, cookies, gitStatusResponse)

	gitDiffResponse := performJSONRequest(t, handler, http.MethodGet, "/api/git/workspace/diff?path=config%2Fnextcloud%2Fapp.conf", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/git/workspace/diff?path=config%2Fnextcloud%2Fapp.conf", nil, cookies, gitDiffResponse)

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.CommandContext(context.Background(), "git", "init", "--bare", remoteDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v\n%s", err, string(output))
	}
	runGit(t, cfg.RootDir, "remote", "add", "origin", remoteDir)
	runGit(t, cfg.RootDir, "push", "-u", "origin", "main")

	gitCommitBody := map[string]any{
		"message": "Update nextcloud config",
		"paths":   []string{"config/nextcloud/app.conf"},
	}
	gitCommitResponse := performJSONRequest(t, handler, http.MethodPost, "/api/git/workspace/commit", gitCommitBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/git/workspace/commit", gitCommitBody, cookies, gitCommitResponse)

	gitPushResponse := performJSONRequest(t, handler, http.MethodPost, "/api/git/workspace/push", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/git/workspace/push", nil, cookies, gitPushResponse)

	stackID := "contract-stack"
	createBody := map[string]any{
		"stack_id":            stackID,
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n",
		"env":                 "",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": false,
	}
	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", createBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/stacks", createBody, cookies, createResponse)

	var createPayload struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	decodeResponse(t, createResponse, &createPayload)

	stacksResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/stacks", nil, cookies, stacksResponse)

	stackDetailResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID, nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/stacks/"+stackID, nil, cookies, stackDetailResponse)

	definitionResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID+"/definition", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/stacks/"+stackID+"/definition", nil, cookies, definitionResponse)

	resolvedBody := map[string]any{
		"compose_yaml": "services:\n  app:\n    image: nginx:stable-alpine3.21\n",
		"env":          "",
	}
	resolvedResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/resolved-config", resolvedBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/stacks/"+stackID+"/resolved-config", resolvedBody, cookies, resolvedResponse)

	saveBody := map[string]any{
		"compose_yaml":        "services:\n  app:\n    image: nginx:stable-alpine3.21\n",
		"env":                 "",
		"validate_after_save": true,
	}
	saveResponse := performJSONRequest(t, handler, http.MethodPut, "/api/stacks/"+stackID+"/definition", saveBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPut, "/api/stacks/"+stackID+"/definition", saveBody, cookies, saveResponse)

	jobStore, err := store.Open(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("store.Open(jobStore) error = %v", err)
	}
	t.Cleanup(func() {
		if err := jobStore.Close(); err != nil {
			t.Fatalf("jobStore.Close() error = %v", err)
		}
	})
	jobService := jobs.NewService(jobStore)

	activeJob, err := jobService.Start(context.Background(), "", "update_stacks", "local")
	if err != nil {
		t.Fatalf("jobs.Start(active) error = %v", err)
	}
	activeWorkflow := []store.JobWorkflowStep{
		{Action: "pull", State: "running", TargetStackID: stackID},
		{Action: "up", State: "queued", TargetStackID: stackID},
	}
	activeJob, err = jobService.UpdateWorkflow(context.Background(), activeJob, activeWorkflow)
	if err != nil {
		t.Fatalf("jobs.UpdateWorkflow(active) error = %v", err)
	}
	if err := jobService.PublishEvent(context.Background(), activeJob, "job_step_started", "Starting pull.", "", &store.JobEventStep{
		Index:         1,
		Total:         2,
		Action:        "pull",
		TargetStackID: stackID,
	}); err != nil {
		t.Fatalf("jobs.PublishEvent(active) error = %v", err)
	}

	activeJobsResponse := performJSONRequest(t, handler, http.MethodGet, "/api/jobs/active", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/jobs/active", nil, cookies, activeJobsResponse)

	jobResponse := performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+createPayload.Job.ID, nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/jobs/"+createPayload.Job.ID, nil, cookies, jobResponse)

	stackAuditResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID+"/audit", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/stacks/"+stackID+"/audit", nil, cookies, stackAuditResponse)

	auditResponse := performJSONRequest(t, handler, http.MethodGet, "/api/audit", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/audit", nil, cookies, auditResponse)

	stacks.ResetComposeCLICacheForTests()
	t.Cleanup(stacks.ResetComposeCLICacheForTests)
	shimDir := t.TempDir()
	writeTestDockerShim(t, filepath.Join(shimDir, "docker"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	maintenanceBody := map[string]any{
		"target": map[string]any{
			"mode":      "selected",
			"stack_ids": []string{stackID},
		},
		"options": map[string]any{
			"pull_images":    true,
			"build_images":   true,
			"remove_orphans": true,
			"prune_after": map[string]any{
				"enabled":         false,
				"include_volumes": false,
			},
		},
	}
	maintenanceImagesResponse := performJSONRequest(t, handler, http.MethodGet, "/api/maintenance/images?usage=all&origin=all", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/maintenance/images?usage=all&origin=all", nil, cookies, maintenanceImagesResponse)

	maintenanceNetworksResponse := performJSONRequest(t, handler, http.MethodGet, "/api/maintenance/networks?usage=all&origin=all", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/maintenance/networks?usage=all&origin=all", nil, cookies, maintenanceNetworksResponse)

	maintenanceVolumesResponse := performJSONRequest(t, handler, http.MethodGet, "/api/maintenance/volumes?usage=all&origin=all", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/maintenance/volumes?usage=all&origin=all", nil, cookies, maintenanceVolumesResponse)

	prunePreviewResponse := performJSONRequest(t, handler, http.MethodGet, "/api/maintenance/prune-preview?images=true&build_cache=true&stopped_containers=true&volumes=false", nil, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodGet, "/api/maintenance/prune-preview?images=true&build_cache=true&stopped_containers=true&volumes=false", nil, cookies, prunePreviewResponse)

	maintenanceResponse := performJSONRequest(t, handler, http.MethodPost, "/api/maintenance/update-stacks", maintenanceBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/maintenance/update-stacks", maintenanceBody, cookies, maintenanceResponse)

	pruneBody := map[string]any{
		"scope": map[string]any{
			"images":             true,
			"build_cache":        true,
			"stopped_containers": true,
			"volumes":            false,
		},
	}
	pruneResponse := performJSONRequest(t, handler, http.MethodPost, "/api/maintenance/prune", pruneBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodPost, "/api/maintenance/prune", pruneBody, cookies, pruneResponse)

	deleteBody := map[string]any{
		"remove_runtime":    false,
		"remove_definition": true,
		"remove_config":     false,
		"remove_data":       false,
	}
	deleteResponse := performJSONRequest(t, handler, http.MethodDelete, "/api/stacks/"+stackID, deleteBody, cookies)
	assertResponseMatchesOpenAPI(t, contract, http.MethodDelete, "/api/stacks/"+stackID, deleteBody, cookies, deleteResponse)
}

func loadOpenAPIContract(t *testing.T) *openAPIContract {
	t.Helper()

	openAPIContractOnce.Do(func() {
		loader := openapi3.NewLoader()
		specPath := filepath.Join("..", "..", "docs", "api", "openapi.yaml")
		rawSpec, err := os.ReadFile(specPath)
		if err != nil {
			openAPIContractErr = err
			return
		}
		// kin-openapi still rejects OpenAPI 3.1 `type: 'null'` in this schema shape.
		// It also rejects `const`, so we sanitize only those constructs locally for
		// contract tests instead of mutating the source OpenAPI document.
		sanitizedSpec := strings.ReplaceAll(string(rawSpec), "type: 'null'", "type: object")
		sanitizedSpec = strings.ReplaceAll(sanitizedSpec, "const: true", "enum: [true]")
		sanitizedSpec = strings.ReplaceAll(sanitizedSpec, "const: false", "enum: [false]")
		sanitizedSpec = strings.ReplaceAll(sanitizedSpec, "const: ok", "enum: [ok]")
		sanitizedSpec = strings.ReplaceAll(sanitizedSpec, "  summary: Compose-first homelab control panel API", "  description: Compose-first homelab control panel API")
		doc, err := loader.LoadFromData([]byte(sanitizedSpec))
		if err != nil {
			openAPIContractErr = err
			return
		}
		doc.Servers = nil
		router, err := legacyrouter.NewRouter(doc)
		if err != nil {
			openAPIContractErr = err
			return
		}
		openAPIContractDoc = &openAPIContract{
			doc:    doc,
			router: router,
		}
	})

	if openAPIContractErr != nil {
		t.Fatalf("load OpenAPI contract: %v", openAPIContractErr)
	}
	return openAPIContractDoc
}

func assertResponseMatchesOpenAPI(t *testing.T, contract *openAPIContract, method, path string, requestBody any, cookies []*http.Cookie, response *httptest.ResponseRecorder) {
	t.Helper()

	request := newOpenAPIValidationRequest(t, method, contractPath(path), requestBody, cookies)
	route, pathParams, err := contract.router.FindRoute(request)
	if err != nil {
		t.Fatalf("FindRoute(%s %s) error = %v", method, path, err)
	}

	input := &openapi3filter.RequestValidationInput{
		Request:    request,
		PathParams: pathParams,
		Route:      route,
	}

	responseInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 response.Code,
		Header:                 response.Result().Header,
	}
	responseInput.SetBodyBytes(response.Body.Bytes())

	if err := openapi3filter.ValidateResponse(context.Background(), responseInput); err != nil {
		t.Fatalf("OpenAPI response validation failed for %s %s: %v; body=%s", method, path, err, response.Body.String())
	}
}

func newOpenAPIValidationRequest(t *testing.T, method, path string, body any, cookies []*http.Cookie) *http.Request {
	t.Helper()

	var requestBody []byte
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal(validation request body) error = %v", err)
		}
		requestBody = payload
	}

	request := httptest.NewRequest(method, "http://stacklab.test"+path, bytes.NewReader(requestBody))
	request.Header.Set("Origin", "http://stacklab.test")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	return request
}

func contractPath(path string) string {
	if strings.HasPrefix(path, "/api/") {
		return strings.TrimPrefix(path, "/api")
	}
	if path == "/api" {
		return "/"
	}
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_PAGER=cat", "TERM=dumb")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
