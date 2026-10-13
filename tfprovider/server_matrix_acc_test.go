package tfprovider_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// accSuiteAPI records disposable-server tests with their normal cleanup policy.
func accSuiteAPI(t *testing.T, client *gigahost.Client) (*gigahost.Client, *accMatrixRecorder) {
	t.Helper()

	matrixDir := os.Getenv("GIGAHOST_TEST_MATRIX_DIR")
	if matrixDir == "" {
		return client, nil
	}

	dir := filepath.Join(filepath.Dir(matrixDir), "suite", t.Name())
	apiURL, recorder := accMatrixAPI(t, client.BaseURL(), dir, func() bool { return false })
	t.Setenv("GIGAHOST_BASE_URL", apiURL)

	client = testAccGigahostClient(t)
	t.Cleanup(func() {
		if !t.Failed() || os.Getenv("GIGAHOST_TEST_KEEP_FAILED") == "1" {
			return
		}

		// A failed Terraform destroy can leave dependants blocking VM cleanup.
		// Only IDs linked to deployment orders created through this recorder qualify.
		for _, serverID := range recorder.deployedServers() {
			recorder.event("cleanup_backstop_started", map[string]any{"server_id": serverID})

			if err := client.Servers.Cancel(accCtx, serverID); err != nil {
				t.Logf("cleanup backstop for server %s: %v", serverID, err)
			}
		}
	})

	return client, recorder
}

// accMatrixRedact removes credentials from API evidence before sharing it.
func accMatrixRedact(body []byte) []byte {
	var data any
	if json.Unmarshal(body, &data) != nil {
		return []byte(`{"error":"non-JSON response withheld"}`)
	}

	var (
		secrets []string
		scrub   func(any)
	)

	scrub = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, value := range v {
				name := strings.ToLower(key)
				if strings.Contains(name, "password") || strings.Contains(name, "passwd") || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "api_key") || strings.Contains(name, "serverkey") {
					if secret, ok := value.(string); ok && secret != "" {
						secrets = append(secrets, secret)
						v[key] = "[redacted]"

						continue
					}
				}

				scrub(value)
			}
		case []any:
			for _, value := range v {
				scrub(value)
			}
		}
	}
	scrub(data)

	encoded, err := json.Marshal(data)
	if err != nil {
		return []byte(`{"error":"response could not be redacted"}`)
	}

	for _, secret := range secrets {
		quoted, err := json.Marshal(secret)
		if err != nil {
			return []byte(`{"error":"response could not be redacted"}`)
		}

		encoded = bytes.ReplaceAll(encoded, quoted[1:len(quoted)-1], []byte("[redacted]"))
	}

	return encoded
}

// accMatrixRecorder timestamps observations without changing API responses.
type accMatrixRecorder struct {
	t           *testing.T
	dir         string
	started     time.Time
	mu          sync.Mutex
	serverID    atomic.Value
	orders      sync.Map
	deployedIDs sync.Map
}

func (r *accMatrixRecorder) write(name string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.WriteFile(filepath.Join(r.dir, name), body, 0o600); err != nil {
		r.t.Errorf("save matrix evidence: %v", err)
	}
}

func (r *accMatrixRecorder) event(stage string, data any) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	body, err := json.Marshal(struct {
		Stage   string    `json:"stage"`
		At      time.Time `json:"at"`
		Elapsed float64   `json:"elapsed_seconds"`
		Data    any       `json:"data,omitempty"`
	}{stage, now.UTC(), now.Sub(r.started).Seconds(), data})
	if err != nil {
		r.t.Errorf("encode matrix event: %v", err)

		return
	}

	f, err := os.OpenFile(filepath.Join(r.dir, "share", "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		r.t.Errorf("open matrix events: %v", err)

		return
	}
	defer f.Close()

	if _, err := f.Write(append(accMatrixRedact(body), '\n')); err != nil {
		r.t.Errorf("save matrix event: %v", err)
	}
}

