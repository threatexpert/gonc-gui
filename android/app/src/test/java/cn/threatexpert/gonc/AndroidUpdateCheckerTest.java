package cn.threatexpert.gonc;

import org.junit.Test;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

public class AndroidUpdateCheckerTest {
    @Test
    public void compareVersionsUsesNumericComponents() throws Exception {
        assertTrue(AndroidUpdateChecker.compareVersions("v1.2.16", "1.2.17") < 0);
        assertTrue(AndroidUpdateChecker.compareVersions("1.10.0", "1.9.9") > 0);
        assertEquals(0, AndroidUpdateChecker.compareVersions("1.2", "1.2.0"));
    }

    @Test
    public void compareVersionsRejectsUnsupportedSyntax() {
        assertInvalidVersion("", "1.2.0");
        assertInvalidVersion("1.2.0-beta", "1.2.0");
        assertInvalidVersion("1.two.0", "1.2.0");
    }

    @Test
    public void arm64SupportRequiresExactAbi() {
        assertTrue(AndroidUpdateChecker.supportsArm64(new String[]{"arm64-v8a", "armeabi-v7a"}));
        assertFalse(AndroidUpdateChecker.supportsArm64(new String[]{"armeabi-v7a"}));
    }

    @Test
    public void checkFollowsRedirectAndSelectsExactAndroidAsset() throws Exception {
        TestServer server = new TestServer();
        try {
            server.context("/manifest.json", exchange -> {
                writeHeaders(exchange, 302, 0, "Location: /real.json\r\n");
            });
            server.context("/real.json", exchange -> respond(exchange, 200,
                    "{\"app\":\"gonc-gui\",\"version\":\"1.2.17\",\"assets\":["
                            + "{\"name\":\"gonc-gui-windows-amd64.zip\",\"versioned_url\":\"https://wrong.example/windows.zip\"},"
                            + "{\"name\":\"gonc-gui-android-arm64.apk\",\"versioned_url\":\"https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-android-arm64.apk\"}]"
                            + "}"));
            server.start();

            AndroidUpdateChecker.Result result = AndroidUpdateChecker.check(
                    server.url("/manifest.json"), "1.2.16", new String[]{"arm64-v8a"});

            assertTrue(result.updateAvailable);
            assertEquals("1.2.17", result.latestVersion);
            assertEquals(
                    "https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-android-arm64.apk",
                    result.downloadUrl);
        } finally {
            server.close();
        }
    }

    @Test
    public void equalAndOlderVersionsHaveNoDownloadUrl() throws Exception {
        assertNoUpdate("1.2.16");
        assertNoUpdate("1.2.15");
    }

    @Test
    public void httpFailureMapsToNetwork() throws Exception {
        TestServer server = singleResponseServer(500, "server-controlled detail");
        try {
            assertCheckFailure(server.url("/manifest.json"), AndroidUpdateChecker.FailureKind.NETWORK);
        } finally {
            server.close();
        }
    }

    @Test
    public void incompleteResponseMapsToNetwork() throws Exception {
        TestServer server = new TestServer();
        try {
            server.context("/manifest.json", exchange -> {
                writeHeaders(exchange, 200, 100, "");
                exchange.getOutputStream().write("short".getBytes(StandardCharsets.UTF_8));
            });
            server.start();
            assertCheckFailure(server.url("/manifest.json"), AndroidUpdateChecker.FailureKind.NETWORK);
        } finally {
            server.close();
        }
    }

    @Test
    public void invalidManifestResponsesAreRejected() throws Exception {
        String validUrl = "https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-android-arm64.apk";
        String[] invalidDocuments = {
                repeat('x', AndroidUpdateChecker.MAX_MANIFEST_BYTES + 1),
                "{malformed",
                manifest("another-app", "1.2.17", AndroidUpdateChecker.ANDROID_ASSET, validUrl),
                manifest("gonc-gui", "1.2.17", "another.apk", validUrl),
                manifest("gonc-gui", "1.2.0-beta", AndroidUpdateChecker.ANDROID_ASSET, validUrl),
                manifest("gonc-gui", "1.2.16", AndroidUpdateChecker.ANDROID_ASSET,
                        "http://gonc.download/release.apk")
        };
        for (String document : invalidDocuments) {
            TestServer server = singleResponseServer(200, document);
            try {
                assertCheckFailure(server.url("/manifest.json"),
                        AndroidUpdateChecker.FailureKind.INVALID_MANIFEST);
            } finally {
                server.close();
            }
        }
    }

    @Test
    public void unsupportedAbiFailsBeforeHttpRequest() throws Exception {
        AtomicInteger requests = new AtomicInteger();
        TestServer server = new TestServer();
        try {
            server.context("/manifest.json", exchange -> {
                requests.incrementAndGet();
                respond(exchange, 200, manifest("gonc-gui", "1.2.17",
                        AndroidUpdateChecker.ANDROID_ASSET, "https://gonc.download/release.apk"));
            });
            server.start();
            try {
                AndroidUpdateChecker.check(server.url("/manifest.json"), "1.2.16",
                        new String[]{"armeabi-v7a"});
                fail("Expected unsupported platform failure");
            } catch (AndroidUpdateChecker.Failure failure) {
                assertEquals(AndroidUpdateChecker.FailureKind.UNSUPPORTED_PLATFORM, failure.kind);
            }
            assertEquals(0, requests.get());
        } finally {
            server.close();
        }
    }

