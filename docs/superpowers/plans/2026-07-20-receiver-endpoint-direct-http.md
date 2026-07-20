# Receiver Endpoint Direct HTTP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Android and desktop file-receive HTTP requests bypass system and environment proxies without changing proxy behavior for update checks or other networking.

**Architecture:** Android will route the existing `HttpReceiver` connection helper through `URL.openConnection(Proxy.NO_PROXY)`. Desktop `internal/httpdownload` will own a reusable client built from a clone of Go's default transport with `Proxy` cleared, and every list/download/manifest/range request will use that client.

**Tech Stack:** Java 8 networking and JUnit 4; Go `net/http` and standard `testing`; Android Gradle.

## Global Constraints

- Only file-receive HTTP traffic bypasses proxies.
- Directory listing, full download, retries, resume, range repair, manifests, and redirects all stay on the direct transport.
- Android update checks and desktop update checks continue using their existing normal clients.
- Do not mutate global proxy settings, `http.DefaultClient`, `http.DefaultTransport`, JVM properties, or environment variables in production code.
- Preserve existing request headers, timeouts, retry timing, decompression, endpoint resolution, and transfer concurrency.
- Preserve the user's existing uncommitted `VERSION` change.

---

### Task 1: Android Receiver Uses `Proxy.NO_PROXY`

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/HttpReceiver.java:29-31,784-789`
- Create: `android/app/src/test/java/cn/threatexpert/gonc/HttpReceiverProxyTest.java`

**Interfaces:**
- Consumes: Java `URL.openConnection(Proxy)` and the existing private `open(String)` call sites.
- Produces: package-private `static HttpURLConnection open(URL url) throws Exception`; existing `open(String)` delegates to it.

- [ ] **Step 1: Write the failing proxy-selection test**

Create `HttpReceiverProxyTest.java` with a custom URL handler that records the exact proxy passed by production code:

```java
package cn.threatexpert.gonc;

import org.junit.Test;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
import java.net.URLConnection;
import java.net.URLStreamHandler;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;

public class HttpReceiverProxyTest {
    @Test
    public void openForcesDirectConnectionAndKeepsTimeouts() throws Exception {
        AtomicReference<Proxy> selectedProxy = new AtomicReference<>();
        URL url = new URL(null, "http://127.0.0.1:12345/files", new URLStreamHandler() {
            @Override
            protected URLConnection openConnection(URL ignored) throws IOException {
                throw new AssertionError("no-argument proxy selection must not be used");
            }

            @Override
            protected URLConnection openConnection(URL target, Proxy proxy) {
                selectedProxy.set(proxy);
                return new RecordingConnection(target);
            }
        });

        HttpURLConnection connection = HttpReceiver.open(url);

        assertSame(Proxy.NO_PROXY, selectedProxy.get());
        assertEquals(10000, connection.getConnectTimeout());
        assertEquals(30000, connection.getReadTimeout());
    }

    private static final class RecordingConnection extends HttpURLConnection {
        RecordingConnection(URL url) {
            super(url);
        }

