package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.Context;
import android.database.Cursor;
import android.net.Uri;
import android.provider.DocumentsContract;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

final class HttpReceiver {
    interface ListCallback {
        void onList(List<RemoteFile> files, int fileCount, int dirCount, long totalBytes);

        void onError(Throwable error);
    }

    interface Callback {
        void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, double bytesPerSecond, String current);

        void onComplete(int totalFiles, long totalBytes, int skippedFiles, int resumedFiles);

        void onError(Throwable error);
    }

    static Session startList(String serverUrl, ListCallback callback) {
        return startList(serverUrl, "", callback);
    }

    static Session startList(String serverUrl, String subPath, ListCallback callback) {
        List<String> paths = new ArrayList<>();
        paths.add(subPath);
        return startList(serverUrl, paths, callback);
    }

    static Session startList(String serverUrl, List<String> subPaths, ListCallback callback) {
        Session session = new Session();
        Thread worker = new Thread(() -> {
            try {
                List<RemoteFile> files = dropRootDirectories(listMany(serverUrl, subPaths, session));
                int fileCount = 0;
                int dirCount = 0;
                long totalBytes = 0;
                for (RemoteFile file : files) {
                    if (file.isDir) {
                        dirCount++;
                    } else {
                        fileCount++;
                        totalBytes += Math.max(0, file.size);
                    }
                }
                if (callback != null && !session.isStopped()) {
                    callback.onList(files, fileCount, dirCount, totalBytes);
                }
            } catch (Throwable error) {
                if (callback != null && !session.isStopped()) {
                    callback.onError(error);
                }
            }
        }, "gonc-http-list");
        worker.start();
        return session;
    }

    static Session start(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, Callback callback) {
        Session session = new Session();
        Thread worker = new Thread(() -> run(context.getApplicationContext(), serverUrl, saveTreeUri, requestedFiles, resume, session, callback), "gonc-http-receive");
        worker.start();
        return session;
    }

    private static void run(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, Session session, Callback callback) {
        try {
            List<RemoteFile> files = requestedFiles == null ? dropRootDirectories(list(serverUrl, "", session)) : new ArrayList<>(requestedFiles);
            long totalBytes = 0;
            int totalFiles = 0;
            for (RemoteFile file : files) {
                if (!file.isDir) {
                    totalFiles++;
                    totalBytes += Math.max(0, file.size);
                }
            }

            int doneFiles = 0;
            long doneBytes = 0;
            long networkDoneBytes = 0;
            int skippedFiles = 0;
            int resumedFiles = 0;
            ProgressClock clock = new ProgressClock();
            if (callback != null) {
                callback.onProgress(0, totalFiles, 0, totalBytes, 0, "");
            }
            for (RemoteFile file : files) {
                if (session.isStopped()) {
                    return;
                }
                if (file.isDir) {
                    ensureDirectory(context, saveTreeUri, file.path);
                    continue;
                }
                DownloadResult result = downloadOne(context, serverUrl, file, saveTreeUri, resume, session, callback, clock, doneFiles, totalFiles, doneBytes, networkDoneBytes, totalBytes);
                doneFiles++;
                doneBytes += result.doneBytes;
                networkDoneBytes += result.networkBytes;
                if (result.skipped) {
                    skippedFiles++;
                }
                if (result.resumed) {
                    resumedFiles++;
                }
                if (callback != null) {
                    callback.onProgress(doneFiles, totalFiles, doneBytes, totalBytes, clock.bytesPerSecond, file.path);
                }
            }
            if (callback != null && !session.isStopped()) {
                callback.onComplete(totalFiles, totalBytes, skippedFiles, resumedFiles);
            }
        } catch (Throwable error) {
            if (callback != null && !session.isStopped()) {
                callback.onError(error);
            }
        }
    }

    private static List<RemoteFile> listMany(String serverUrl, List<String> subPaths, Session session) throws Exception {
        Map<String, RemoteFile> merged = new LinkedHashMap<>();
        if (subPaths == null || subPaths.isEmpty()) {
            subPaths = new ArrayList<>();
            subPaths.add("");
        }
        for (String subPath : subPaths) {
            if (session.isStopped()) {
                break;
            }
            for (RemoteFile file : list(serverUrl, subPath, session)) {
                String key = normalizePath(file.path);
                if (!merged.containsKey(key)) {
                    merged.put(key, file);
                }
            }
        }
        return new ArrayList<>(merged.values());
    }

    private static List<RemoteFile> list(String serverUrl, String subPath, Session session) throws Exception {
        HttpURLConnection conn = open(resolveUrl(serverUrl, subPath));
        conn.setRequestProperty("Accept", "application/json");
        if (conn.getResponseCode() == HttpURLConnection.HTTP_NOT_FOUND) {
            return new ArrayList<>();
        }
        if (conn.getResponseCode() != HttpURLConnection.HTTP_OK) {
            throw new IllegalStateException("Remote list failed: HTTP " + conn.getResponseCode());
        }
        List<RemoteFile> files = new ArrayList<>();
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(conn.getInputStream(), StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                if (session.isStopped()) {
                    return files;
                }
                line = line.trim();
                if (line.isEmpty()) {
                    continue;
                }
                JSONObject json = new JSONObject(line);
                RemoteFile file = new RemoteFile();
                file.name = json.optString("name");
                file.isDir = json.optBoolean("is_dir");
                file.size = json.optLong("size");
                file.path = json.optString("path");
                if (file.path == null || file.path.trim().isEmpty()) {
                    file.path = file.name;
                }
                files.add(file);
            }
        } finally {
            conn.disconnect();
        }
        return files;
    }

    private static String normalizePath(String path) {
        String clean = path == null ? "" : path.trim().replace('\\', '/');
        while (clean.startsWith("/")) {
            clean = clean.substring(1);
        }
        while (clean.endsWith("/") && clean.length() > 1) {
            clean = clean.substring(0, clean.length() - 1);
        }
        return ".".equals(clean) ? "" : clean;
    }

    private static DownloadResult downloadOne(Context context, String serverUrl, RemoteFile file, Uri saveTreeUri, boolean resume, Session session, Callback callback, ProgressClock clock, int doneFiles, int totalFiles, long baseDoneBytes, long baseNetworkBytes, long totalBytes) throws Exception {
        DocumentInfo target = ensureFile(context, saveTreeUri, file.path, file.name, !resume);
        long offset = resume ? Math.max(0, target.size) : 0;
        if (resume && offset >= file.size && file.size >= 0) {
            return new DownloadResult(file.size, 0, true, false);
        }

        HttpURLConnection conn = open(resolveUrl(serverUrl, file.path));
        if (offset > 0) {
            conn.setRequestProperty("Range", "bytes=" + offset + "-");
        }
        int status = conn.getResponseCode();
        boolean resumed = offset > 0 && status == HttpURLConnection.HTTP_PARTIAL;
        if (offset > 0 && status == HttpURLConnection.HTTP_OK) {
            offset = 0;
        } else if (status != HttpURLConnection.HTTP_OK && status != HttpURLConnection.HTTP_PARTIAL) {
            throw new IllegalStateException("Download failed for " + file.path + ": HTTP " + status);
        }

        long copied = offset;
        long initialCopied = offset;
        if (callback != null && offset > 0) {
            callback.onProgress(doneFiles, totalFiles, baseDoneBytes + offset, totalBytes, 0, file.path);
        }
        ContentResolver resolver = context.getContentResolver();
        try (InputStream input = conn.getInputStream(); OutputStream output = resolver.openOutputStream(target.uri, offset > 0 ? "wa" : "wt")) {
            if (output == null) {
                throw new IllegalStateException("Cannot open destination: " + file.path);
            }
            byte[] buffer = new byte[128 * 1024];
            int n;
            while ((n = input.read(buffer)) >= 0) {
                if (session.isStopped()) {
                    return new DownloadResult(copied, Math.max(0, copied - initialCopied), false, resumed);
                }
                output.write(buffer, 0, n);
                copied += n;
                long networkCopied = Math.max(0, copied - initialCopied);
                if (callback != null && clock.shouldEmit(baseNetworkBytes + networkCopied)) {
                    callback.onProgress(doneFiles, totalFiles, baseDoneBytes + copied, totalBytes, clock.bytesPerSecond, file.path);
                }
            }
        } finally {
            conn.disconnect();
        }
        return new DownloadResult(copied, Math.max(0, copied - initialCopied), false, resumed);
    }

    private static HttpURLConnection open(String url) throws Exception {
        HttpURLConnection conn = (HttpURLConnection) new URL(url).openConnection();
        conn.setConnectTimeout(10000);
        conn.setReadTimeout(30000);
        return conn;
    }

    private static DocumentInfo ensureFile(Context context, Uri treeUri, String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
        String[] parts = pathParts(remotePath);
        Uri parent = rootDocumentUri(treeUri);
        for (int i = 0; i < parts.length - 1; i++) {
            parent = findOrCreate(context, treeUri, parent, parts[i], DocumentsContract.Document.MIME_TYPE_DIR);
        }
        String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
        DocumentInfo existing = findChild(context, treeUri, parent, name);
        if (existing != null && replaceExisting) {
            DocumentsContract.deleteDocument(context.getContentResolver(), existing.uri);
            existing = null;
        }
        if (existing != null) {
            return existing;
        }
        Uri created = DocumentsContract.createDocument(context.getContentResolver(), parent, "application/octet-stream", name);
        if (created == null) {
            throw new IllegalStateException("Cannot create " + name);
        }
        return new DocumentInfo(created, 0);
    }

    private static Uri ensureDirectory(Context context, Uri treeUri, String remotePath) throws Exception {
        Uri parent = rootDocumentUri(treeUri);
        for (String part : pathParts(remotePath)) {
            parent = findOrCreate(context, treeUri, parent, part, DocumentsContract.Document.MIME_TYPE_DIR);
        }
        return parent;
    }

    private static Uri findOrCreate(Context context, Uri treeUri, Uri parent, String name, String mimeType) throws Exception {
        DocumentInfo existing = findChild(context, treeUri, parent, name);
        if (existing != null) {
            return existing.uri;
        }
        Uri created = DocumentsContract.createDocument(context.getContentResolver(), parent, mimeType, name);
        if (created == null) {
            throw new IllegalStateException("Cannot create " + name);
        }
        return created;
    }

    private static DocumentInfo findChild(Context context, Uri treeUri, Uri parent, String name) {
        String parentId = DocumentsContract.getDocumentId(parent);
        Uri children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId);
        String[] columns = {
                DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                DocumentsContract.Document.COLUMN_SIZE
        };
        Cursor cursor = context.getContentResolver().query(children, columns, null, null, null);
        if (cursor == null) {
            return null;
        }
        try {
            while (cursor.moveToNext()) {
                String childName = cursor.getString(1);
                if (name.equals(childName)) {
                    long size = cursor.isNull(2) ? 0 : cursor.getLong(2);
                    return new DocumentInfo(DocumentsContract.buildDocumentUriUsingTree(treeUri, cursor.getString(0)), size);
                }
            }
        } finally {
            cursor.close();
        }
        return null;
    }

    static String displayName(Context context, Uri uri) {
        try {
            Uri root = rootDocumentUri(uri);
            Cursor cursor = context.getContentResolver().query(root, new String[]{DocumentsContract.Document.COLUMN_DISPLAY_NAME}, null, null, null);
            if (cursor != null) {
                try {
                    if (cursor.moveToFirst()) {
                        String name = cursor.getString(0);
                        if (name != null && !name.trim().isEmpty()) {
                            return name;
                        }
                    }
                } finally {
                    cursor.close();
                }
            }
        } catch (RuntimeException ignored) {
        }
        return context.getString(R.string.selected_folder);
    }

    private static Uri rootDocumentUri(Uri treeUri) {
        return DocumentsContract.buildDocumentUriUsingTree(treeUri, DocumentsContract.getTreeDocumentId(treeUri));
    }

    private static String[] pathParts(String path) {
        String clean = path == null ? "" : path.trim();
        while (clean.startsWith("/")) {
            clean = clean.substring(1);
        }
        if (clean.isEmpty() || ".".equals(clean)) {
            return new String[0];
        }
        String[] raw = clean.split("/");
        List<String> parts = new ArrayList<>();
        for (String part : raw) {
            part = safeName(part);
            if (!part.isEmpty() && !".".equals(part) && !"..".equals(part)) {
                parts.add(part);
            }
        }
        return parts.toArray(new String[0]);
    }

    private static String safeName(String value) {
        String name = value == null ? "" : value.trim();
        name = name.replace('/', '_').replace('\\', '_').replace(':', '_');
        return name.isEmpty() ? "received-file" : name;
    }

    private static String resolveUrl(String serverUrl, String path) throws Exception {
        String base = serverUrl.endsWith("/") ? serverUrl.substring(0, serverUrl.length() - 1) : serverUrl;
        String[] parts = pathParts(path);
        StringBuilder builder = new StringBuilder(base);
        for (String part : parts) {
            builder.append('/');
            builder.append(URLEncoder.encode(part, "UTF-8").replace("+", "%20"));
        }
        return builder.toString();
    }

    private static List<RemoteFile> dropRootDirectories(List<RemoteFile> files) {
        List<RemoteFile> out = new ArrayList<>();
        for (RemoteFile file : files) {
            if (file.isDir) {
                String path = file.path == null ? "" : file.path.trim();
                if (path.isEmpty() || "/".equals(path) || ".".equals(path)) {
                    continue;
                }
            }
            out.add(file);
        }
        return out;
    }

    static String formatBytes(long value) {
        String[] units = {"B", "KB", "MB", "GB", "TB"};
        double size = Math.max(0, value);
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    static final class Session {
        private volatile boolean stopped;

        void stop() {
            stopped = true;
        }

        boolean isStopped() {
            return stopped;
        }
    }

    private static final class ProgressClock {
        private long lastMs = System.currentTimeMillis();
        private long lastBytes;
        private double bytesPerSecond;

        boolean shouldEmit(long doneBytes) {
            long now = System.currentTimeMillis();
            long elapsed = now - lastMs;
            if (elapsed < 1000 && doneBytes != lastBytes) {
                return false;
            }
            if (elapsed > 0) {
                bytesPerSecond = Math.max(0, (doneBytes - lastBytes) * 1000.0 / elapsed);
            }
            lastMs = now;
            lastBytes = doneBytes;
            return true;
        }
    }

    static final class RemoteFile {
        String name;
        boolean isDir;
        long size;
        String path;
    }

    private static final class DocumentInfo {
        final Uri uri;
        final long size;

        DocumentInfo(Uri uri, long size) {
            this.uri = uri;
            this.size = size;
        }
    }

    private static final class DownloadResult {
        final long doneBytes;
        final long networkBytes;
        final boolean skipped;
        final boolean resumed;

        DownloadResult(long doneBytes, long networkBytes, boolean skipped, boolean resumed) {
            this.doneBytes = doneBytes;
            this.networkBytes = networkBytes;
            this.skipped = skipped;
            this.resumed = resumed;
        }
    }
}
