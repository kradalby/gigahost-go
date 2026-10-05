package tfprovider_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
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
)

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

// The framework always destroys resources. This test-only proxy records the
// actual API inputs and skips cancellation only for opted-in failed cells.
func accMatrixAPI(t *testing.T, baseURL, dir string, keep func() bool) string {
	t.Helper()

	for _, subdir := range []string{"share", "private"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}

	var id atomic.Value
	id.Store("")

	var (
		cancelled atomic.Bool
		mu        sync.Mutex
	)

	write := func(name string, body []byte) {
		mu.Lock()
		defer mu.Unlock()

		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Errorf("save matrix evidence: %v", err)
		}
	}
	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		r := pr.Out
		if r.Method == http.MethodPost && r.URL.Path == "/deploy/servers" {
			body, err := io.ReadAll(r.Body)
			r.Body.Close()

			if err != nil {
				t.Errorf("read deployment request: %v", err)
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			write("share/deploy-request.json", accMatrixRedact(body))
		}

		pr.SetURL(target)
		// Let the transport decompress responses before recording JSON.
		r.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		path := strings.TrimPrefix(r.Request.URL.Path, strings.TrimSuffix(target.Path, "/"))
		if r.Request.Method == http.MethodPost && strings.HasSuffix(path, "/cancel") && r.StatusCode < 300 {
			cancelled.Store(true)
		}

		if path != "/deploy/servers" && path != "/deploy/status" && (!strings.HasPrefix(path, "/servers/") || strings.Count(path, "/") != 2) {
			return nil
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()

		if err != nil {
			return err
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		if after, ok := strings.CutPrefix(path, "/servers/"); ok {
			serverID := after
			if id.Load() != serverID {
				t.Logf("server_id=%s", serverID)
				id.Store(serverID)
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
		} else {
			name := "deploy-response.json"
			if path == "/deploy/status" {
				name = "deploy-status.json"
			}

			write("share/"+name, accMatrixRedact(body))
		}

		return nil
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel") && keep() {
			id.Store(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/servers/"), "/cancel"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{}}`)

			return
		}

		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(api.Close)
	t.Cleanup(func() {
		serverID, _ := id.Load().(string)
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
		t.Logf("matrix_result: %s", body)
	})

	return api.URL
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
			api := accMatrixAPI(t, upstream.URL, dir, func() bool { return tc.keep })

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
		})
	}
}