        @Override public void disconnect() {}
        @Override public boolean usingProxy() { return false; }
        @Override public void connect() {}
    }
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run from `android`:

```powershell
.\gradlew.bat :app:testDebugUnitTest --tests cn.threatexpert.gonc.HttpReceiverProxyTest
```

Expected: compilation fails because `HttpReceiver.open(URL)` does not exist or is inaccessible.

- [ ] **Step 3: Implement the direct Android connection helper**

Add `java.net.Proxy` and change the helper without touching its call sites:

```java
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
```

```java
private static HttpURLConnection open(String url) throws Exception {
    return open(new URL(url));
}

static HttpURLConnection open(URL url) throws Exception {
    HttpURLConnection conn = (HttpURLConnection) url.openConnection(Proxy.NO_PROXY);
    conn.setConnectTimeout(10000);
    conn.setReadTimeout(30000);
    return conn;
}
```

- [ ] **Step 4: Run focused and full Android unit tests**

Run from `android`:

```powershell
.\gradlew.bat :app:testDebugUnitTest --tests cn.threatexpert.gonc.HttpReceiverProxyTest
.\gradlew.bat :app:testDebugUnitTest
```

Expected: both commands end with `BUILD SUCCESSFUL`; the new proxy test passes along with existing receiver tests.

- [ ] **Step 5: Commit the Android change**

```powershell
git add -- android/app/src/main/java/cn/threatexpert/gonc/HttpReceiver.java android/app/src/test/java/cn/threatexpert/gonc/HttpReceiverProxyTest.java
git commit -m "fix(android): bypass proxy for receiver endpoint"
```

---

### Task 2: Desktop Receiver Owns a Direct HTTP Client

**Files:**
- Modify: `internal/httpdownload/downloader.go:141-154,237-247`
- Modify: `internal/httpdownload/downloader_test.go`

**Interfaces:**
- Consumes: Go's `http.DefaultTransport.(*http.Transport).Clone()`.
- Produces: `newDirectHTTPClient() *http.Client` and package-level `receiverHTTPClient`; `List` and download workers use the latter.

- [ ] **Step 1: Write failing desktop client tests**

Append tests that lock both the no-proxy property and isolation from the default transport:

```go
func TestNewDirectHTTPClientDisablesProxyWithoutMutatingDefaultTransport(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("default transport type = %T, want *http.Transport", http.DefaultTransport)
	}
	defaultProxy := defaultTransport.Proxy

	client := newDirectHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("receiver transport type = %T, want *http.Transport", client.Transport)
	}
	if transport == defaultTransport {
		t.Fatal("receiver transport aliases http.DefaultTransport")
	}
	if transport.Proxy != nil {
		t.Fatal("receiver transport still has a proxy function")
	}
	if defaultTransport.Proxy == nil || reflect.ValueOf(defaultTransport.Proxy).Pointer() != reflect.ValueOf(defaultProxy).Pointer() {
		t.Fatal("http.DefaultTransport proxy configuration was mutated")
	}
}

func TestReceiverHTTPClientCanListFilesDirectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `{"name":"file.txt","is_dir":false,"size":4,"path":"/file.txt"}`)
	}))
	defer server.Close()

	files, err := List(context.Background(), server.URL, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "/file.txt" {
		t.Fatalf("files = %#v, want /file.txt", files)
	}
}
```

The file already imports `reflect`, so no extra test import is required.

- [ ] **Step 2: Run the focused Go test and verify it fails**

```powershell
go test ./internal/httpdownload -run 'TestNewDirectHTTPClient|TestReceiverHTTPClient' -count=1
```

Expected: compilation fails with `undefined: newDirectHTTPClient`.

- [ ] **Step 3: Add the dedicated direct client and route every receiver request through it**

Add near the package constants/types in `downloader.go`:

```go
var receiverHTTPClient = newDirectHTTPClient()

func newDirectHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport}
}
```

Change list execution:

```go
resp, err := receiverHTTPClient.Do(req)
```

Change each worker to reuse the concurrency-safe dedicated client:

```go
go func() {
	defer wg.Done()
	for file := range queue {
		if ctx.Err() != nil {
			return
		}
		if err := d.downloadWithRetry(ctx, receiverHTTPClient, file, sink); err != nil {
```

Because manifest, full-file, and range helpers already receive that worker client, no other request sites need separate proxy logic.

- [ ] **Step 4: Format and run focused Go tests**

```powershell
gofmt -w internal/httpdownload/downloader.go internal/httpdownload/downloader_test.go
go test ./internal/httpdownload -run 'TestNewDirectHTTPClient|TestReceiverHTTPClient' -count=1
go test ./internal/httpdownload -count=1
```

Expected: all commands succeed and both focused tests pass.

- [ ] **Step 5: Commit the desktop change**

```powershell
git add -- internal/httpdownload/downloader.go internal/httpdownload/downloader_test.go
git commit -m "fix(desktop): bypass proxy for receiver endpoint"
```

---

### Task 3: Cross-Platform Regression Verification

**Files:**
- Verify only; no production file changes expected.

**Interfaces:**
- Consumes: Android `HttpReceiver.open(URL)` and desktop `receiverHTTPClient` from Tasks 1 and 2.
- Produces: verification evidence that receiver traffic is direct and unrelated update traffic remains unchanged.

- [ ] **Step 1: Confirm all Android receiver HTTP requests use the centralized helper**

```powershell
rg -n "openConnection\(|HttpReceiver\.open|open\(" android/app/src/main/java/cn/threatexpert/gonc/HttpReceiver.java android/app/src/main/java/cn/threatexpert/gonc/AndroidUpdateChecker.java
```

Expected: `HttpReceiver` has one `openConnection(Proxy.NO_PROXY)` site; its list/download/manifest/range paths use `open(...)`; `AndroidUpdateChecker` retains its separate default `openConnection()`.

- [ ] **Step 2: Confirm desktop receiver and update clients remain separated**

```powershell
rg -n "receiverHTTPClient|http\.DefaultClient|CheckForUpdate|newDirectHTTPClient" app.go internal/httpdownload internal/appupdate
```

Expected: receiver requests use `receiverHTTPClient`; update checking still receives its existing normal `http.Client` from `app.go`.

- [ ] **Step 3: Run complete regression suites**

```powershell
go test ./... -count=1
Push-Location android
.\gradlew.bat :app:testDebugUnitTest
Pop-Location
```

Expected: Go reports all packages passing; Gradle ends with `BUILD SUCCESSFUL`.

- [ ] **Step 4: Check diff hygiene and worktree ownership**

```powershell
git diff --check
git status --short
```

Expected: no whitespace errors; only the user's pre-existing `VERSION` modification remains after task commits.

