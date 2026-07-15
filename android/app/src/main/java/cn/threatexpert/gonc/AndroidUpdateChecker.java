package cn.threatexpert.gonc;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.URL;
import java.nio.charset.StandardCharsets;

final class AndroidUpdateChecker {
    static final int CONNECT_TIMEOUT_MS = 10_000;
    static final int READ_TIMEOUT_MS = 10_000;
    static final int MAX_MANIFEST_BYTES = 1 << 20;
    static final String APP_NAME = "gonc-gui";
    static final String ANDROID_ASSET = "gonc-gui-android-arm64.apk";

    enum FailureKind { NETWORK, INVALID_MANIFEST, UNSUPPORTED_PLATFORM }

    static final class Failure extends Exception {
        final FailureKind kind;

        Failure(FailureKind kind, String message) {
            super(message);
            this.kind = kind;
        }

        Failure(FailureKind kind, String message, Throwable cause) {
            super(message, cause);
            this.kind = kind;
        }
    }

    static final class Result {
        final String currentVersion;
        final String latestVersion;
        final boolean updateAvailable;
        final String downloadUrl;

        Result(String currentVersion, String latestVersion, boolean updateAvailable, String downloadUrl) {
            this.currentVersion = currentVersion;
            this.latestVersion = latestVersion;
            this.updateAvailable = updateAvailable;
            this.downloadUrl = downloadUrl;
        }
    }

    private AndroidUpdateChecker() {
    }

    static int compareVersions(String left, String right) throws Failure {
        long[] leftParts = parseVersion(left);
        long[] rightParts = parseVersion(right);
        int count = Math.max(leftParts.length, rightParts.length);
        for (int index = 0; index < count; index++) {
            long leftPart = index < leftParts.length ? leftParts[index] : 0;
            long rightPart = index < rightParts.length ? rightParts[index] : 0;
            if (leftPart < rightPart) {
                return -1;
            }
            if (leftPart > rightPart) {
                return 1;
            }
        }
        return 0;
    }

    static boolean supportsArm64(String[] supportedAbis) {
        if (supportedAbis == null) {
            return false;
        }
        for (String abi : supportedAbis) {
            if ("arm64-v8a".equals(abi)) {
                return true;
            }
        }
        return false;
    }

    static Result check(String endpoint, String currentVersion, String[] supportedAbis) throws Failure {
        compareVersions(currentVersion, currentVersion);
        if (!supportsArm64(supportedAbis)) {
            throw new Failure(FailureKind.UNSUPPORTED_PLATFORM, "arm64-v8a is required");
        }
        String json = fetch(endpoint);
        return parseManifest(json, currentVersion);
    }

    private static String fetch(String endpoint) throws Failure {
        HttpURLConnection connection = null;
        try {
            connection = (HttpURLConnection) new URL(endpoint).openConnection();
            connection.setInstanceFollowRedirects(true);
            connection.setConnectTimeout(CONNECT_TIMEOUT_MS);
            connection.setReadTimeout(READ_TIMEOUT_MS);
            connection.setRequestMethod("GET");
            connection.setRequestProperty("Accept", "application/json");

            int status = connection.getResponseCode();
            if (status < 200 || status >= 300) {
                throw new Failure(FailureKind.NETWORK, "Update service returned HTTP " + status);
            }

            long expectedLength = connection.getContentLengthLong();
            byte[] data;
            try (InputStream input = connection.getInputStream();
                 ByteArrayOutputStream output = new ByteArrayOutputStream()) {
                byte[] buffer = new byte[8192];
                int read;
                while (output.size() <= MAX_MANIFEST_BYTES
                        && (read = input.read(buffer, 0,
                        Math.min(buffer.length, MAX_MANIFEST_BYTES + 1 - output.size()))) != -1) {
                    output.write(buffer, 0, read);
                }
                data = output.toByteArray();
            }
            if (data.length > MAX_MANIFEST_BYTES) {
                throw new Failure(FailureKind.INVALID_MANIFEST, "Update manifest is too large");
            }
            if (expectedLength >= 0 && data.length < expectedLength) {
                throw new Failure(FailureKind.NETWORK, "Update manifest response was incomplete");
            }
            return new String(data, StandardCharsets.UTF_8);
        } catch (Failure failure) {
            throw failure;
        } catch (IOException | RuntimeException error) {
            throw new Failure(FailureKind.NETWORK, "Unable to fetch update manifest", error);
        } finally {
            if (connection != null) {
                connection.disconnect();
            }
        }
    }

    private static Result parseManifest(String json, String currentVersion) throws Failure {
        try {
            JSONObject manifest = new JSONObject(json);
            if (!APP_NAME.equals(manifest.getString("app"))) {
                throw invalidManifest("Unexpected app in update manifest");
            }
            String latestVersion = manifest.getString("version");
            int versionOrder = compareVersions(currentVersion, latestVersion);
            JSONArray assets = manifest.getJSONArray("assets");
            String downloadUrl = null;
            for (int index = 0; index < assets.length(); index++) {
                JSONObject asset = assets.getJSONObject(index);
                if (ANDROID_ASSET.equals(asset.getString("name"))) {
                    downloadUrl = asset.getString("versioned_url");
                    break;
                }
            }
            if (downloadUrl == null || downloadUrl.isEmpty()) {
                throw invalidManifest("Android update asset is missing");
            }
            URI downloadUri = new URI(downloadUrl);
            if (!"https".equals(downloadUri.getScheme())
                    || downloadUri.getHost() == null
                    || downloadUri.getHost().isEmpty()) {
                throw invalidManifest("Android update asset URL must be absolute HTTPS");
            }

            boolean updateAvailable = versionOrder < 0;
            return new Result(currentVersion, latestVersion, updateAvailable,
                    updateAvailable ? downloadUrl : "");
        } catch (Failure failure) {
            throw failure;
        } catch (JSONException | URISyntaxException | NumberFormatException error) {
            throw new Failure(FailureKind.INVALID_MANIFEST, "Invalid update manifest", error);
        }
    }

    private static Failure invalidManifest(String message) {
        return new Failure(FailureKind.INVALID_MANIFEST, message);
    }

    private static long[] parseVersion(String version) throws Failure {
        if (version == null) {
            throw new Failure(FailureKind.INVALID_MANIFEST, "Version is missing");
        }
        String normalized = version.trim();
        if (normalized.startsWith("v")) {
            normalized = normalized.substring(1);
        }
        if (normalized.isEmpty()) {
            throw new Failure(FailureKind.INVALID_MANIFEST, "Version is empty");
        }
        String[] components = normalized.split("\\.", -1);
        long[] parsed = new long[components.length];
        for (int index = 0; index < components.length; index++) {
            String component = components[index];
            if (component.isEmpty()) {
                throw new Failure(FailureKind.INVALID_MANIFEST, "Version component is empty");
            }
            for (int character = 0; character < component.length(); character++) {
                if (!Character.isDigit(component.charAt(character))) {
                    throw new Failure(FailureKind.INVALID_MANIFEST, "Version component is not numeric");
                }
            }
            try {
                parsed[index] = Long.parseLong(component);
            } catch (NumberFormatException error) {
                throw new Failure(FailureKind.INVALID_MANIFEST, "Version component is invalid", error);
            }
        }
        return parsed;
    }
}