    private static void assertInvalidVersion(String left, String right) {
        try {
            AndroidUpdateChecker.compareVersions(left, right);
            fail("Expected invalid manifest failure");
        } catch (AndroidUpdateChecker.Failure failure) {
            assertEquals(AndroidUpdateChecker.FailureKind.INVALID_MANIFEST, failure.kind);
        }
    }

    private static void assertNoUpdate(String remoteVersion) throws Exception {
        TestServer server = singleResponseServer(200, manifest("gonc-gui", remoteVersion,
                AndroidUpdateChecker.ANDROID_ASSET, "https://gonc.download/release.apk"));
        try {
            AndroidUpdateChecker.Result result = AndroidUpdateChecker.check(
                    server.url("/manifest.json"), "1.2.16", new String[]{"arm64-v8a"});
            assertFalse(result.updateAvailable);
            assertEquals("", result.downloadUrl);
        } finally {
            server.close();
        }
    }

    private static void assertCheckFailure(String endpoint, AndroidUpdateChecker.FailureKind kind) {
        try {
            AndroidUpdateChecker.check(endpoint, "1.2.16", new String[]{"arm64-v8a"});
            fail("Expected check failure");
        } catch (AndroidUpdateChecker.Failure failure) {
            assertEquals(kind, failure.kind);
            assertFalse(failure.getMessage().contains("server-controlled detail"));
        }
    }

    private static TestServer singleResponseServer(int status, String body) throws IOException {
        TestServer server = new TestServer();
        server.context("/manifest.json", exchange -> respond(exchange, status, body));
        server.start();
        return server;
    }

    private static String manifest(String app, String version, String asset, String url) {
        return "{\"app\":\"" + app + "\",\"version\":\"" + version
                + "\",\"assets\":[{\"name\":\"" + asset + "\",\"versioned_url\":\""
                + url + "\"}]}";
    }

    private static String repeat(char value, int count) {
        StringBuilder builder = new StringBuilder(count);
        for (int index = 0; index < count; index++) {
            builder.append(value);
        }
        return builder.toString();
    }

    private static void respond(Socket exchange, int status, String body) throws IOException {
        byte[] data = body.getBytes(StandardCharsets.UTF_8);
        writeHeaders(exchange, status, data.length, "Content-Type: application/json\r\n");
        exchange.getOutputStream().write(data);
    }

    private static void writeHeaders(Socket socket, int status, int length, String extraHeaders)
            throws IOException {
        String reason = status == 200 ? "OK" : status == 302 ? "Found" : "Error";
        String headers = "HTTP/1.1 " + status + " " + reason + "\r\n"
                + extraHeaders
                + "Content-Length: " + length + "\r\n"
                + "Connection: close\r\n\r\n";
        socket.getOutputStream().write(headers.getBytes(StandardCharsets.US_ASCII));
    }

    private static final class TestServer implements AutoCloseable {
        private final ServerSocket server;
        private final Map<String, TestHandler> handlers = new ConcurrentHashMap<>();
        private volatile boolean running;
        private Thread thread;

        TestServer() throws IOException {
            server = new ServerSocket(0, 10, java.net.InetAddress.getByName("127.0.0.1"));
        }

        void context(String path, TestHandler handler) {
            handlers.put(path, handler);
        }

        void start() {
            running = true;
            thread = new Thread(() -> {
                while (running) {
                    try (Socket socket = server.accept()) {
                        handle(socket);
                    } catch (IOException ignored) {
                        if (running) {
                            throw new RuntimeException(ignored);
                        }
                    }
                }
            }, "android-update-checker-test-server");
            thread.setDaemon(true);
            thread.start();
        }

        String url(String path) {
            return "http://127.0.0.1:" + server.getLocalPort() + path;
        }

        @Override
        public void close() throws IOException {
            running = false;
            server.close();
        }

        private void handle(Socket socket) throws IOException {
            BufferedReader reader = new BufferedReader(new InputStreamReader(
                    socket.getInputStream(), StandardCharsets.US_ASCII));
            String requestLine = reader.readLine();
            String header;
            while ((header = reader.readLine()) != null && !header.isEmpty()) {
                // Consume request headers before responding.
            }
            if (requestLine == null) {
                return;
            }
            String[] parts = requestLine.split(" ");
            TestHandler handler = parts.length >= 2 ? handlers.get(parts[1]) : null;
            if (handler == null) {
                respond(socket, 404, "not found");
            } else {
                handler.handle(socket);
            }
        }
    }

    private interface TestHandler {
        void handle(Socket socket) throws IOException;
    }
}