func (r *accMatrixRecorder) request(req *http.Request) {
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/cancel") {
		r.event("cleanup_started", nil)
	}

	deploy := req.Method == http.MethodPost && req.URL.Path == "/deploy/servers"
	if !deploy && (req.Method == http.MethodGet || !strings.HasPrefix(req.URL.Path, "/servers/")) {
		return
	}

	var body []byte

	if req.Body != nil {
		var err error

		body, err = io.ReadAll(req.Body)
		req.Body.Close()

		if err != nil {
			r.t.Errorf("read API request: %v", err)
		}

		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	if deploy {
		r.write("share/deploy-request.json", accMatrixRedact(body))
		r.event("deploy_submitted", jsontext.Value(body))

		return
	}

	r.event("api_request", map[string]any{
		"path": req.URL.Path, "method": req.Method, "body": jsontext.Value(accMatrixRedact(body)),
	})
}

func (r *accMatrixRecorder) allocation(body []byte) {
	var status struct {
		Data struct {
			Servers []struct {
				ID      any `json:"srv_id"`
				OrderID any `json:"order_id"`
			} `json:"servers"`
		} `json:"data"`
	}

	if json.Unmarshal(body, &status) != nil {
		return
	}

	for _, server := range status.Data.Servers {
		if server.ID == nil {
			continue
		}

		if _, owned := r.orders.Load(fmt.Sprint(server.OrderID)); !owned {
			continue
		}

		serverID := fmt.Sprint(server.ID)
		if serverID != "" && serverID != "0" {
			r.serverID.Store(serverID)
			r.deployedIDs.Store(serverID, true)
		}
	}
}

func (r *accMatrixRecorder) deployment(body []byte) {
	var response struct {
		Data struct {
			OrderIDs []any `json:"order_ids"`
		} `json:"data"`
	}

	if json.Unmarshal(body, &response) != nil {
		return
	}

	for _, orderID := range response.Data.OrderIDs {
		if orderID != nil {
			r.orders.Store(fmt.Sprint(orderID), true)
		}
	}
}

func (r *accMatrixRecorder) deployedServers() []string {
	var ids []string

	r.deployedIDs.Range(func(key, _ any) bool {
		if id, ok := key.(string); ok {
			ids = append(ids, id)
		}

		return true
	})

	return ids
}

func (r *accMatrixRecorder) observe(path, method string, statusCode int, body []byte) {
	r.event("api_response", map[string]any{
		"path": path, "method": method, "status_code": statusCode, "body": jsontext.Value(accMatrixRedact(body)),
	})

	if path == "/deploy/status" {
		r.allocation(body)
	}

	if path == "/deploy/servers" && statusCode >= 200 && statusCode < 300 {
		r.deployment(body)
	}
}

// The framework always destroys resources. This test-only proxy records the
// actual API inputs and skips cancellation only for opted-in failed cells.
func accMatrixAPI(t *testing.T, baseURL, dir string, keep func() bool) (string, *accMatrixRecorder) {
	t.Helper()

	for _, subdir := range []string{"share", "private"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	recorder := &accMatrixRecorder{t: t, dir: dir, started: time.Now()}
	recorder.event("case_started", nil)

	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}

	recorder.serverID.Store("")

	var cancelled atomic.Bool

	write := recorder.write
	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		r := pr.Out
		recorder.request(r)

		pr.SetURL(target)
		// Let the transport decompress responses before recording JSON.
		r.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		path := strings.TrimPrefix(r.Request.URL.Path, strings.TrimSuffix(target.Path, "/"))
		if r.Request.Method == http.MethodPost && strings.HasSuffix(path, "/cancel") {
			cancelled.Store(r.StatusCode < 300)
			recorder.event("cleanup_finished", map[string]any{"status_code": r.StatusCode})
		}

		if path != "/deploy/servers" && path != "/deploy/status" && !strings.HasPrefix(path, "/servers/") {
			return nil
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()

		if err != nil {
			return err
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		recorder.observe(path, r.Request.Method, r.StatusCode, body)

		if after, ok := strings.CutPrefix(path, "/servers/"); ok && strings.Count(path, "/") == 2 {
			serverID := after
			if recorder.serverID.Load() != serverID {
				t.Logf("server_id=%s", serverID)
				recorder.serverID.Store(serverID)
			}

			write("share/server.json", accMatrixRedact(body))

			var response struct {
				Data []struct {
					InstallDetails jsontext.Value `json:"install_details"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &response) == nil && len(response.Data) > 0 {
				details := response.Data[0].InstallDetails
				if details.Kind() == '{' {
					write("share/installer.json", accMatrixRedact(details))

					var credentials struct {
						Password string `json:"root_password"`
					}
					if json.Unmarshal(details, &credentials) == nil && credentials.Password != "" {
						write("private/credentials.json", details)
					}
				}
			}
		} else if path == "/deploy/servers" || path == "/deploy/status" {
			name := "deploy-response.json"
			if path == "/deploy/status" {
				name = "deploy-status.json"
			}

			write("share/"+name, accMatrixRedact(body))
		}

		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		recorder.event("api_transport_error", map[string]any{"error": err.Error()})
		http.Error(w, "upstream request failed", http.StatusBadGateway)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel") && keep() {
			recorder.event("cleanup_retained", nil)
			recorder.serverID.Store(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/servers/"), "/cancel"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{}}`)

			return
		}

		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(api.Close)
	t.Cleanup(func() {
		serverID, _ := recorder.serverID.Load().(string)
		if serverID == "0" {
			serverID = "" // The API uses zero while an order has no VM yet.
		}

		result := struct {
			Case     string `json:"case"`
			ServerID string `json:"server_id"`
			Passed   bool   `json:"passed"`
			Retained bool   `json:"retained"`
		}{t.Name(), serverID, !t.Failed(), keep() && !cancelled.Load() && serverID != ""}
		body, _ := json.Marshal(result)
		write("share/result.json", body)
		recorder.event("case_finished", result)
		t.Logf("matrix_result: %s", body)
	})

	return api.URL, recorder
}

func TestMatrixEvidenceRedaction(t *testing.T) {
	body := []byte(`{"data":[{"srv_id":"42","srv_serverkey":"server-key-secret","srv_vnc_token":"console-secret","install_details":{"root_password":"root-secret","prov_post":"echo root-secret"}}]}`)

	redacted := accMatrixRedact(body)
	if bytes.Contains(redacted, []byte("root-secret")) || bytes.Contains(redacted, []byte("console-secret")) || bytes.Contains(redacted, []byte("server-key-secret")) {
		t.Fatal("credentials leaked into shareable evidence")
	}

	if !bytes.Contains(redacted, []byte(`"srv_id":"42"`)) {
		t.Fatal("server ID was lost from evidence")
	}
}

func TestMatrixAPIRetention(t *testing.T) {
	for _, tc := range []struct {
		keep bool
		id   string
	}{{false, "42"}, {true, "42"}, {true, "0"}} {
		t.Run(tc.id+"/"+strconv.FormatBool(tc.keep), func(t *testing.T) {
			var cancellations atomic.Int64

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				var out io.Writer = w
				if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
					w.Header().Set("Content-Encoding", "gzip")

					compressed := gzip.NewWriter(w)
					defer compressed.Close()

					out = compressed
				}

				if strings.HasSuffix(r.URL.Path, "/cancel") {
					cancellations.Add(1)
					fmt.Fprint(out, `{"data":{}}`)

					return
				}

				fmt.Fprintf(out, `{"data":[{"srv_id":%q,"install_details":{"root_password":"root-secret","prov_post":"echo root-secret"}}]}`, tc.id)
			}))
			t.Cleanup(upstream.Close)
			dir := t.TempDir()
			t.Cleanup(func() {
				body, err := os.ReadFile(filepath.Join(dir, "share", "result.json"))

				var result struct {
					ServerID string `json:"server_id"`
					Retained bool   `json:"retained"`
				}
				if err != nil || json.Unmarshal(body, &result) != nil || (result.ServerID != "") != (tc.id != "0") || result.Retained != (tc.keep && tc.id != "0") {
					t.Fatal("matrix result incorrectly identified a retained VM")
				}
			})
			api, _ := accMatrixAPI(t, upstream.URL, dir, func() bool { return tc.keep })

			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path := "/servers/" + tc.id
				if method == http.MethodPost {
					path += "/cancel"
				}

				req, err := http.NewRequestWithContext(t.Context(), method, api+path, nil)
				if err != nil {
					t.Fatal(err)
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}

				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("proxy returned HTTP %d", resp.StatusCode)
				}
			}

			if (cancellations.Load() == 0) != tc.keep {
				t.Fatal("failed-VM retention did not control cancellation")
			}

			body, err := os.ReadFile(filepath.Join(dir, "share", "installer.json"))
			if err != nil || bytes.Contains(body, []byte("root-secret")) {
				t.Fatal("installer evidence missing or leaked credentials")
			}

			body, err = os.ReadFile(filepath.Join(dir, "private", "credentials.json"))
			if err != nil || !bytes.Contains(body, []byte("root-secret")) {
				t.Fatal("private debug credentials were not retained")
			}

			body, err = os.ReadFile(filepath.Join(dir, "share", "events.jsonl"))
			if err != nil || bytes.Contains(body, []byte("root-secret")) || !bytes.Contains(body, []byte(`"stage":"api_response"`)) {
				t.Fatal("API history missing or leaked credentials")
			}
		})
	}
}

func TestMatrixAPITransparentRecording(t *testing.T) {
	requestBody := `{"ip_id":"7793","dns":"acceptance.example.com"}`
	responseBody := `{"message":"rejected","root_password":"private-response"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != requestBody || r.Method != http.MethodPut || r.URL.Path != "/api/v0/servers/42/reverse" {
			t.Errorf("recorder changed the upstream request: %s %s %s (%v)", r.Method, r.URL.Path, body, err)
		}

		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("recorder changed authentication")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, responseBody)
	}))
	t.Cleanup(upstream.Close)
	dir := t.TempDir()
	api, _ := accMatrixAPI(t, upstream.URL+"/api/v0", dir, func() bool { return false })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, api+"/servers/42/reverse", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer private-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != responseBody || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("recorder changed the API response: %d %s (%v)", resp.StatusCode, body, err)
	}

	history, err := os.ReadFile(filepath.Join(dir, "share", "events.jsonl"))
	if err != nil || bytes.Contains(history, []byte("private-response")) || bytes.Contains(history, []byte("private-token")) || !bytes.Contains(history, []byte(`"ip_id":"7793"`)) || !bytes.Contains(history, []byte(`"status_code":400`)) {
		t.Fatal("request/error evidence missing or leaked credentials")
	}
}

func TestMatrixAllocationOwnership(t *testing.T) {
	recorder := &accMatrixRecorder{}
	recorder.serverID.Store("")
	recorder.deployment([]byte(`{"data":{"order_ids":[11]}}`))
	recorder.allocation([]byte(`{"data":{"servers":[{"order_id":"11","srv_id":0},{"order_id":"12","srv_id":43}]}}`))

	if recorder.serverID.Load() != "" {
		t.Fatal("unassigned or unrelated VM became a cleanup target")
	}

	recorder.allocation([]byte(`{"data":{"servers":[{"order_id":"11","srv_id":42},{"order_id":12,"srv_id":43}]}}`))

	if recorder.serverID.Load() != "42" {
		t.Fatal("assigned VM was lost")
	}

	ids := recorder.deployedServers()

	if len(ids) != 1 || ids[0] != "42" {
		t.Fatalf("cleanup targets include unrelated VMs: %v", ids)
	}
}

func TestSSHAttemptDeadline(t *testing.T) {
	_, signer := accEphemeralKey(t)
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)

	for _, stage := range []string{"handshake", "session", "command"} {
		t.Run(stage, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { listener.Close() })

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			done := make(chan struct{})
			go func() {
				defer close(done)

				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()

				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()

				if stage == "handshake" {
					io.Copy(io.Discard, conn)

					return
				}

				server, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
				if err != nil {
					return
				}
				defer server.Close()

				go ssh.DiscardRequests(requests)

				for ch := range channels {
					if stage == "session" {
						continue
					}

					channel, requests, err := ch.Accept()
					if err != nil {
						return
					}

					for range requests {
					}

					channel.Close()
				}
			}()

			err = accSSHAttempt(ctx, listener.Addr().String(), &ssh.ClientConfig{
				User: "root", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 100 * time.Millisecond,
			})

			want := stage
			if stage == "command" {
				want = "ssh run hostname"
			}

			if err == nil || !strings.Contains(err.Error(), want) || ctx.Err() != nil {
				t.Fatalf("%s exceeded attempt bound or returned wrong error: %v", stage, err)
			}

			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("stalled SSH server did not exit after the client closed")
			}
		})
	}
}

func TestConditionWait(t *testing.T) {
	checks := 0
	if err := accWait(t.Context(), time.Hour, func(context.Context) error {
		checks++

		return nil
	}); err != nil || checks != 1 {
		t.Fatalf("successful condition did not finish immediately: checks=%d error=%v", checks, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	last := errors.New("VM still installing")

	err := accWait(ctx, time.Hour, func(context.Context) error {
		cancel()

		return last
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, last) {
		t.Fatalf("cancelled wait lost the condition error: %v", err)
	}
}
